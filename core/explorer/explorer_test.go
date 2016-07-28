package explorer

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"

	"chain/core/asset/assettest"
	"chain/core/issuer"
	"chain/core/txdb"
	"chain/cos"
	"chain/cos/bc"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/testutil"
)

const (
	blockHash2 = "4aa7e0df4a7332ad09039ca7bbc7298de74d4f28792042dbc12140ee2c71f9ac"
	blockHash1 = "3250d2426527ad63fcbdde790fd92d5b50f53a8aeb1f25179ae6dbf958684592"
)

var (
	otherAssetID = bc.AssetID{0xde, 0xad, 0xbe, 0xef}
)

func mustParseHash(str string) bc.Hash {
	hash, err := bc.ParseHash(str)
	if err != nil {
		panic(err)
	}
	return hash
}

func TestListBlocks(t *testing.T) {
	ctx := context.Background()
	_, db := pgtest.NewDB(t, pgtest.SchemaPath)
	store, pool := txdb.New(db)
	fc, err := cos.NewFC(ctx, store, pool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(fc, db, store, true)

	pgtest.Exec(pg.NewContext(ctx, db), t, `
		INSERT INTO blocks(block_hash, height, data, header)
		VALUES(
			$1,
			1,
			decode('01010000000000000000000000000000000000000000000000000000000000000000006400000107000000000000', 'hex'),
			''
		), (
			$2,
			2,
			decode('0102000000000000000000000000000000000000000000000000000000000000000000690000020700000000000007000000000000', 'hex'),
			''
		);
	`, blockHash1, blockHash2)

	cases := []struct {
		prev     string
		limit    int
		want     []ListBlocksItem
		wantLast string
	}{{
		prev:  "",
		limit: 50,
		want: []ListBlocksItem{{
			ID:      mustParseHash(blockHash2),
			Height:  2,
			Time:    time.Unix(0, 105*int64(time.Millisecond)).UTC(),
			TxCount: 2,
		}, {
			ID:      mustParseHash(blockHash1),
			Height:  1,
			Time:    time.Unix(0, 100*int64(time.Millisecond)).UTC(),
			TxCount: 1,
		}},
		wantLast: "",
	}, {
		prev:  "2",
		limit: 50,
		want: []ListBlocksItem{{
			ID:      mustParseHash(blockHash1),
			Height:  1,
			Time:    time.Unix(0, 100*int64(time.Millisecond)).UTC(),
			TxCount: 1,
		}},
		wantLast: "",
	}, {
		prev:  "",
		limit: 1,
		want: []ListBlocksItem{{
			ID:      mustParseHash(blockHash2),
			Height:  2,
			Time:    time.Unix(0, 105*int64(time.Millisecond)).UTC(),
			TxCount: 2,
		}},
		wantLast: "2",
	}, {
		prev:     "1",
		limit:    50,
		want:     nil,
		wantLast: "",
	}}
	for _, c := range cases {
		got, gotLast, err := e.ListBlocks(ctx, c.prev, c.limit)
		if err != nil {
			t.Errorf("ListBlocks(%v, %v) unexpected err = %q", c.prev, c.limit, err)
			continue
		}

		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("got ListBlocks(%v, %v) = %+v want %+v", c.prev, c.limit, got, c.want)
		}
		if gotLast != c.wantLast {
			t.Errorf("got ListBlocks(%v, %v) last = %q want %q", c.prev, c.limit, gotLast, c.wantLast)
		}
	}
}

