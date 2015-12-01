package txdb

import (
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/script"
	"chain/fedchain/state"
	chainlog "chain/log"
)

func init() {
	chainlog.SetOutput(ioutil.Discard)

	u := "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		u = s
	}

	db := pgtest.Open(u, "txdbtest", "../appdb/schema.sql")
	err := appdb.Init(db)
	if err != nil {
		log.Fatal(err)
	}
}

// Establish a context object with a new db transaction in which to
// run the given callback function.
func withContext(tb testing.TB, sql string, fn func(context.Context)) {
	var dbtx pg.Tx
	if sql == "" {
		dbtx = pgtest.TxWithSQL(tb)
	} else {
		dbtx = pgtest.TxWithSQL(tb, sql)
	}
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)
	fn(ctx)
}

func mustParseHash(s string) bc.Hash {
	h, err := bc.ParseHash(s)
	if err != nil {
		panic(err)
	}
	return h
}

func TestPoolTxs(t *testing.T) {
	const fix = `
		INSERT INTO pool_txs (tx_hash, data)
		VALUES (
			'9e8cf364fc0446a1341dd021098a07983108c7bb853a8a33b466a292c4a8b248',
			decode('00000001000000000000000000000568656c6c6f', 'hex')
		);
	`
	withContext(t, fix, func(ctx context.Context) {
		got, err := PoolTxs(ctx)
		if err != nil {
			t.Fatalf("err got = %v want nil", err)
		}

		want := []*bc.Tx{
			{
				Version:  1,
				Metadata: []byte("hello"),
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("txs:\ngot:  %v\nwant: %v", got, want)
		}
	})
}

func TestGetTxs(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		tx := &bc.Tx{Metadata: []byte("tx")}
		err := InsertTx(ctx, tx)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		txs, err := GetTxs(ctx, tx.Hash().String())
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		if !reflect.DeepEqual(txs[tx.Hash().String()], tx) {
			t.Errorf("got:\n\t%+v\nwant:\n\t%+v", txs[tx.Hash().String()], tx)
		}

		_, gotErr := GetTxs(ctx, tx.Hash().String(), "nonexistent")
		if errors.Root(gotErr) != pg.ErrUserInputNotFound {
			t.Errorf("got err=%q want %q", errors.Root(gotErr), pg.ErrUserInputNotFound)
		}
	})
}

func TestInsertTx(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		tx := &bc.Tx{Metadata: []byte("tx")}
		err := InsertTx(ctx, tx)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		_, err = GetTxs(ctx, tx.Hash().String())
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}
	})
}

func TestLatestBlock(t *testing.T) {
	const fix = `
		INSERT INTO blocks (block_hash, height, data)
		VALUES
		('0000000000000000000000000000000000000000000000000000000000000000', 0, ''),
		(
			'341fb89912be0110b527375998810c99ac96a317c63b071ccf33b7514cf0f0a5',
			1,
			decode('0000000100000000000000013132330000000000000000000000000000000000000000000000000000000000414243000000000000000000000000000000000000000000000000000000000058595a000000000000000000000000000000000000000000000000000000000000000000000000640f746573742d7369672d73637269707412746573742d6f75747075742d73637269707401000000010000000000000000000007746573742d7478', 'hex')
		);
	`
	withContext(t, fix, func(ctx context.Context) {
		got, err := LatestBlock(ctx)
		if err != nil {
			t.Fatalf("err got = %v want nil", err)
		}

		want := &bc.Block{
			BlockHeader: bc.BlockHeader{
				Version:           1,
				Height:            1,
				PreviousBlockHash: [32]byte{'1', '2', '3'},
				TxRoot:            [32]byte{'A', 'B', 'C'},
				StateRoot:         [32]byte{'X', 'Y', 'Z'},
				Timestamp:         100,
				SignatureScript:   script.Script("test-sig-script"),
				OutputScript:      script.Script("test-output-script"),
			},
			Transactions: []*bc.Tx{
				{Version: 1, Metadata: []byte("test-tx")},
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("latest block:\ngot:  %+v\nwant: %+v", got, want)
		}
	})
}

func TestInsertBlock(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		blk := &bc.Block{
			BlockHeader: bc.BlockHeader{
				Version: 1,
				Height:  1,
			},
			Transactions: []*bc.Tx{{
				Metadata: []byte("a"),
			}, {
				Metadata: []byte("b"),
			}},
		}
		err := InsertBlock(ctx, blk)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		// block in database
		_, err = GetBlock(ctx, blk.Hash().String())
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		// txs in database
		txs := blk.Transactions
		_, err = GetTxs(ctx, txs[0].Hash().String(), txs[1].Hash().String())
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}
	})
}

