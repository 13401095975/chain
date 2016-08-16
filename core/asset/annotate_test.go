package asset

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/testutil"
)

func TestAnnotateTxs(t *testing.T) {
	ctx := pg.NewContext(context.Background(), pgtest.NewTx(t))

	tags1 := map[string]interface{}{"foo": "bar"}

	asset1, err := Define(ctx, []string{testutil.TestXPub.String()}, 1, nil, bc.Hash{}, "", tags1, nil)
	if err != nil {
		t.Fatal(err)
	}

	tags2 := map[string]interface{}{"foo": "baz"}
	asset2, err := Define(ctx, []string{testutil.TestXPub.String()}, 1, nil, bc.Hash{}, "", tags2, nil)
	if err != nil {
		t.Fatal(err)
	}

	txs := []map[string]interface{}{
		{
			"inputs": []interface{}{
				map[string]interface{}{
					"asset_id": asset1.AssetID.String(),
				},
				map[string]interface{}{
					"asset_id": asset2.AssetID.String(),
				},
				map[string]interface{}{
					"asset_id": "unknown",
				},
			},
			"outputs": []interface{}{
				map[string]interface{}{
					"asset_id": asset1.AssetID.String(),
				},
				map[string]interface{}{
					"asset_id": asset2.AssetID.String(),
				},
				map[string]interface{}{
					"asset_id": "unknown",
				},
			},
		},
	}
	want := []map[string]interface{}{
		{
			"inputs": []interface{}{
				map[string]interface{}{
					"asset_id":   asset1.AssetID.String(),
					"asset_tags": interface{}(tags1),
				},
				map[string]interface{}{
					"asset_id":   asset2.AssetID.String(),
					"asset_tags": interface{}(tags2),
				},
				map[string]interface{}{
					"asset_id":   "unknown",
					"asset_tags": map[string]interface{}{},
				},
			},
			"outputs": []interface{}{
				map[string]interface{}{
					"asset_id":   asset1.AssetID.String(),
					"asset_tags": interface{}(tags1),
				},
				map[string]interface{}{
					"asset_id":   asset2.AssetID.String(),
					"asset_tags": interface{}(tags2),
				},
				map[string]interface{}{
					"asset_id":   "unknown",
					"asset_tags": map[string]interface{}{},
				},
			},
		},
	}

	err = AnnotateTxs(ctx, txs)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(txs, want) {
		t.Errorf("got:\n%+v\nwant:\n%+v", txs, want)
	}
}
