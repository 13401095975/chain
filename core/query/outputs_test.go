package query

import (
	"reflect"
	"testing"
	"time"

	"chain/core/query/chql"
	"chain/cos/bc"
)

func TestConstructOutputsQuery(t *testing.T) {
	now := time.Unix(233400000, 0)
	nowMillis := bc.Millis(now)

	testCases := []struct {
		query      string
		values     []interface{}
		cursor     *OutputsCursor
		wantQuery  string
		wantValues []interface{}
	}{
		{
			query:      "asset_id = $1 AND account_id = 'abc'",
			values:     []interface{}{"foo"},
			wantQuery:  `SELECT block_height, tx_pos, output_index, data FROM "annotated_outputs" WHERE ((data @> $1::jsonb)) AND timespan @> $2::int8 ORDER BY block_height ASC, tx_pos ASC, output_index ASC LIMIT 10`,
			wantValues: []interface{}{`{"account_id":"abc","asset_id":"foo"}`, nowMillis},
		},
		{
			query:  "asset_id = $1 AND account_id = 'abc'",
			values: []interface{}{"foo"},
			cursor: &OutputsCursor{
				lastBlockHeight: 15,
				lastTxPos:       17,
				lastIndex:       19,
			},
			wantQuery:  `SELECT block_height, tx_pos, output_index, data FROM "annotated_outputs" WHERE ((data @> $1::jsonb)) AND timespan @> $2::int8 AND (block_height, tx_pos, output_index) > ($3, $4, $5) ORDER BY block_height ASC, tx_pos ASC, output_index ASC LIMIT 10`,
			wantValues: []interface{}{`{"account_id":"abc","asset_id":"foo"}`, nowMillis, uint64(15), uint32(17), uint32(19)},
		},
	}

	for i, tc := range testCases {
		q, err := chql.Parse(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		expr, err := chql.AsSQL(q, "data", tc.values)
		if err != nil {
			t.Fatal(err)
		}
		query, values := constructOutputsQuery(expr, nowMillis, tc.cursor, 10)
		if query != tc.wantQuery {
			t.Errorf("case %d: got %s want %s", i, query, tc.wantQuery)
		}
		if !reflect.DeepEqual(values, tc.wantValues) {
			t.Errorf("case %d: got %#v, want %#v", i, values, tc.wantValues)
		}
	}
}

func TestQueryOutputs(t *testing.T) {
	type (
		assetAccountAmount struct {
			bc.AssetAmount
			AccountID string
		}
		testcase struct {
			query  string
			values []interface{}
			when   time.Time
			want   []assetAccountAmount
		}
	)

	ctx, indexer, time1, time2, acct1, acct2, asset1, asset2 := setupQueryTest(t)

	cases := []testcase{
		{
			query:  "asset_id = $1",
			values: []interface{}{asset1.AssetID.String()},
			when:   time1,
		},
		{
			query:  "asset_tags.currency = $1",
			values: []interface{}{"USD"},
			when:   time1,
		},
		{
			query:  "asset_id = $1",
			values: []interface{}{asset1.AssetID.String()},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{asset1.AssetID, 867}, acct1.ID},
			},
		},
		{
			query:  "asset_tags.currency = $1",
			values: []interface{}{"USD"},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{asset1.AssetID, 867}, acct1.ID},
			},
		},
		{
			query:  "asset_id = $1",
			values: []interface{}{asset2.AssetID.String()},
			when:   time1,
		},
		{
			query:  "asset_id = $1",
			values: []interface{}{asset2.AssetID.String()},
			when:   time2,
		},
		{
			query:  "account_id = $1",
			values: []interface{}{acct1.ID},
			when:   time1,
			want:   []assetAccountAmount{},
		},
		{
			query:  "account_id = $1",
			values: []interface{}{acct1.ID},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{asset1.AssetID, 867}, acct1.ID},
			},
		},
		{
			query:  "account_id = $1",
			values: []interface{}{acct2.ID},
			when:   time1,
			want:   []assetAccountAmount{},
		},
		{
			query:  "account_id = $1",
			values: []interface{}{acct2.ID},
			when:   time2,
			want:   []assetAccountAmount{},
		},
		{
			query:  "asset_id = $1 AND account_id = $2",
			values: []interface{}{asset1.AssetID.String(), acct1.ID},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{asset1.AssetID, 867}, acct1.ID},
			},
		},
		{
			query:  "asset_id = $1 AND account_id = $2",
			values: []interface{}{asset2.AssetID.String(), acct1.ID},
			when:   time2,
			want:   []assetAccountAmount{},
		},
	}

	for i, tc := range cases {
		chql, err := chql.Parse(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		outputs, _, err := indexer.Outputs(ctx, chql, tc.values, bc.Millis(tc.when), nil, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if len(outputs) != len(tc.want) {
			t.Fatalf("case %d: got %d outputs, want %d", i, len(outputs), len(tc.want))
		}
		for j, w := range tc.want {
			var found bool
			wantAssetID := w.AssetID.String()
			for _, output := range outputs {
				got, ok := output.(map[string]interface{})
				if !ok {
					t.Fatalf("case %d: output is not a JSON object", i)
				}
				gotAssetIDItem, ok := got["asset_id"]
				if !ok {
					t.Fatalf("case %d: output does not contain asset_id", i)
				}
				gotAssetID, ok := gotAssetIDItem.(string)
				if !ok {
					t.Fatalf("case %d: output asset_id is not a string", i)
				}
				gotAmountItem, ok := got["amount"]
				if !ok {
					t.Fatalf("case %d: output does not contain amount", i)
				}
				gotAmount, ok := gotAmountItem.(float64)
				if !ok {
					t.Fatalf("case %d: output amount is not a float64", i)
				}
				gotAccountIDItem, ok := got["account_id"]
				if !ok {
					t.Fatalf("case %d: output does not contain account_id", i)
				}
				gotAccountID, ok := gotAccountIDItem.(string)
				if !ok {
					t.Fatalf("case %d: output account_id is not a string", i)
				}

				if wantAssetID == gotAssetID && w.Amount == uint64(gotAmount) && w.AccountID == gotAccountID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("case %d: did not find item %d in output", i, j)
			}
		}
	}
}
