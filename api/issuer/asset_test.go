package issuer_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"

	"chain/api/appdb"
	. "chain/api/issuer"
	"chain/database/pg/pgtest"
)

func TestCreateAsset(t *testing.T) {
	ctx := pgtest.NewContext(t, `
		ALTER SEQUENCE issuer_nodes_key_index_seq RESTART;
		ALTER SEQUENCE assets_key_index_seq RESTART;
		INSERT INTO issuer_nodes (id, project_id, label, keyset)
		VALUES ('in1', 'a1', 'foo', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}');
	`)
	defer pgtest.Finish(ctx)

	clientToken := "a-client-provided-unique-token"
	definition := make(map[string]interface{})
	asset, err := CreateAsset(ctx, "in1", "fooAsset", definition, &clientToken)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}

	wantID := "7b97c04149f6f932a12b5e2f4149aa2024b48f5fdb1be383592f1f30af5aa8ab"
	if asset.Hash.String() != wantID {
		t.Errorf("got asset id = %v want %v", asset.Hash.String(), wantID)
	}

	wantRedeem := "51210371fe1fe0352f0cea91344d06c9d9b16e394e1945ee0f3063c2f9891d163f0f5551ae"
	if hex.EncodeToString(asset.RedeemScript) != wantRedeem {
		t.Errorf("got redeem script = %x want %v", asset.RedeemScript, wantRedeem)
	}

	if asset.Label != "fooAsset" {
		t.Errorf("got label = %v want %v", asset.Label, "fooAsset")
	}

	wantIssuance := "a9147ca5bdd7e39cb806681d7c635b1bc36e23cbefa987"
	if hex.EncodeToString(asset.IssuanceScript) != wantIssuance {
		t.Errorf("got issuance script=%x want=%v", asset.IssuanceScript, wantIssuance)
	}

	// Try to create the same asset again, and ensure that it returns the
	// original asset.
	newAsset, err := CreateAsset(ctx, "in1", "fooAsset2", definition, &clientToken)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if !reflect.DeepEqual(asset, newAsset) {
		t.Errorf("got new asset = %#v want original asset with same client_token = %#v", newAsset, asset)
	}
}

func TestCreateDefs(t *testing.T) {
	const fix = `
		INSERT INTO issuer_nodes (id, project_id, label, keyset)
		VALUES ('inode-0', 'proj-0', 'label-0', '{xpub661MyMwAqRbcGKBeRA9p52h7EueXnRWuPxLz4Zoo1ZCtX8CJR5hrnwvSkWCDf7A9tpEZCAcqex6KDuvzLxbxNZpWyH6hPgXPzji9myeqyHd}');
	`

	examples := []struct {
		def  map[string]interface{}
		want []byte
	}{
		// blank def
		{nil, nil},

		// empty JSON def
		{make(map[string]interface{}), []byte(`{}`)},

		// non-empty JSON def (whitespace matters)
		{map[string]interface{}{"foo": "bar"}, []byte(`{
  "foo": "bar"
}`,
		)},
	}

	for i, ex := range examples {
		t.Log("Example", i)
		clientToken := fmt.Sprintf("example-%d", i)

		ctx := pgtest.NewContext(t, fix)
		defer pgtest.Finish(ctx)
		gotCreated, err := CreateAsset(ctx, "inode-0", "label", ex.def, &clientToken)
		if err != nil {
			t.Fatal("unexpected error: ", err)
		}

		if !bytes.Equal(gotCreated.Definition, ex.want) {
			t.Errorf("create result:\ngot:  %s\nwant: %s", gotCreated.Definition, ex.want)
		}

		gotFetch, err := appdb.AssetByID(ctx, gotCreated.Hash)
		if err != nil {
			t.Fatal("unexpected error: ", err)
		}

		if !bytes.Equal(gotFetch.Definition, ex.want) {
			t.Errorf("db fetch result:\ngot:  %s\nwant: %s", gotFetch.Definition, ex.want)
		}
		pgtest.Finish(ctx)
	}
}
