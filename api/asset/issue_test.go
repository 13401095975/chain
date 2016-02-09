package asset_test

import (
	"os"
	"reflect"
	"testing"

	"golang.org/x/net/context"

	. "chain/api/asset"
	"chain/api/txbuilder"
	"chain/api/txdb"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/fedchain"
	"chain/fedchain/bc"
)

func init() {
	ConnectFedchain(fedchain.New(&txdb.Store{}, nil))
	u := "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		u = s
	}

	ctx := context.Background()
	pgtest.Open(ctx, u, "assettest", "../appdb/schema.sql")
}

// Establish a context object with a new db transaction in which to
// run the given callback function.
func withContext(tb testing.TB, sql string, fn func(context.Context)) {
	var ctx context.Context
	if sql == "" {
		ctx = pgtest.NewContext(tb)
	} else {
		ctx = pgtest.NewContext(tb, sql)
	}
	defer pgtest.Finish(ctx)
	fn(ctx)
}

func mustParseHash(s string) [32]byte {
	h, err := bc.ParseHash(s)
	if err != nil {
		panic(err)
	}
	return h
}

func TestIssue(t *testing.T) {
	ctx := pgtest.NewContext(t, `
		INSERT INTO projects (id, name) VALUES ('proj-id-0', 'proj-0');
		INSERT INTO issuer_nodes (id, project_id, label, keyset, key_index)
			VALUES ('in1', 'proj-id-0', 'foo', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}', 0);
		INSERT INTO assets (id, issuer_node_id, key_index, keyset, redeem_script, issuance_script, label)
		VALUES(
			'0000000000000000000000000000000000000000000000000000000000000000',
			'in1',
			0,
			'{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}',
			decode('51210371fe1fe0352f0cea91344d06c9d9b16e394e1945ee0f3063c2f9891d163f0f5551ae', 'hex'),
			'\x'::bytea,
			'foo'
		);
		INSERT INTO blocks (block_hash, height, data, header)
		VALUES(
			'341fb89912be0110b527375998810c99ac96a317c63b071ccf33b7514cf0f0a5',
			1,
			decode('0000000100000000000000013132330000000000000000000000000000000000000000000000000000000000414243000000000000000000000000000000000000000000000000000000000058595a000000000000000000000000000000000000000000000000000000000000000000000000640f746573742d7369672d73637269707412746573742d6f75747075742d73637269707401000000010000000000000000000007746573742d7478', 'hex'),
			''
		);
	`)
	defer pgtest.Finish(ctx)

	outScript := mustDecodeHex("a9140ac9c982fd389181752e5a414045dd424a10754b87")
	assetAmount := &bc.AssetAmount{Amount: 123}
	dest, err := NewScriptDestination(ctx, assetAmount, outScript, nil)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}
	outs := []*txbuilder.Destination{dest}
	resp, err := Issue(ctx, "0000000000000000000000000000000000000000000000000000000000000000", outs)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	want := &bc.TxData{
		Version: 1,
		Inputs: []*bc.TxInput{{Previous: bc.Outpoint{
			Index: bc.InvalidOutputIndex,
			Hash:  bc.Hash{},
		}}},
		Outputs: []*bc.TxOutput{{AssetAmount: bc.AssetAmount{AssetID: bc.AssetID{}, Amount: 123}, Script: outScript}},
	}

	if !reflect.DeepEqual(resp.Unsigned, want) {
		t.Errorf("got tx = %+v want %+v", resp.Unsigned, want)
	}
}
