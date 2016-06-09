package mempool

import (
	"chain/cos/bc"
	"chain/cos/state"

	"golang.org/x/net/context"
)

// MemPool satisfies the cos.Pool interface.
// It is used by tests to avoid needing a database.
type MemPool struct {
	pool    []*bc.Tx // used for keeping topological order
	poolMap map[bc.Hash]*bc.Tx
}

// New returns a new MemPool.
func New() *MemPool {
	return &MemPool{
		poolMap: make(map[bc.Hash]*bc.Tx),
	}
}

// Insert adds a new pending tx to the pending tx pool.
func (m *MemPool) Insert(ctx context.Context, tx *bc.Tx, assets map[bc.AssetID]*state.AssetState) error {
	m.poolMap[tx.Hash] = tx
	m.pool = append(m.pool, tx)
	return nil
}

// GetTxs looks up transactions in the tx pool by their tx hashes.
func (m *MemPool) GetTxs(ctx context.Context, hashes ...bc.Hash) (poolTxs map[bc.Hash]*bc.Tx, err error) {
	poolTxs = make(map[bc.Hash]*bc.Tx)
	for _, hash := range hashes {
		if tx := m.poolMap[hash]; tx != nil {
			poolTxs[hash] = m.poolMap[hash]
		}
	}
	return poolTxs, nil
}

// GetPrevouts looks up each prevout of the provided transactions in the
// transaction pool. It returns all the matching outputs that exist in
// the pool.
func (m *MemPool) GetPrevouts(ctx context.Context, txs []*bc.Tx) (map[bc.Outpoint]*state.Output, error) {
	prevouts := map[bc.Outpoint]*state.Output{}
	for _, tx := range txs {
		for _, in := range tx.Inputs {
			if in.IsIssuance() {
				continue
			}
			tx, ok := m.poolMap[in.Previous.Hash]
			if !ok {
				continue
			}
			if in.Previous.Index >= uint32(len(tx.Outputs)) {
				continue
			}
			o := tx.Outputs[in.Previous.Index]
			prevouts[in.Previous] = state.NewOutput(*o, in.Previous)
		}
	}
	return prevouts, nil
}

// Dump returns all pending transactions in the pool.
func (m *MemPool) Dump(context.Context) ([]*bc.Tx, error) {
	return m.pool[:len(m.pool):len(m.pool)], nil
}

// Clean removes confirmed or conflicting transactions from the pool.
func (m *MemPool) Clean(
	ctx context.Context,
	confirmed,
	conflicting []*bc.Tx,
	assets map[bc.AssetID]*state.AssetState,
) error {
	for _, tx := range append(confirmed, conflicting...) {
		delete(m.poolMap, tx.Hash)
		for i := range m.pool {
			if m.pool[i].Hash == tx.Hash {
				m.pool = append(m.pool[:i], m.pool[i+1:]...)
				break
			}
		}
	}
	return nil
}
