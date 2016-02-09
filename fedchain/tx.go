package fedchain

import (
	"time"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/state"
	"chain/fedchain/validation"
	"chain/metrics"
)

// MaxBlockTxs limits the number of transactions
// included in each block.
const MaxBlockTxs = 10000

// AddTx inserts tx into the set of "pending" transactions available
// to be included in the next block produced by GenerateBlock.
//
// It validates tx against the blockchain state and the existing
// pending pool.
//
// TODO(kr): accept tx if it is valid for any *subset* of the pool.
// This means accepting conflicting transactions in the same pool
// at the same time.
func (fc *FC) AddTx(ctx context.Context, tx *bc.Tx) error {
	dbtx, txCtx, err := pg.Begin(ctx)
	if err != nil {
		return errors.Wrap(err)
	}
	defer dbtx.Rollback(ctx)

	poolView, err := fc.store.NewPoolViewForPrevouts(txCtx, []*bc.Tx{tx})
	if err != nil {
		return errors.Wrap(err)
	}

	bcView, err := fc.store.NewViewForPrevouts(txCtx, []*bc.Tx{tx})
	if err != nil {
		return errors.Wrap(err)
	}

	view := state.MultiReader(poolView, bcView)
	// TODO(kr): get current block hash for last argument to ValidateTx
	err = validation.ValidateTx(txCtx, view, tx, uint64(time.Now().Unix()), nil)
	if err != nil {
		return errors.Wrapf(ErrTxRejected, "validate tx: %v", err)
	}

	// Update persistent tx pool state
	err = fc.applyTx(txCtx, tx)
	if err != nil {
		return errors.Wrap(err, "apply TX")
	}

	err = dbtx.Commit(txCtx)
	if err != nil {
		return errors.Wrap(err)
	}

	for _, cb := range fc.txCallbacks {
		cb(ctx, tx)
	}
	return nil
}

// applyTx updates the output set to reflect
// the effects of tx. It deletes consumed utxos
// and inserts newly-created outputs.
// Must be called inside a transaction.
func (fc *FC) applyTx(ctx context.Context, tx *bc.Tx) (err error) {
	defer metrics.RecordElapsed(time.Now())

	_ = pg.FromContext(ctx).(pg.Tx) // panics if not in a db transaction

	fc.store.ApplyTx(ctx, tx)

	return errors.Wrap(err, "insert into pool inputs")
}
