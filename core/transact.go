package core

import (
	"context"
	"sync"
	"time"

	"chain/core/account"
	"chain/core/txbuilder"
	"chain/database/pg"
	"chain/errors"
	"chain/metrics"
	"chain/net/http/reqid"
	"chain/net/trace/span"
	"chain/protocol"
	"chain/protocol/bc"
)

const (
	defaultTxTTL = 5 * time.Minute
)

func buildSingle(ctx context.Context, req *buildRequest) (*txbuilder.Template, error) {
	defer metrics.RecordElapsed(time.Now())
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)
	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback(ctx)

	ttl := req.TTL
	if ttl == 0 {
		ttl = defaultTxTTL
	}
	maxTime := time.Now().Add(ttl)
	tpl, err := txbuilder.Build(ctx, req.Tx, req.actions(), req.ReferenceData, maxTime)
	if err != nil {
		return nil, err
	}

	err = dbtx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	// ensure null is never returned for inputs
	if tpl.Inputs == nil {
		tpl.Inputs = []*txbuilder.Input{}
	}

	return tpl, nil
}

// POST /build-transaction-template
func build(ctx context.Context, buildReqs []*buildRequest) (interface{}, error) {
	defer metrics.RecordElapsed(time.Now())
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	responses := make([]interface{}, len(buildReqs))
	var wg sync.WaitGroup
	wg.Add(len(responses))

	for i := 0; i < len(responses); i++ {
		go func(i int) {
			defer wg.Done()

			err := filterAliases(ctx, buildReqs[i])
			if err != nil {
				logHTTPError(ctx, err)
				responses[i], _ = errInfo(err)
				return
			}

			resp, err := buildSingle(reqid.NewSubContext(ctx, reqid.New()), buildReqs[i])
			if err != nil {
				logHTTPError(ctx, err)
				responses[i], _ = errInfo(err)
			} else {
				responses[i] = resp
			}
		}(i)
	}

	wg.Wait()
	return responses, nil
}

type submitSingleArg struct {
	tpl  *txbuilder.Template
	wait time.Duration
}

func submitSingle(ctx context.Context, c *protocol.Chain, x submitSingleArg) (interface{}, error) {
	defer metrics.RecordElapsed(time.Now())
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	// TODO(bobg): Set up an expiring context object outside this
	// function, perhaps in handler.ServeHTTPContext, and perhaps
	// initialize the timeout from the HTTP Timeout field.  (Or just
	// switch to gRPC.)
	timeout := x.wait
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tx, err := finalizeTxWait(ctx, c, x.tpl)
	if err != nil {
		return nil, err
	}

	return map[string]string{"id": tx.Hash.String()}, nil
}

// finalizeTxWait calls FinalizeTx and then waits for confirmation of
// the transaction.  A nil error return means the transaction is
// confirmed on the blockchain.  ErrRejected means a conflicting tx is
// on the blockchain.  context.DeadlineExceeded means ctx is an
// expiring context that timed out.
func finalizeTxWait(ctx context.Context, c *protocol.Chain, txTemplate *txbuilder.Template) (*bc.Tx, error) {
	// Avoid a race condition.  Calling c.Height() here ensures that
	// when we start waiting for blocks below, we don't begin waiting at
	// block N+1 when the tx we want is in block N.
	height := c.Height()

	tx, err := txbuilder.FinalizeTx(ctx, c, txTemplate)
	if err != nil {
		return nil, err
	}

	// As a rule we only index confirmed blockchain data to prevent dirty
	// reads, but here we're explicitly breaking that rule iff all of the
	// inputs to the transaction are from locally-controlled keys. In that
	// case, we're confident that this tx will be confirmed, so we relax
	// that constraint to allow use of unconfirmed change, etc.
	if txTemplate.Local {
		err := account.IndexUnconfirmedUTXOs(ctx, tx)
		if err != nil {
			return nil, errors.Wrap(err, "indexing unconfirmed account utxos")
		}
	}

	for {
		height++
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case <-waitBlock(ctx, c, height):
			// TODO(jackson): Avoid stampeding herd of get block queries.
			// Maybe just cache n most recent blocks in protocol.Chain?
			b, err := c.GetBlock(ctx, height)
			if err != nil {
				return nil, errors.Wrap(err, "getting block that just landed")
			}
			for _, confirmed := range b.Transactions {
				if confirmed.Hash == tx.Hash {
					// confirmed
					return tx, nil
				}
			}

			poolTxs, err := c.PendingTxs(ctx, tx.Hash)
			if err != nil {
				return nil, errors.Wrap(err, "getting pool txs")
			}
			if _, ok := poolTxs[tx.Hash]; !ok {
				// rejected
				return nil, txbuilder.ErrRejected
			}

			// still in the pool; iterate
		}
	}
}

func waitBlock(ctx context.Context, c *protocol.Chain, height uint64) <-chan struct{} {
	done := make(chan struct{}, 1)
	go func() {
		c.WaitForBlock(height)
		done <- struct{}{}
	}()
	return done
}

// TODO(bobg): allow caller to specify reservation by (encrypted) id?
// POST /v3/assets/cancel-reservation
// Idempotent
func cancelReservation(ctx context.Context, x struct{ Transaction bc.Tx }) error {
	var outpoints []bc.Outpoint
	for _, input := range x.Transaction.Inputs {
		if !input.IsIssuance() {
			outpoints = append(outpoints, input.Outpoint())
		}
	}
	return account.CancelReservations(ctx, outpoints)
}

type submitArg struct {
	Transactions []*txbuilder.Template
	wait         time.Duration
}

// POST /v3/transact/submit
// Idempotent
func (a *api) submit(ctx context.Context, x submitArg) interface{} {
	defer metrics.RecordElapsed(time.Now())
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	responses := make([]interface{}, len(x.Transactions))
	var wg sync.WaitGroup
	wg.Add(len(responses))
	for i := range responses {
		go func(i int) {
			resp, err := submitSingle(reqid.NewSubContext(ctx, reqid.New()), a.c, submitSingleArg{tpl: x.Transactions[i], wait: x.wait})
			if err != nil {
				logHTTPError(ctx, err)
				responses[i], _ = errInfo(err)
			} else {
				responses[i] = resp
			}
			wg.Done()
		}(i)
	}

	wg.Wait()
	return responses
}
