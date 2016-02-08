package appdb_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	. "chain/api/appdb"
	"chain/api/asset/assettest"
	"chain/database/pg/pgtest"
	"chain/fedchain-sandbox/hdkey"
	"chain/testutil"
)

func TestAddressLoadNextIndex(t *testing.T) {
	ctx := pgtest.NewContext(t)
	defer pgtest.Finish(ctx)

	ResetSeqs(ctx, t) // Force predictable values.
	mn := assettest.CreateManagerNodeFixture(ctx, t, "", "", nil, nil)
	acc := assettest.CreateAccountFixture(ctx, t, mn, "", nil)

	exp := time.Now().Add(5 * time.Minute)
	addr := &Address{
		AccountID: acc,
		Amount:    100,
		Expires:   exp,
	}
	err := addr.LoadNextIndex(ctx) // get most fields from the db given AccountID
	if err != nil {
		t.Fatal(err)
	}

	want := &Address{
		AccountID: acc,
		Amount:    100,
		Expires:   exp,

		ManagerNodeID:    mn,
		ManagerNodeIndex: []uint32{0, 1},
		AccountIndex:     []uint32{0, 0},
		Index:            []uint32{0, 1},
		SigsRequired:     1,
		Keys:             []*hdkey.XKey{testutil.TestXPub},
	}

	if !reflect.DeepEqual(addr, want) {
		t.Errorf("addr = %+v want %+v", addr, want)
	}
}

func TestAddressInsert(t *testing.T) {
	t0 := time.Now()
	ctx := pgtest.NewContext(t)
	defer pgtest.Finish(ctx)

	ResetSeqs(ctx, t) // Force predictable values.
	mn := assettest.CreateManagerNodeFixture(ctx, t, "", "", nil, nil)
	acc := assettest.CreateAccountFixture(ctx, t, mn, "", nil)

	addr := &Address{
		AccountID:        acc,
		Amount:           100,
		Expires:          t0.Add(5 * time.Minute),
		ManagerNodeID:    mn,
		ManagerNodeIndex: []uint32{0, 1},
		AccountIndex:     []uint32{0, 0},
		Index:            []uint32{0, 0},
		SigsRequired:     1,
		Keys:             []*hdkey.XKey{testutil.TestXPub},

		RedeemScript: []byte{},
		PKScript:     []byte{},
	}

	err := addr.Insert(ctx) // get most fields from the db given AccountID
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(addr.ID, "a") {
		t.Errorf("ID = %q want prefix 'a'", addr.ID)
	}
	if addr.Created.Before(t0) {
		t.Errorf("Created = %v want after %v", addr.Created, t0)
	}
}

var dummyXPub2, _ = hdkey.NewXKey("xpub661MyMwAqRbcFoBSqmqxsAGLAgoLBDHXgZutXooGvHGKXgqPK9HYiVZNoqhGuwzeFW27JBpgZZEabMZhFHkxehJmT8H3AfmfD4zhniw5jcw")

func TestCreateAddress(t *testing.T) {
	t0 := time.Now()
	ctx := pgtest.NewContext(t)
	defer pgtest.Finish(ctx)

	ResetSeqs(ctx, t) // Force predictable values.
	mn0 := assettest.CreateManagerNodeFixture(ctx, t, "", "foo", []*hdkey.XKey{dummyXPub2}, nil)
	acc0 := assettest.CreateAccountFixture(ctx, t, mn0, "foo", nil)

	exp := t0.Add(5 * time.Minute)
	addr := &Address{
		AccountID: acc0,
		Amount:    100,
		Expires:   exp,
	}

	err := CreateAddress(ctx, addr, true)
	if err != nil {
		t.Fatal(err)
	}

	want := &Address{
		AccountID:        acc0,
		Amount:           100,
		Expires:          exp,
		ManagerNodeID:    mn0,
		ManagerNodeIndex: []uint32{0, 1},
		AccountIndex:     []uint32{0, 0},
		Index:            []uint32{0, 1},
		SigsRequired:     1,
		Keys:             []*hdkey.XKey{dummyXPub2},

		RedeemScript: []byte{
			81, 33, 2, 241, 154, 202, 111, 123, 48, 123, 116, 244, 53,
			11, 207, 218, 165, 175, 26, 38, 65, 147, 76, 125, 77, 183,
			254, 50, 18, 62, 238, 216, 139, 92, 16, 81, 174,
		},
		PKScript: []byte{
			118, 169, 20, 209, 12, 223, 249, 230, 16, 228, 14, 42, 205, 213,
			7, 90, 164, 51, 115, 60, 99, 212, 242, 136, 201,
		},
	}

	if !strings.HasPrefix(addr.ID, "a") {
		t.Errorf("ID = %q want prefix 'a'", addr.ID)
	}
	addr.ID = ""
	if addr.Created.Before(t0) {
		t.Errorf("Created = %v want after %v", addr.Created, t0)
	}
	addr.Created = time.Time{}
	if !reflect.DeepEqual(addr, want) {
		t.Errorf("addr = %+v want %+v", addr, want)
	}
}
