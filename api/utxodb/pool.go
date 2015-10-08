package utxodb

import (
	"container/heap"
	"sync"
	"time"

	"golang.org/x/net/context"

	"chain/fedchain-sandbox/wire"
	"chain/metrics"
)

// A pool holds outputs of a single asset type in a bucket.
type pool struct {
	mu         sync.Mutex // protects the following
	ready      bool
	outputs    utxosByResvExpires // min heap
	byOutpoint map[wire.OutPoint]*UTXO
}

func (p *pool) init(ctx context.Context, db DB, k key) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ready {
		return nil
	}

	defer metrics.RecordElapsed(time.Now())

	// The pool might have collected some utxos
	// from Apply before we acquired p.mu here.
	// We will load them (along with all the other
	// utxos) again now, so throw away the dups.
	p.outputs = nil
	p.byOutpoint = map[wire.OutPoint]*UTXO{}

	utxos, err := db.LoadUTXOs(ctx, k.BucketID, k.AssetID)
	if err != nil {
		return err
	}

	for _, utxo := range utxos {
		heap.Push(&p.outputs, utxo)
		p.byOutpoint[utxo.Outpoint] = utxo
	}
	p.ready = true
	return nil
}

// reserve reserves UTXOs from p to satisfy in and returns them.
// If the input can't be satisfied, it returns nil.
func (p *pool) reserve(in Input, now, exp time.Time) ([]*UTXO, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer metrics.RecordElapsed(time.Now())

	// Put all collected utxos back in the heap,
	// no matter what. They may or may not have
	// ResvExpires changed.
	var utxos []*UTXO
	defer func() {
		for _, utxo := range utxos {
			heap.Push(&p.outputs, utxo)
		}
	}()

	// TODO(kr): handle reserve-by-txid

	var total uint64
	countingReserved := false
	for len(p.outputs) > 0 {
		u := heap.Pop(&p.outputs).(*UTXO)
		total += u.Amount
		utxos = append(utxos, u)
		if u.ResvExpires.After(now) {
			// We cannot satisfy the request now, but we should
			// still check if there's enough money in the bucket,
			// counting reserved outputs. This lets us discriminate
			// between "you don't have enough money" (ErrInsufficient)
			// vs "you might have enough money, but some of it is
			// locked up in a reservation and you have to wait
			// for a new change output before you can spend it"
			// (ErrReserved).
			//
			// If we also kept track of the amount requested,
			// we could give a more precise answer to "should I
			// expect the future change to be enough to cover this
			// request?"
			countingReserved = true
		}
		if total >= in.Amount && countingReserved {
			return nil, ErrReserved
		}
		if total >= in.Amount {
			// Success. Mark the collected utxos
			// with a reservation expiration time.
			for _, utxo := range utxos {
				utxo.ResvExpires = exp
			}
			return utxos, nil
		}
	}
	return nil, ErrInsufficient
}

// delete deletes u's outpoint from p
// if p contains a UTXO with that outpoint.
// The caller must hold p.mu.
func (p *pool) delete(utxo *UTXO) {
	if u := p.byOutpoint[utxo.Outpoint]; u != nil {
		heap.Remove(&p.outputs, u.heapIndex)
		delete(p.byOutpoint, u.Outpoint)
	}
}

// caller must hold p.mu
func (p *pool) contains(u *UTXO) bool {
	i := u.heapIndex
	return i < len(p.outputs) && p.outputs[i] == u
}

// findReservation finds the UTXO in p that reserves op.
// If there is no such reservation, it returns nil.
func (p *pool) findReservation(op wire.OutPoint) *UTXO {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer metrics.RecordElapsed(time.Now())

	u := p.byOutpoint[op]
	if u == nil || time.Now().After(u.ResvExpires) {
		return nil
	}
	return u
}