func TestGetBlock(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		blk := &bc.Block{
			BlockHeader: bc.BlockHeader{
				Version: 1,
				Height:  1,
			},
		}
		err := InsertBlock(ctx, blk)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		got, err := GetBlock(ctx, blk.Hash().String())
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, blk) {
			t.Errorf("got:\n\t%+v\nwant:\n\t:%+v", got, blk)
		}

		_, gotErr := GetBlock(ctx, "nonexistent")
		if errors.Root(gotErr) != pg.ErrUserInputNotFound {
			t.Errorf("got err=%q want %q", errors.Root(gotErr), pg.ErrUserInputNotFound)
		}
	})
}

func TestListBlocks(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		blks := []*bc.Block{
			{BlockHeader: bc.BlockHeader{Height: 1}},
			{BlockHeader: bc.BlockHeader{Height: 0}},
		}
		for _, blk := range blks {
			err := InsertBlock(ctx, blk)
			if err != nil {
				t.Log(errors.Stack(err))
				t.Fatal(err)
			}
		}
		cases := []struct {
			prev  string
			limit int
			want  []*bc.Block
		}{{
			prev:  "",
			limit: 50,
			want:  blks,
		}, {
			prev:  "1",
			limit: 50,
			want:  []*bc.Block{blks[1]},
		}, {
			prev:  "",
			limit: 1,
			want:  []*bc.Block{blks[0]},
		}, {
			prev:  "0",
			limit: 50,
			want:  nil,
		}}

		for _, c := range cases {
			got, err := ListBlocks(ctx, c.prev, c.limit)
			if err != nil {
				t.Log(errors.Stack(err))
				t.Errorf("ListBlocks(%q, %d) error = %q", c.prev, c.limit, err)
				continue
			}

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got ListBlocks(%q, %d):\n\t%+v\nwant:\n\t%+v", c.prev, c.limit, got, c.want)
			}
		}
	})
}

func TestRemoveBlockOutputs(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		blk := &bc.Block{BlockHeader: bc.BlockHeader{Height: 1}}
		out := &Output{
			Output: state.Output{
				TxOutput: bc.TxOutput{
					AssetID:  bc.AssetID{},
					Value:    5,
					Script:   []byte("a"),
					Metadata: []byte("b"),
				},
				Outpoint: bc.Outpoint{},
			},
			AccountID:     "account-1",
			ManagerNodeID: "mnode-1",
			AddrIndex:     [2]uint32{0, 0},
		}
		err := InsertBlockOutputs(ctx, blk, []*Output{out})
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		out.Spent = true
		err = RemoveBlockSpentOutputs(ctx, []*Output{out})
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		gotOut, err := loadOutput(ctx, out.Outpoint)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		if gotOut != nil {
			t.Fatal("expected out to be removed from database")
		}
	})
}

func TestInsertBlockOutputs(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		blk := &bc.Block{BlockHeader: bc.BlockHeader{Height: 1}}
		out := &Output{
			Output: state.Output{
				TxOutput: bc.TxOutput{
					AssetID:  bc.AssetID{},
					Value:    5,
					Script:   []byte("a"),
					Metadata: []byte("b"),
				},
				Outpoint: bc.Outpoint{},
			},
			AccountID:     "account-1",
			ManagerNodeID: "mnode-1",
			AddrIndex:     [2]uint32{0, 0},
		}
		err := InsertBlockOutputs(ctx, blk, []*Output{out})
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		_, err = loadOutput(ctx, out.Outpoint)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}
	})
}

// Helper function just for testing.
// In production, we ~never want to load a single output;
// we always load in batches.
func loadOutput(ctx context.Context, p bc.Outpoint) (*state.Output, error) {
	m, err := loadOutputs(ctx, []bc.Outpoint{p})
	return m[p], err
}
