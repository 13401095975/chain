package appdb

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/database/pg/pgtest"
)

var (
	dummyXPub, _ = NewKey("xpub661MyMwAqRbcFoBSqmqxsAGLAgoLBDHXgZutXooGvHGKXgqPK9HYiVZNoqhGuwzeFW27JBpgZZEabMZhFHkxehJmT8H3AfmfD4zhniw5jcw")
)

func TestKeyIndexSQL(t *testing.T) {
	pairs := []struct {
		encoded int64
		decoded []uint32
	}{
		{1, []uint32{0, 1}},
		{2, []uint32{0, 2}},
		{0x80000000, []uint32{1, 0}},
		{0x80000001, []uint32{1, 1}},
		{0x100000000, []uint32{2, 0}},
	}

	for _, pair := range pairs {
		var got []uint32

		err := db.QueryRow(`SELECT key_index($1)`, pair.encoded).Scan((*pg.Uint32s)(&got))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			continue
		}
		if !reflect.DeepEqual(got, pair.decoded) {
			t.Errorf("key_index(%d) = %v want %v", pair.encoded, got, pair.decoded)
		}

		var got2 int64

		err = db.QueryRow(`SELECT to_key_index($1::int[])`, pg.Uint32s(pair.decoded)).Scan(&got2)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			continue
		}
		if got2 != pair.encoded {
			t.Errorf("to_key_index(%v) = %d want %d", pair.decoded, got, pair.encoded)
		}
	}
}

func TestUpsertKeys(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)
	err := upsertKeys(ctx, dummyXPub)
	if err != nil {
		t.Errorf("upsertKeys(%v) error: %v", dummyXPub, err)
	}
	err = upsertKeys(ctx, dummyXPub)
	if err != nil {
		t.Errorf("upsertKeys(%v) error: %v", dummyXPub, err)
	}
}
