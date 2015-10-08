package appdb

import (
	"database/sql"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/lib/pq"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
	"chain/metrics"
	"chain/strings"
)

// Address represents a blockchain address that is
// contained in a bucket.
type Address struct {
	// Initialized by Insert
	// (Insert reads all other fields)
	ID      string
	Created time.Time

	// Initialized by the package client
	Address      string // base58-encoded
	RedeemScript []byte
	PKScript     []byte
	Amount       uint64
	Expires      time.Time
	BucketID     string // read by LoadNextIndex
	IsChange     bool

	// Initialized by LoadNextIndex
	WalletID     string
	WalletIndex  []uint32
	BucketIndex  []uint32
	Index        []uint32
	SigsRequired int
	Keys         []*hdkey.XKey
}

var (
	// Map bucket ID to address template.
	// Entries set the following fields:
	//   WalletID
	//   WalletIndex
	//   BucketIndex
	//   Keys
	//   SigsRequired
	addrInfo      = map[string]*Address{}
	addrIndexNext int64 // next addr index in our block
	addrIndexCap  int64 // points to end of block
	addrMu        sync.Mutex
)

func nextIndex(ctx context.Context) ([]uint32, error) {
	defer metrics.RecordElapsed(time.Now())
	addrMu.Lock()
	defer addrMu.Unlock()

	if addrIndexNext >= addrIndexCap {
		var cap int64
		const incrby = 10000 // address_index_seq increments by 10,000
		const q = `SELECT nextval('address_index_seq')`
		err := pg.FromContext(ctx).QueryRow(q).Scan(&cap)
		if err != nil {
			return nil, errors.Wrap(err, "scan")
		}
		addrIndexCap = cap
		addrIndexNext = cap - incrby
	}

	n := addrIndexNext
	addrIndexNext++
	return keyIndex(n), nil
}

func keyIndex(n int64) []uint32 {
	index := make([]uint32, 2)
	index[0] = uint32(n >> 31)
	index[1] = uint32(n & 0x7fffffff)
	return index
}

// LoadNextIndex is a low-level function to initialize a new Address.
// It is intended to be used by the asset package.
// Field BucketID must be set.
// LoadNextIndex will initialize some other fields;
// See Address for which ones.
func (a *Address) LoadNextIndex(ctx context.Context) error {
	defer metrics.RecordElapsed(time.Now())

	addrMu.Lock()
	ai, ok := addrInfo[a.BucketID]
	addrMu.Unlock()
	if !ok {
		// Concurrent cache misses might be doing
		// duplicate loads here, but ok.
		ai = new(Address)
		var xpubs []string
		const q = `
		SELECT
			w.id, key_index(b.key_index), key_index(w.key_index),
			r.keyset, w.sigs_required
		FROM buckets b
		LEFT JOIN wallets w ON w.id=b.wallet_id
		LEFT JOIN rotations r ON r.id=w.current_rotation
		WHERE b.id=$1
	`
		err := pg.FromContext(ctx).QueryRow(q, a.BucketID).Scan(
			&ai.WalletID,
			(*pg.Uint32s)(&ai.BucketIndex),
			(*pg.Uint32s)(&ai.WalletIndex),
			(*pg.Strings)(&xpubs),
			&ai.SigsRequired,
		)
		if err == sql.ErrNoRows {
			err = pg.ErrUserInputNotFound
		}
		if err != nil {
			return errors.WithDetailf(err, "bucket %s", a.BucketID)
		}

		ai.Keys, err = xpubsToKeys(xpubs)
		if err != nil {
			return errors.Wrap(err, "parsing keys")
		}

		addrMu.Lock()
		addrInfo[a.BucketID] = ai
		addrMu.Unlock()
	}

	var err error
	a.Index, err = nextIndex(ctx)
	if err != nil {
		return errors.Wrap(err, "nextIndex")
	}

	a.WalletID = ai.WalletID
	a.BucketIndex = ai.BucketIndex
	a.WalletIndex = ai.WalletIndex
	a.SigsRequired = ai.SigsRequired
	a.Keys = ai.Keys
	return nil
}

// Insert is a low-level function to insert an Address record.
// It is intended to be used by the asset package.
// Insert will initialize fields ID and Created;
// all other fields must be set prior to calling Insert.
func (a *Address) Insert(ctx context.Context) error {
	defer metrics.RecordElapsed(time.Now())
	const q = `
		INSERT INTO addresses (
			address, redeem_script, pk_script, wallet_id, bucket_id,
			keyset, expiration, amount, key_index, is_change
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, to_key_index($9), $10)
		RETURNING id, created_at;
	`
	row := pg.FromContext(ctx).QueryRow(q,
		a.Address,
		a.RedeemScript,
		a.PKScript,
		a.WalletID,
		a.BucketID,
		pg.Strings(keysToXPubs(a.Keys)),
		pq.NullTime{Time: a.Expires, Valid: !a.Expires.IsZero()},
		a.Amount,
		pg.Uint32s(a.Index),
		a.IsChange,
	)
	return row.Scan(&a.ID, &a.Created)
}

// AddressesByID loads an array of addresses
// from the database using their IDs.
func AddressesByID(ctx context.Context, ids []string) ([]*Address, error) {
	defer metrics.RecordElapsed(time.Now())
	sort.Strings(ids)
	ids = strings.Uniq(ids)

	const q = `
		SELECT a.id, w.id, w.sigs_required, a.redeem_script,
			key_index(w.key_index), key_index(b.key_index), key_index(a.key_index),
			a.keyset
		FROM addresses a
		JOIN buckets b ON b.id=a.bucket_id
		JOIN wallets w ON w.id=a.wallet_id
		WHERE a.id=ANY($1)
	`

	rows, err := pg.FromContext(ctx).Query(q, pg.Strings(ids))
	if err != nil {
		return nil, errors.Wrap(err, "select")
	}
	defer rows.Close()

	var addrs []*Address
	for rows.Next() {
		var (
			a     Address
			xpubs []string
		)
		err = rows.Scan(
			&a.ID, &a.WalletID, &a.SigsRequired, &a.RedeemScript,
			(*pg.Uint32s)(&a.WalletIndex), (*pg.Uint32s)(&a.BucketIndex), (*pg.Uint32s)(&a.Index),
			(*pg.Strings)(&xpubs),
		)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		a.Keys, err = xpubsToKeys(xpubs)
		if err != nil {
			return nil, errors.Wrap(err, "parsing keys")
		}

		addrs = append(addrs, &a)
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	if len(addrs) < len(ids) {
		return nil, errors.Wrapf(errors.New("missing address"), "from set %v", ids)
	}

	return addrs, nil
}
