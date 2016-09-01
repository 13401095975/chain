package protocol

import (
	"context"
	"time"

	"chain/errors"
	"chain/protocol/bc"
	"chain/protocol/validation"
)

// AddTx inserts tx into the set of "pending" transactions available
// to be included in the next block produced by GenerateBlock.
//
// It validates tx against the blockchain state and the existing
// pending pool.
//
// It is okay to add the same transaction more than once; subsequent
// attempts will have no effect and return a nil error.
//
// It is okay to add conflicting transactions to the pool. The conflict
// will be resolved when a block lands.
//
// It is an error to call AddTx before the initial block has landed.
// Use WaitForBlock to guarantee this.
func (c *Chain) AddTx(ctx context.Context, tx *bc.Tx) error {
	// Check if the transaction already exists in the tx pool.
	poolTxs, err := c.pool.GetTxs(ctx, tx.Hash)
	if err != nil {
		return errors.Wrap(err)
	}
	if _, ok := poolTxs[tx.Hash]; ok {
		return nil
	}
	// Check if the transaction already exists in the blockchain.
	bcTxs, err := c.store.GetTxs(ctx, tx.Hash)
	if err != nil {
		return errors.Wrap(err)
	}
	if _, ok := bcTxs[tx.Hash]; ok {
		return nil
	}

	// Check if this transaction's max time has already elapsed.
	// We purposely do not check the min time, because we can still
	// add it to the pool if it hasn't been reached yet.
	if tx.MaxTime > 0 && bc.Millis(time.Now()) > tx.MaxTime {
		return errors.WithDetail(validation.ErrBadTx, "transaction max time has passed")
	}

	err = validation.ValidateTx(tx)
	if err != nil {
		return errors.Wrap(err, "tx rejected")
	}

	// Update persistent tx pool state.
	err = c.pool.Insert(ctx, tx)
	return errors.Wrap(err, "applying tx to store")
}
