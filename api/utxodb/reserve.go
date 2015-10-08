package utxodb

import (
	"container/heap"
	"errors"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/context"

	"chain/fedchain-sandbox/wire"
	"chain/log"
	"chain/metrics"
)

var (
	// ErrInsufficient indicates the bucket doesn't contain enough
	// units of the requested asset to satisfy the reservation.
	// New units must be deposited into the bucket in order to
	// satisfy the request; change will not be sufficient.
	ErrInsufficient = errors.New("reservation found insufficient funds")

	// ErrReserved indicates that a reservation could not be
	// satisfied because some of the outputs were already reserved.
	// If those reservations are allowed to expire, the same
	// request will succeed later. Even if those existing
	// reservations are finalized into a transaction, it is
	// still possible (but not guaranteed) that new change outputs
	// will be created in sufficient amounts to satisfy the request.
	ErrReserved = errors.New("reservation found outputs already reserved")
)

type (
	Reserver struct {
		db DB

		mu  sync.Mutex // protects the following
		tab map[key]*pool
	}

	key struct{ BucketID, AssetID string }

	// TODO(kr): see if we can avoid storing
	// BucketID and AssetID in UTXO

	// TODO(kr): try interning strings in UTXO

	UTXO struct {
		BucketID string
		AssetID  string
		Amount   uint64

		ResvExpires time.Time

		AddressID string
		Outpoint  wire.OutPoint

		heapIndex int
	}

	// Change represents reserved units beyond what was asked for.
	// Total reservation is for Amount+Input.Amount.
	Change struct {
		Input  Input
		Amount uint64
	}

	Input struct {
		AssetID  string `json:"asset_id"`
		BucketID string `json:"account_id"`
		TxID     string `json:"transaction_id"`
		Amount   uint64
	}

	DB interface {
		// LoadUTXOs loads the set of UTXOs
		// available to reserve
		// for the given asset in the given bucket.
		LoadUTXOs(ctx context.Context, bucketID, assetID string) ([]*UTXO, error)

		// SaveReservations stores the reservation expiration
		// time in the database for the given UTXOs.
		SaveReservations(ctx context.Context, u []*UTXO, expires time.Time) error

		// ApplyTx applies the Tx to the database,
		// deleteing spent outputs and inserting new UTXOs.
		// It returns the deleted and inserted outputs.
		ApplyTx(context.Context, *wire.MsgTx) (deleted, inserted []*UTXO, err error)
	}
)

func New(db DB) *Reserver {
	return &Reserver{
		db:  db,
		tab: make(map[key]*pool),
	}
}

// pool returns the pool for the given bucket and asset,
// creating it if necessary.
func (rs *Reserver) pool(bucketID, assetID string) *pool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	k := key{bucketID, assetID}
	p, ok := rs.tab[k]
	if !ok {
		p = &pool{
			byOutpoint: map[wire.OutPoint]*UTXO{},
		}
		rs.tab[k] = p
	}
	return p
}

func (rs *Reserver) Reserve(ctx context.Context, inputs []Input, ttl time.Duration) (u []*UTXO, c []Change, err error) {
	defer metrics.RecordElapsed(time.Now())

	var reserved []*UTXO
	var change []Change
	defer func() {
		if err != nil {
			u = nil
			c = nil
			rs.unreserve(reserved)
		}
	}()

	now := time.Now().UTC()
	exp := now.Add(ttl)

	sort.Sort(byKey(inputs))
	for _, in := range inputs {
		p := rs.pool(in.BucketID, in.AssetID)
		err := p.init(ctx, rs.db, key{in.BucketID, in.AssetID})
		if err != nil {
			return nil, nil, err
		}
		res, err := p.reserve(in, now, exp)
		if err != nil {
			return nil, nil, err
		}
		reserved = append(reserved, res...)
		if n := sum(res); n > in.Amount {
			change = append(change, Change{in, n - in.Amount})
		}
	}

	if ttl > 2*time.Minute {
		err = rs.db.SaveReservations(ctx, reserved, exp)
	}
	return reserved, change, err
}

// Cancel cancels the given reservations, if they still exist.
// If any do not exist (if they've already been consumed
// or canceled), it silently ignores them.
func (rs *Reserver) Cancel(ctx context.Context, outpoints []wire.OutPoint) {
	var utxos []*UTXO
	for _, op := range outpoints {
		if u := rs.findReservation(op); u != nil {
			utxos = append(utxos, u)
		}
	}
	rs.unreserve(utxos)
}

func (rs *Reserver) Apply(ctx context.Context, tx *wire.MsgTx) error {
	defer metrics.RecordElapsed(time.Now())
	deleted, inserted, err := rs.db.ApplyTx(ctx, tx)
	if err != nil {
		return err
	}
	rs.delete(deleted)
	sort.Sort(byKeyUTXO(inserted))
	rs.insert(inserted)
	return nil
}

// findReservation does a linear scan through the set
// of pools in rs to find the UTXO that reserves op.
// If there is no such reservation, it returns nil.
func (rs *Reserver) findReservation(op wire.OutPoint) *UTXO {
	// TODO(kr): augment the SDK to include bucket ID and asset ID
	// for each reservation, so we can do this lookup faster.
	defer metrics.RecordElapsed(time.Now())
	var keys []key
	rs.mu.Lock()
	for k := range rs.tab {
		keys = append(keys, k)
	}
	rs.mu.Unlock()

	for _, k := range keys {
		p := rs.pool(k.BucketID, k.AssetID)
		if u := p.findReservation(op); u != nil {
			return u
		}
	}
	return nil
}

// mappool finds the pool for each element of utxos
// and calls f.
// It holds the pool's lock when it calls f,
// so f can modify the pool outputs list and u.
// f must preserve the heap invariant for p.outputs.
func (rs *Reserver) mappool(utxos []*UTXO, f func(*pool, *UTXO)) {
	var prev *pool
	for _, u := range utxos {
		p := rs.pool(u.BucketID, u.AssetID)
		if p != prev {
			p.mu.Lock()
			if prev != nil {
				prev.mu.Unlock()
			}
			prev = p
		}
		f(p, u)
	}
	if prev != nil {
		prev.mu.Unlock()
	}
}

func (rs *Reserver) insert(utxos []*UTXO) {
	ctx := context.TODO()
	var i int64
	rs.mappool(utxos, func(p *pool, u *UTXO) {
		heap.Push(&p.outputs, u)
		p.byOutpoint[u.Outpoint] = u
		i++
		if i%1e6 == 0 {
			log.Messagef(ctx, "build utxo heaps: did %d so far", i)
		}
	})
}

func (rs *Reserver) unreserve(utxos []*UTXO) {
	sort.Sort(byKeyUTXO(utxos))
	rs.mappool(utxos, func(p *pool, u *UTXO) {
		u.ResvExpires = time.Time{}
		heap.Fix(&p.outputs, u.heapIndex)
	})
}

func (rs *Reserver) delete(utxos []*UTXO) {
	sort.Sort(byKeyUTXO(utxos))
	rs.mappool(utxos, (*pool).delete)
}

func sum(utxos []*UTXO) (total uint64) {
	for _, u := range utxos {
		total += u.Amount
	}
	return
}