func TestGetBlockSummary(t *testing.T) {
	ctx := context.Background()
	_, db := pgtest.NewDB(t, pgtest.SchemaPath)
	store, pool := txdb.New(db)
	fc, err := cos.NewFC(ctx, store, pool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := New(fc, db, store, true)

	blockHash := "4aa7e0df4a7332ad09039ca7bbc7298de74d4f28792042dbc12140ee2c71f9ac"
	pgtest.Exec(pg.NewContext(ctx, db), t, `
		INSERT INTO blocks(block_hash, height, data, header)
		VALUES(
			$1,
			2,
			decode('0102000000000000000000000000000000000000000000000000000000000000000000690000020700000000000007000000000000', 'hex'),
			''
		);
	`, blockHash)

	got, err := e.GetBlockSummary(ctx, blockHash)
	if err != nil {
		t.Fatal(err)
	}
	want := &BlockSummary{
		ID:      mustParseHash(blockHash),
		Height:  2,
		Time:    time.Unix(0, 105*int64(time.Millisecond)).UTC(),
		TxCount: 2,
		TxHashes: []bc.Hash{
			mustParseHash("39e746dc19f9ee593d9f5b776c8f08bac2181c6375a21522cd99149f4260bbd9"),
			mustParseHash("39e746dc19f9ee593d9f5b776c8f08bac2181c6375a21522cd99149f4260bbd9"),
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got block header:\n\t%+v\nwant:\n\t%+v", got, want)
	}
}

func TestGetTxIssuance(t *testing.T) {
	ctx := context.Background()
	_, db := pgtest.NewDB(t, pgtest.SchemaPath)
	dbctx := pg.NewContext(ctx, db)
	store, pool := txdb.New(db) // TODO(kr): use memstore and mempool
	fc, err := cos.NewFC(ctx, store, pool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(fc, db, store, true)
	inodeID := assettest.CreateIssuerNodeFixture(dbctx, t, "", "", nil, nil)
	assetDef := map[string]interface{}{"c": "d"}
	assetObj, err := issuer.CreateAsset(dbctx, inodeID, "label", bc.Hash{}, assetDef, nil)
	if err != nil {
		t.Fatal(err)
	}

	assetDefStr, err := json.Marshal(assetDef)
	if err != nil {
		t.Fatal(err)
	}
	refData := []byte(`{"a":"b"}`)

	now := time.Now().UTC()

	tx := bc.NewTx(bc.TxData{
		Inputs: []*bc.TxInput{
			bc.NewIssuanceInput(now, now.Add(time.Hour), bc.Hash{}, 0, assetObj.IssuanceScript, assetDefStr, refData, [][]byte{assetObj.RedeemScript}),
		},
		Outputs: []*bc.TxOutput{
			bc.NewTxOutput(assetObj.Hash, 5, []byte("addr0"), []byte{2}),
			bc.NewTxOutput(assetObj.Hash, 6, []byte("addr1"), nil),
		},
		Metadata: []byte{0},
	})

	blk := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:      1,
			TimestampMS: uint64(now.UnixNano() / int64(time.Millisecond)),
		},
		Transactions: []*bc.Tx{tx},
	}

	err = pool.Insert(ctx, tx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	err = store.SaveBlock(ctx, blk)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, err := e.GetTx(ctx, tx.Hash.String())
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	bh := blk.Hash()

	want := &Tx{
		ID:          tx.Hash,
		BlockID:     &bh,
		BlockHeight: 1,
		BlockTime:   now.Truncate(time.Millisecond),
		Metadata:    []byte{0},
		Inputs: []*TxInput{{
			Type:     "issuance",
			AssetID:  assetObj.Hash,
			Metadata: []byte(`{"a":"b"}`),
			AssetDef: []byte(`{"c":"d"}`),
		}},
		Outputs: []*TxOutput{{
			AssetID:  assetObj.Hash,
			Amount:   5,
			Address:  []byte("addr0"),
			Script:   []byte("addr0"),
			Metadata: []byte{2},
		}, {
			AssetID: assetObj.Hash,
			Amount:  6,
			Address: []byte("addr1"),
			Script:  []byte("addr1"),
		}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s", spew.Sdump(got), spew.Sdump(want))
	}
}

func TestGetTxTransfer(t *testing.T) {
	ctx := context.Background()
	_, db := pgtest.NewDB(t, pgtest.SchemaPath)
	store, pool := txdb.New(db) // TODO(kr): use memstore and mempool
	fc, err := cos.NewFC(ctx, store, pool, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := New(fc, db, store, true)

	assetID1 := bc.AssetID([32]byte{1})
	assetID2 := bc.AssetID([32]byte{2})

	prevTxs := []*bc.Tx{
		bc.NewTx(bc.TxData{
			Outputs: []*bc.TxOutput{
				bc.NewTxOutput(assetID1, 5, nil, nil),
			},
		}),
		bc.NewTx(bc.TxData{
			Outputs: []*bc.TxOutput{
				{},
				bc.NewTxOutput(assetID2, 6, nil, nil),
			},
		}),
	}
	tx := bc.NewTx(bc.TxData{
		Inputs: []*bc.TxInput{
			bc.NewSpendInput(prevTxs[0].Hash, 0, nil, assetID1, 5, nil, nil),
			bc.NewSpendInput(prevTxs[1].Hash, 1, nil, assetID2, 6, nil, nil),
		},
		Outputs: []*bc.TxOutput{
			bc.NewTxOutput(bc.AssetID([32]byte{1}), 5, []byte("addr0"), nil),
			bc.NewTxOutput(bc.AssetID([32]byte{2}), 6, []byte("addr1"), nil),
		},
	})

	now := time.Now().UTC()
	blk := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:      1,
			TimestampMS: uint64(now.UnixNano() / int64(time.Millisecond)),
		},
		Transactions: append(prevTxs, tx),
	}

	err = store.SaveBlock(ctx, blk)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, err := e.GetTx(ctx, tx.Hash.String())
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	var blkHash = blk.Hash()

	zero, one := uint32(0), uint32(1)
	five, six := uint64(5), uint64(6)
	h0, h1 := prevTxs[0].Hash, prevTxs[1].Hash

	want := &Tx{
		ID:          tx.Hash,
		BlockID:     &blkHash,
		BlockHeight: 1,
		BlockTime:   now.Truncate(time.Millisecond),
		Inputs: []*TxInput{{
			Type:    "transfer",
			AssetID: bc.AssetID([32]byte{1}),
			Amount:  &five,
			TxHash:  &h0,
			TxOut:   &zero,
		}, {
			Type:    "transfer",
			AssetID: bc.AssetID([32]byte{2}),
			Amount:  &six,
			TxHash:  &h1,
			TxOut:   &one,
		}},
		Outputs: []*TxOutput{{
			AssetID: bc.AssetID([32]byte{1}),
			Amount:  5,
			Address: []byte("addr0"),
			Script:  []byte("addr0"),
		}, {
			AssetID: bc.AssetID([32]byte{2}),
			Amount:  6,
			Address: []byte("addr1"),
			Script:  []byte("addr1"),
		}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n\t%+v\nwant:\n\t%+v", got, want)
	}
}

func TestGetAssets(t *testing.T) {
	ctx := context.Background()
	dbtx := pgtest.NewTx(t)
	dbctx := pg.NewContext(ctx, dbtx)
	fc, g, err := assettest.InitializeSigningGenerator(dbctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := New(fc, dbtx, nil, true)

	in0 := assettest.CreateIssuerNodeFixture(dbctx, t, "", "in-0", nil, nil)

	asset0 := assettest.CreateAssetFixture(dbctx, t, in0, "asset-0", "def-0")
	asset1 := assettest.CreateAssetFixture(dbctx, t, in0, "asset-1", "def-1")

	def0 := []byte("{\n  \"s\": \"def-0\"\n}")
	defPtr0 := bc.HashAssetDefinition(def0).String()

	assettest.IssueAssetsFixture(dbctx, t, asset0, 58, "")

	_, err = g.MakeBlock(dbctx)
	if err != nil {
		t.Fatal(err)
	}

	assettest.IssueAssetsFixture(dbctx, t, asset0, 12, "")
	assettest.IssueAssetsFixture(dbctx, t, asset1, 10, "")

	assets, err := e.GetAssets(ctx, []bc.AssetID{
		asset0,
		asset1,
		otherAssetID,
	})
	if err != nil {
		testutil.FatalErr(t, err)
	}

	got := make(map[string]*Asset, len(assets))
	for id, asset := range assets {
		got[id.String()] = asset
	}
	want := map[string]*Asset{
		asset0.String(): &Asset{
			ID:            asset0,
			DefinitionPtr: defPtr0,
			Definition:    def0,
			Issued:        58,
		},
	}
	if !reflect.DeepEqual(got, want) {
		g, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			testutil.FatalErr(t, err)
		}
		w, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			testutil.FatalErr(t, err)
		}
		t.Errorf("assets:\ngot:  %v\nwant: %v", string(g), string(w))
	}
}

func TestGetAsset(t *testing.T) {
	ctx := context.Background()
	dbtx := pgtest.NewTx(t)
	dbctx := pg.NewContext(ctx, dbtx)
	fc, g, err := assettest.InitializeSigningGenerator(dbctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := New(fc, dbtx, nil, true)

	in0 := assettest.CreateIssuerNodeFixture(dbctx, t, "", "in-0", nil, nil)

	asset0 := assettest.CreateAssetFixture(dbctx, t, in0, "asset-0", "def-0")
	asset1 := assettest.CreateAssetFixture(dbctx, t, in0, "asset-1", "def-1")

	def0 := []byte("{\n  \"s\": \"def-0\"\n}")
	defPtr0 := bc.HashAssetDefinition(def0).String()

	assettest.IssueAssetsFixture(dbctx, t, asset0, 58, "")

	_, err = g.MakeBlock(dbctx)
	if err != nil {
		t.Fatal(err)
	}

	assettest.IssueAssetsFixture(dbctx, t, asset0, 12, "")
	assettest.IssueAssetsFixture(dbctx, t, asset1, 10, "")

	examples := []struct {
		id      bc.AssetID
		wantErr error
		want    *Asset
	}{
		{
			id: asset0,
			want: &Asset{
				ID:            asset0,
				DefinitionPtr: defPtr0,
				Definition:    def0,
				Issued:        58,
			},
		},

		// Issued, but not landed in block yet
		{
			id:      asset1,
			wantErr: pg.ErrUserInputNotFound,
		},

		// Missing asset
		{
			id:      otherAssetID,
			wantErr: pg.ErrUserInputNotFound,
		},
	}

	for _, ex := range examples {
		t.Log("Example", ex.id)

		got, err := e.GetAsset(ctx, ex.id)
		if errors.Root(err) != ex.wantErr {
			t.Fatalf("error:\ngot:  %v\nwant: %v", errors.Root(err), ex.wantErr)
		}

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("got:\n\t%+v\nwant:\n\t%+v", got, ex.want)
		}
	}
}
