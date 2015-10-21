package asset

import (
	"bytes"
	"encoding/hex"
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
)

func init() {
	u := "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		u = s
	}

	db := pgtest.Open(u, "assettest", "../appdb/schema.sql")
	err := appdb.Init(db)
	if err != nil {
		log.Fatal(err)
	}
}

func TestIssue(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO projects (id, name) VALUES ('proj-id-0', 'proj-0');
		INSERT INTO issuer_nodes (id, project_id, label, keyset, key_index)
			VALUES ('ag1', 'proj-id-0', 'foo', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}', 0);
		INSERT INTO assets (id, issuer_node_id, key_index, keyset, redeem_script, label)
		VALUES(
			'0000000000000000000000000000000000000000000000000000000000000000',
			'ag1',
			0,
			'{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}',
			decode('51210371fe1fe0352f0cea91344d06c9d9b16e394e1945ee0f3063c2f9891d163f0f5551ae', 'hex'),
			'foo'
		);
	`)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	outs := []*Output{{
		Address: "32g4QsxVQrhZeXyXTUnfSByNBAdTfVUdVK",
		Amount:  123,
	}}

	resp, err := Issue(ctx, "0000000000000000000000000000000000000000000000000000000000000000", outs)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	outScript, _ := hex.DecodeString("a9140ac9c982fd389181752e5a414045dd424a10754b87")
	want := &bc.Tx{
		Version: 1,
		Inputs:  []*bc.TxInput{{Previous: bc.IssuanceOutpoint}},
		Outputs: []*bc.TxOutput{{AssetID: bc.AssetID{}, Value: 123, Script: outScript}},
	}

	if !reflect.DeepEqual(resp.Unsigned, want) {
		t.Errorf("got tx = %+v want %+v", resp.Unsigned, want)
	}

	// Bad output destination error
	outs = []*Output{{Amount: 5}}
	_, err = Issue(ctx, "0000000000000000000000000000000000000000000000000000000000000000", outs)

	if errors.Root(err) != ErrBadOutDest {
		t.Errorf("got err = %v want %v", errors.Root(err), ErrBadOutDest)
	}
}

func TestOutputPKScript(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO projects (id, name) VALUES ('proj-id-0', 'proj-0');
		INSERT INTO manager_nodes (id, project_id, label, current_rotation)
			VALUES('mn1', 'proj-id-0', 'mn1', 'rot1');
		INSERT INTO rotations (id, manager_node_id, keyset)
			VALUES('rot1', 'mn1', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}');
		INSERT INTO accounts (id, manager_node_id, key_index)
			VALUES('acc1', 'mn1', 0);
	`)
	defer dbtx.Rollback()

	// Test account output pk script (address creation)
	var (
		out = &Output{AccountID: "acc1"}
		ctx = pg.NewContext(context.Background(), dbtx)
	)
	got, _, err := out.PKScript(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	want, _ := hex.DecodeString("a91400065635e652a6e00a53cfa07e822de50ccf94a887")
	if !bytes.Equal(got, want) {
		t.Errorf("got pkscript = %x want %x", got, want)
	}

	// Test stringified address output
	out = &Output{Address: "31h9Wq4sVTr2ogZQgcazqgwJtEhM3hFtT2"}
	got, _, err = out.PKScript(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("got pkscript = %x want %x", got, want)
	}

	// Test bad address output error
	out = &Output{Address: "bad-addr"}
	_, _, err = out.PKScript(ctx)
	if errors.Root(err) != ErrBadAddr {
		t.Errorf("got pkscript = %x want %x", errors.Root(err), ErrBadAddr)
	}
}

func TestPKScriptChangeAddr(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO projects (id, name) VALUES ('proj-id-0', 'proj-0');
		INSERT INTO manager_nodes (id, project_id, label, current_rotation)
			VALUES('mn1', 'proj-id-0', 'mn1', 'rot1');
		INSERT INTO rotations (id, manager_node_id, keyset)
			VALUES('rot1', 'mn1', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}');
		INSERT INTO accounts (id, manager_node_id, key_index)
			VALUES('acc1', 'mn1', 0);
	`)
	defer dbtx.Rollback()

	ctx := pg.NewContext(context.Background(), dbtx)

	out := &Output{AccountID: "acc1", isChange: true}
	_, recv, err := out.PKScript(ctx)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	if !recv.IsChange {
		t.Fatal("Expected change output")
	}
}
