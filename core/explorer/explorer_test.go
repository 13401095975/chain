package explorer

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"chain/core/asset"
	"chain/core/asset/assettest"
	"chain/core/generator"
	"chain/core/txbuilder"
	"chain/core/txdb"
	"chain/cos/bc"
	"chain/cos/patricia"
	"chain/cos/txscript"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/database/sql"
	"chain/errors"
	"chain/testutil"
)

const (
	blockHash2 = "a704e7f7ed80a2367bc8d1483ddb97176f1423dee5170cc1e20c38dce6cbccec"
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

func TestHistoricalOutput(t *testing.T) {
	ctx := pgtest.NewContext(t)

	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB))
	fc, err := assettest.InitializeSigningGenerator(ctx, store, pool)
	if err != nil {
		t.Fatal(err)
	}

	Connect(ctx, fc, true, 0, true)

	account1ID := assettest.CreateAccountFixture(ctx, t, "", "", nil)
	account2ID := assettest.CreateAccountFixture(ctx, t, "", "", nil)
	assetID := assettest.CreateAssetFixture(ctx, t, "", "", "")
	assettest.IssueAssetsFixture(ctx, t, assetID, 100, account1ID)

	count := func() int64 {
		const q = `SELECT amount FROM explorer_outputs WHERE asset_id = $1 AND account_id = $2 AND NOT UPPER_INF(timespan)`

		var n int64
		err := pg.ForQueryRows(ctx, q, assetID, account1ID, func(amt int64) {
			n += amt
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := count(); n != 0 {
		t.Errorf("expected 0 historical units, got %d", n)
	}

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if n := count(); n != 0 {
		t.Errorf("expected 0 historical units, got %d", n)
	}

	srcs := []*txbuilder.Source{asset.NewAccountSource(ctx, &bc.AssetAmount{AssetID: assetID, Amount: 10}, account1ID, nil, nil, nil)}
	dests := []*txbuilder.Destination{assettest.AccountDest(ctx, t, account2ID, assetID, 10)}
	assettest.Transfer(ctx, t, srcs, dests)

	if n := count(); n != 0 {
		t.Errorf("expected 0 historical units, got %d", n)
	}

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if n := count(); n != 100 {
		t.Errorf("expected 100 historical units, got %d", n)
	}
}

func TestListBlocks(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store := txdb.NewStore(pg.FromContext(ctx).(*sql.DB))
	pgtest.Exec(ctx, t, `
		INSERT INTO blocks(block_hash, height, data, header)
		VALUES(
			$1,
			1,
			decode('010100000000000000000000000000000000000000000000000000000000000000000064000001070100000003747831', 'hex'),
			''
		), (
			$2,
			2,
			decode('0102b3431f1d6c5aa2746a08d933bab1c5e68df1b18f3a43010f6f247b839d89e1740069000002070100000003747832070100000003747833', 'hex'),
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
			Time:    time.Unix(105, 0).UTC(),
			TxCount: 2,
		}, {
			ID:      mustParseHash(blockHash1),
			Height:  1,
			Time:    time.Unix(100, 0).UTC(),
			TxCount: 1,
		}},
		wantLast: "",
	}, {
		prev:  "2",
		limit: 50,
		want: []ListBlocksItem{{
			ID:      mustParseHash(blockHash1),
			Height:  1,
			Time:    time.Unix(100, 0).UTC(),
			TxCount: 1,
		}},
		wantLast: "",
	}, {
		prev:  "",
		limit: 1,
		want: []ListBlocksItem{{
			ID:      mustParseHash(blockHash2),
			Height:  2,
			Time:    time.Unix(105, 0).UTC(),
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
		got, gotLast, err := ListBlocks(ctx, store, c.prev, c.limit)
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
	ctx := pgtest.NewContext(t)
	store := txdb.NewStore(pg.FromContext(ctx).(*sql.DB))
	pgtest.Exec(ctx, t, `
		INSERT INTO blocks(block_hash, height, data, header)
		VALUES(
			$1,
			2,
			decode('0102b3431f1d6c5aa2746a08d933bab1c5e68df1b18f3a43010f6f247b839d89e1740069000002070100000003747832070100000003747833', 'hex'),
			''
		);
	`, blockHash2)

	got, err := GetBlockSummary(ctx, store, blockHash2)
	if err != nil {
		t.Fatal(err)
	}
	want := &BlockSummary{
		ID:      mustParseHash(blockHash2),
		Height:  2,
		Time:    time.Unix(105, 0).UTC(),
		TxCount: 2,
		TxHashes: []bc.Hash{
			mustParseHash("6dd15ec9e85508b14e1b77ed952e3dddc36a62ada30116cba47f2138f333e896"),
			mustParseHash("c710227b2f40e14e5da6daa908133430f9cf9f2416453fe59c2c200499c842e8"),
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got block header:\n\t%+v\nwant:\n\t%+v", got, want)
	}
}

func TestGetTxIssuance(t *testing.T) {
	ctx := pgtest.NewContext(t)
	assetID, sigScript := mockAssetIDAndSigScript()

	tx := bc.NewTx(bc.TxData{
		Inputs: []*bc.TxInput{{
			Previous:        bc.Outpoint{Index: bc.InvalidOutputIndex},
			SignatureScript: sigScript,
			Metadata:        []byte(`{"a":"b"}`),
			AssetDefinition: []byte(`{"c":"d"}`),
		}},
		Outputs: []*bc.TxOutput{{
			AssetAmount: bc.AssetAmount{AssetID: assetID, Amount: 5},
			Metadata:    []byte{2},
			Script:      []byte("addr0"),
		}, {
			AssetAmount: bc.AssetAmount{AssetID: assetID, Amount: 6},
			Script:      []byte("addr1"),
		}},
		Metadata: []byte{0},
	})

	now := time.Now().UTC().Truncate(time.Second)
	blk := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:    1,
			Timestamp: uint64(now.Unix()),
		},
		Transactions: []*bc.Tx{tx},
	}

	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // TODO(kr): use memstore and mempool

	err := pool.Insert(ctx, tx, nil)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	_, err = store.ApplyBlock(ctx, blk, nil, patricia.NewTree(nil))
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, err := GetTx(ctx, store, pool, tx.Hash.String())
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	bh := blk.Hash()

	want := &Tx{
		ID:          tx.Hash,
		BlockID:     &bh,
		BlockHeight: 1,
		BlockTime:   now,
		Metadata:    []byte{0},
		Inputs: []*TxInput{{
			Type:     "issuance",
			AssetID:  assetID,
			Metadata: []byte(`{"a":"b"}`),
			AssetDef: []byte(`{"c":"d"}`),
		}},
		Outputs: []*TxOutput{{
			AssetID:  assetID,
			Amount:   5,
			Address:  []byte("addr0"),
			Script:   []byte("addr0"),
			Metadata: []byte{2},
		}, {
			AssetID: assetID,
			Amount:  6,
			Address: []byte("addr1"),
			Script:  []byte("addr1"),
		}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n\t%+v\nwant:\n\t%+v", got, want)
	}
}

func TestGetTxTransfer(t *testing.T) {
	ctx := pgtest.NewContext(t)
	prevTxs := []*bc.Tx{
		bc.NewTx(bc.TxData{
			Outputs: []*bc.TxOutput{{
				AssetAmount: bc.AssetAmount{AssetID: bc.AssetID([32]byte{1}), Amount: 5},
			}},
		}),
		bc.NewTx(bc.TxData{
			Outputs: []*bc.TxOutput{{}, {
				AssetAmount: bc.AssetAmount{AssetID: bc.AssetID([32]byte{2}), Amount: 6},
			}},
		}),
	}
	tx := bc.NewTx(bc.TxData{
		Inputs: []*bc.TxInput{{
			Previous: bc.Outpoint{Hash: prevTxs[0].Hash, Index: 0},
		}, {
			Previous: bc.Outpoint{Hash: prevTxs[1].Hash, Index: 1},
		}},
		Outputs: []*bc.TxOutput{{
			AssetAmount: bc.AssetAmount{AssetID: bc.AssetID([32]byte{1}), Amount: 5},
			Script:      []byte("addr0"),
		}, {
			AssetAmount: bc.AssetAmount{AssetID: bc.AssetID([32]byte{2}), Amount: 6},
			Script:      []byte("addr1"),
		}},
	})

	now := time.Now().UTC().Truncate(time.Second)
	blk := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:    1,
			Timestamp: uint64(now.Unix()),
		},
		Transactions: append(prevTxs, tx),
	}

	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // TODO(kr): use memstore
	_, err := store.ApplyBlock(ctx, blk, nil, patricia.NewTree(nil))
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, err := GetTx(ctx, store, pool, tx.Hash.String())
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
		BlockTime:   now,
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
	ctx := pgtest.NewContext(t)
	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // TODO(kr): use memstore and mempool
	_, err := assettest.InitializeSigningGenerator(ctx, store, pool)
	if err != nil {
		t.Fatal(err)
	}

	in0 := assettest.CreateIssuerNodeFixture(ctx, t, "", "in-0", nil, nil)

	asset0 := assettest.CreateAssetFixture(ctx, t, in0, "asset-0", "def-0")
	asset1 := assettest.CreateAssetFixture(ctx, t, in0, "asset-1", "def-1")

	def0 := []byte("{\n  \"s\": \"def-0\"\n}")
	defPtr0 := bc.HashAssetDefinition(def0).String()

	assettest.IssueAssetsFixture(ctx, t, asset0, 58, "")

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}

	assettest.IssueAssetsFixture(ctx, t, asset0, 12, "")
	assettest.IssueAssetsFixture(ctx, t, asset1, 10, "")

	got, err := GetAssets(ctx, []bc.AssetID{
		asset0,
		asset1,
		otherAssetID,
	})
	if err != nil {
		testutil.FatalErr(t, err)
	}

	want := map[bc.AssetID]*Asset{
		asset0: &Asset{
			ID:            asset0,
			DefinitionPtr: defPtr0,
			Definition:    def0,
			Issued:        58,
		},
		asset1: &Asset{
			ID:            asset1,
			DefinitionPtr: "",
			Definition:    nil,
			Issued:        0,
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
	ctx := pgtest.NewContext(t)
	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // TODO(kr): use memstore and mempool
	_, err := assettest.InitializeSigningGenerator(ctx, store, pool)
	if err != nil {
		t.Fatal(err)
	}

	in0 := assettest.CreateIssuerNodeFixture(ctx, t, "", "in-0", nil, nil)

	asset0 := assettest.CreateAssetFixture(ctx, t, in0, "asset-0", "def-0")
	asset1 := assettest.CreateAssetFixture(ctx, t, in0, "asset-1", "def-1")

	def0 := []byte("{\n  \"s\": \"def-0\"\n}")
	defPtr0 := bc.HashAssetDefinition(def0).String()

	assettest.IssueAssetsFixture(ctx, t, asset0, 58, "")

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}

	assettest.IssueAssetsFixture(ctx, t, asset0, 12, "")
	assettest.IssueAssetsFixture(ctx, t, asset1, 10, "")

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

		// Blank definition
		{
			id: asset1,
			want: &Asset{
				ID:            asset1,
				DefinitionPtr: "",
				Definition:    nil,
				Issued:        0,
			},
		},

		// Missing asset
		{
			id:      otherAssetID,
			wantErr: pg.ErrUserInputNotFound,
		},
	}

	for _, ex := range examples {
		t.Log("Example", ex.id)

		got, err := GetAsset(ctx, ex.id)
		if errors.Root(err) != ex.wantErr {
			t.Fatalf("error:\ngot:  %v\nwant: %v", errors.Root(err), ex.wantErr)
		}

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("got:\n\t%+v\nwant:\n\t%+v", got, ex.want)
		}
	}
}

func TestListUTXOsByAsset(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // TODO(kr): use memstore and mempool
	fc, err := assettest.InitializeSigningGenerator(ctx, store, pool)
	if err != nil {
		t.Fatal(err)
	}
	Connect(ctx, fc, true, 0, true)

	projectID := assettest.CreateProjectFixture(ctx, t, "", "")
	issuerNodeID := assettest.CreateIssuerNodeFixture(ctx, t, projectID, "", nil, nil)
	managerNodeID := assettest.CreateManagerNodeFixture(ctx, t, projectID, "", nil, nil)
	assetID := assettest.CreateAssetFixture(ctx, t, issuerNodeID, "", "")
	accountID := assettest.CreateAccountFixture(ctx, t, managerNodeID, "", nil)

	tx := assettest.Issue(ctx, t, assetID, []*txbuilder.Destination{
		assettest.AccountDest(ctx, t, accountID, assetID, 1),
	})

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	zero := uint32(0)

	want := []*TxOutput{{
		TxHash:   &tx.Hash,
		TxIndex:  &zero,
		AssetID:  assetID,
		Amount:   1,
		Address:  tx.Outputs[0].Script,
		Script:   tx.Outputs[0].Script,
		Metadata: []byte{},
	}}

	got, _, err := ListUTXOsByAsset(ctx, assetID, "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}

	if !reflect.DeepEqual(got, want) {
		gotStr, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal("unexpected error: ", err)
		}

		wantStr, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal("unexpected error: ", err)
		}

		t.Errorf("txs:\ngot:\n%s\nwant:\n%s", string(gotStr), string(wantStr))
	}
}

func TestListHistoricalOutputsByAsset(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store, pool := txdb.New(pg.FromContext(ctx).(*sql.DB)) // should this use memstore? per TODO above.
	fc, err := assettest.InitializeSigningGenerator(ctx, store, pool)
	if err != nil {
		t.Fatal(err)
	}

	Connect(ctx, fc, true, 0, true)
	projectID := assettest.CreateProjectFixture(ctx, t, "", "")
	issuerNodeID := assettest.CreateIssuerNodeFixture(ctx, t, projectID, "", nil, nil)
	managerNodeID := assettest.CreateManagerNodeFixture(ctx, t, projectID, "", nil, nil)
	assetID := assettest.CreateAssetFixture(ctx, t, issuerNodeID, "", "")
	uncountedAssetID := assettest.CreateAssetFixture(ctx, t, issuerNodeID, "", "") // this asset should never show up.
	account1ID := assettest.CreateAccountFixture(ctx, t, managerNodeID, "", nil)
	account2ID := assettest.CreateAccountFixture(ctx, t, managerNodeID, "", nil)
	tx := assettest.Issue(ctx, t, assetID, []*txbuilder.Destination{
		assettest.AccountDest(ctx, t, account1ID, assetID, 100),
	})
	assettest.IssueAssetsFixture(ctx, t, uncountedAssetID, 200, account1ID)

	check := func(got []*TxOutput, gotLast string) {
		zero := uint32(0)
		want := []*TxOutput{{
			TxHash:   &tx.Hash,
			TxIndex:  &zero,
			AssetID:  assetID,
			Amount:   100,
			Address:  tx.Outputs[0].Script,
			Script:   tx.Outputs[0].Script,
			Metadata: []byte{},
		}}
		if !reflect.DeepEqual(got, want) {
			gotStr, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal("unexpected error: ", err)
			}

			wantStr, err := json.MarshalIndent(want, "", "  ")
			if err != nil {
				t.Fatal("unexpected error: ", err)
			}

			t.Errorf("txs:\ngot:\n%s\nwant:\n%s", string(gotStr), string(wantStr))
		}

		wantLast := tx.Hash.String() + ":0"
		if gotLast != wantLast {
			t.Fatalf("last:\ngot:\n%s\nwant:\n%s", gotLast, wantLast)
		}
	}

	checkEmpty := func(got []*TxOutput) {
		if len(got) != 0 {
			t.Errorf("expected 0 historical outputs, got %d", len(got))
		}
	}

	// before we make a block, we shouldn't have any historical outputs
	got, _, err := ListHistoricalOutputsByAsset(ctx, assetID, time.Now(), "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}
	checkEmpty(got)

	// for non-historical queries, we should check that we have the same result
	got, _, err = ListHistoricalOutputsByAsset(ctx, assetID, time.Time{}, "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}
	checkEmpty(got)

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	ownershipTime := time.Now()
	got, gotLast, err := ListHistoricalOutputsByAsset(ctx, assetID, ownershipTime, "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}
	check(got, gotLast)

	// sleep, so that we can be sure the next block isn't in the same second (sorry everyone)
	time.Sleep(time.Second)

	// spend that UTXO, and make sure it comes back at ownershipTime.
	assettest.Transfer(ctx, t, []*txbuilder.Source{
		asset.NewAccountSource(ctx, &bc.AssetAmount{AssetID: assetID, Amount: 100}, account1ID, nil, nil, nil),
	}, []*txbuilder.Destination{
		assettest.AccountDest(ctx, t, account2ID, assetID, 100),
	})

	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, gotLast, err = ListHistoricalOutputsByAsset(ctx, assetID, ownershipTime, "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}
	check(got, gotLast)

	// issue another 200 units of the first asset. This shouldn't change the results of our query.
	assettest.IssueAssetsFixture(ctx, t, assetID, 200, account1ID)
	_, err = generator.MakeBlock(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	got, gotLast, err = ListHistoricalOutputsByAsset(ctx, assetID, ownershipTime, "", 10000)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}
	check(got, gotLast)
}

func mockAssetIDAndSigScript() (bc.AssetID, []byte) {
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_FALSE)
	script, err := builder.Script()
	if err != nil {
		panic(err)
	}

	redeemScript, err := txscript.RedeemScriptFromP2SHSigScript(script)
	if err != nil {
		panic(err)
	}
	pkScript := txscript.RedeemToPkScript(redeemScript)
	assetID := bc.ComputeAssetID(pkScript, [32]byte{})

	return assetID, script
}
