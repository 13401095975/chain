package asset

import (
	"context"
	"reflect"
	"testing"

	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/protocol/bc"
	"chain/testutil"
)

func TestDefineAsset(t *testing.T) {
	ctx := pg.NewContext(context.Background(), pgtest.NewTx(t))
	keys := []string{testutil.TestXPub.String()}
	var initialBlockHash bc.Hash
	asset, err := Define(ctx, keys, 1, nil, initialBlockHash, "", nil, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	if asset.sortID == "" {
		t.Error("asset.sortID empty")
	}

	// Verify that the asset was defined.
	var id string
	var checkQ = `SELECT id FROM assets`
	err = pg.QueryRow(ctx, checkQ).Scan(&id)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if id != asset.AssetID.String() {
		t.Errorf("expected new asset %s to be recorded as %s", asset.AssetID.String(), id)
	}
}

func TestDefineAssetIdempotency(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	token := "test_token"
	keys := []string{testutil.TestXPub.String()}
	var initialBlockHash bc.Hash
	asset0, err := Define(ctx, keys, 1, nil, initialBlockHash, "", nil, &token)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	asset1, err := Define(ctx, keys, 1, nil, initialBlockHash, "", nil, &token)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	// asset0 and asset1 should be exactly the same because they use the same client token
	if !reflect.DeepEqual(asset0, asset1) {
		t.Errorf("expected %v and %v to match", asset0, asset1)
	}
}

func TestFindAssetByID(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	keys := []string{testutil.TestXPub.String()}
	var initialBlockHash bc.Hash
	asset, err := Define(ctx, keys, 1, nil, initialBlockHash, "", nil, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	found, err := findByID(ctx, asset.AssetID)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	if !reflect.DeepEqual(asset, found) {
		t.Errorf("expected %v and %v to match", asset, found)
	}
}

func TestAssetByClientToken(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	keys := []string{testutil.TestXPub.String()}
	token := "test_token"
	var initialBlockHash bc.Hash

	asset, err := Define(ctx, keys, 1, nil, initialBlockHash, "", nil, &token)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	found, err := assetByClientToken(ctx, token)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	if found.AssetID != asset.AssetID {
		t.Fatalf("assetByClientToken(\"test_token\")=%x, want %x", found.AssetID[:], asset.AssetID[:])
	}
}
