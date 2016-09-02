package query

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"chain/core/query/filter"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/protocol"
	"chain/protocol/bc"
)

func TestDecodeOutputsCursor(t *testing.T) {
	testCases := []struct {
		str string
		cur OutputsCursor
	}{
		{str: "1-1-0", cur: OutputsCursor{lastBlockHeight: 1, lastTxPos: 1}},
		{str: "1-2-3", cur: OutputsCursor{lastBlockHeight: 1, lastTxPos: 2, lastIndex: 3}},
		{str: "a-1-0", cur: OutputsCursor{lastBlockHeight: 10, lastTxPos: 1}},
		{str: "f-f-f", cur: OutputsCursor{lastBlockHeight: 0xf, lastTxPos: 0xf, lastIndex: 0xf}},
		{str: "c001-cafe-ca75", cur: OutputsCursor{lastBlockHeight: 0xc001, lastTxPos: 0xcafe, lastIndex: 0xca75}},
	}

	for _, tc := range testCases {
		decoded, err := DecodeOutputsCursor(tc.str)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(decoded, &tc.cur) {
			t.Errorf("got %#v, want %#v", decoded, &tc.cur)
		}
		if decoded.String() != tc.str {
			t.Errorf("re-encode: got %s, want %s", decoded.String(), tc.str)
		}
	}
}

func TestOutputsCursor(t *testing.T) {
	_, db := pgtest.NewDB(t, pgtest.SchemaPath)
	ctx := pg.NewContext(context.Background(), db)
	_, err := db.Exec(ctx, `
		INSERT INTO annotated_outputs (block_height, tx_pos, output_index, tx_hash, data, timespan)
		VALUES
			(1, 0, 0, 'ab', '{"account_id": "abc"}', int8range(1, 100)),
			(1, 1, 0, 'cd', '{"account_id": "abc"}', int8range(1, 100)),
			(2, 0, 0, 'ef', '{"account_id": "abc"}', int8range(10, 50));
	`)
	if err != nil {
		t.Fatal(err)
	}
	q, err := filter.Parse(`account_id = 'abc'`)
	if err != nil {
		t.Fatal(err)
	}

	indexer := NewIndexer(db, &protocol.Chain{})
	results, cursor, err := indexer.Outputs(ctx, q, nil, 25, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
	if cursor.String() != "1-1-0" {
		t.Errorf("got cursor=%q want 1-1-0", cursor.String())
	}

	results, cursor, err = indexer.Outputs(ctx, q, nil, 25, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("got %d results, want 1", len(results))
	}
	if cursor.String() != "2-0-0" {
		t.Errorf("got cursor=%q want 2-0-0", cursor.String())
	}
}

func TestConstructOutputsQuery(t *testing.T) {
	now := time.Unix(233400000, 0)
	nowMillis := bc.Millis(now)

	testCases := []struct {
		filter     string
		values     []interface{}
		cursor     *OutputsCursor
		wantQuery  string
		wantValues []interface{}
	}{
		{
			// empty filter
			wantQuery:  `SELECT block_height, tx_pos, output_index, data FROM "annotated_outputs" WHERE timespan @> $1::int8 ORDER BY block_height ASC, tx_pos ASC, output_index ASC LIMIT 10`,
			wantValues: []interface{}{nowMillis},
		},
		{
			filter:     "asset_id = $1 AND account_id = 'abc'",
			values:     []interface{}{"foo"},
			wantQuery:  `SELECT block_height, tx_pos, output_index, data FROM "annotated_outputs" WHERE ((data @> $1::jsonb)) AND timespan @> $2::int8 ORDER BY block_height ASC, tx_pos ASC, output_index ASC LIMIT 10`,
			wantValues: []interface{}{`{"account_id":"abc","asset_id":"foo"}`, nowMillis},
		},
		{
			filter: "asset_id = $1 AND account_id = 'abc'",
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
		f, err := filter.Parse(tc.filter)
		if err != nil {
			t.Fatal(err)
		}
		expr, err := filter.AsSQL(f, "data", tc.values)
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
			filter string
			values []interface{}
			when   time.Time
			want   []assetAccountAmount
		}
	)

	ctx, indexer, time1, time2, acct1, acct2, asset1, asset2 := setupQueryTest(t)

	cases := []testcase{
		{
			filter: "asset_id = $1",
			values: []interface{}{asset1.AssetID.String()},
			when:   time1,
		},
		{
			filter: "asset_tags.currency = $1",
			values: []interface{}{"USD"},
			when:   time1,
		},
		{
			filter: "asset_id = $1",
			values: []interface{}{asset1.AssetID.String()},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{AssetID: asset1.AssetID, Amount: 867}, acct1.ID},
			},
		},
		{
			filter: "asset_tags.currency = $1",
			values: []interface{}{"USD"},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{AssetID: asset1.AssetID, Amount: 867}, acct1.ID},
			},
		},
		{
			filter: "asset_id = $1",
			values: []interface{}{asset2.AssetID.String()},
			when:   time1,
		},
		{
			filter: "asset_id = $1",
			values: []interface{}{asset2.AssetID.String()},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{AssetID: asset2.AssetID, Amount: 100}, acct1.ID},
			},
		},
		{
			filter: "account_id = $1",
			values: []interface{}{acct1.ID},
			when:   time1,
			want:   []assetAccountAmount{},
		},
		{
			filter: "account_id = $1",
			values: []interface{}{acct1.ID},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{AssetID: asset1.AssetID, Amount: 867}, acct1.ID},
				{bc.AssetAmount{AssetID: asset2.AssetID, Amount: 100}, acct1.ID},
			},
		},
		{
			filter: "account_id = $1",
			values: []interface{}{acct2.ID},
			when:   time1,
			want:   []assetAccountAmount{},
		},
		{
			filter: "account_id = $1",
			values: []interface{}{acct2.ID},
			when:   time2,
			want:   []assetAccountAmount{},
		},
		{
			filter: "asset_id = $1 AND account_id = $2",
			values: []interface{}{asset1.AssetID.String(), acct1.ID},
			when:   time2,
			want: []assetAccountAmount{
				{bc.AssetAmount{AssetID: asset1.AssetID, Amount: 867}, acct1.ID},
			},
		},
		{
			filter: "asset_id = $1 AND account_id = $2",
			values: []interface{}{asset2.AssetID.String(), acct2.ID},
			when:   time2,
			want:   []assetAccountAmount{},
		},
	}

	for i, tc := range cases {
		f, err := filter.Parse(tc.filter)
		if err != nil {
			t.Fatal(err)
		}
		outputs, _, err := indexer.Outputs(ctx, f, tc.values, bc.Millis(tc.when), nil, 1000)
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
				var got struct {
					AssetID   *string `json:"asset_id"`
					Amount    *uint64
					AccountID *string `json:"account_id"`
				}

				bytes := output.(*json.RawMessage)
				err := json.Unmarshal(*bytes, &got)
				if err != nil {
					t.Fatalf("case %d: output is not a JSON object", i)
				}

				if wantAssetID == *got.AssetID && w.Amount == uint64(*got.Amount) && w.AccountID == *got.AccountID {
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
