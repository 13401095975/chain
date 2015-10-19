package appdb

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
)

func TestInsertAssetGroup(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	group, err := InsertAssetGroup(ctx, "a1", "foo", []*hdkey.XKey{dummyXPub}, nil)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if group.ID == "" {
		t.Errorf("got empty asset group id")
	}
}

func TestListAssetGroups(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO projects (id, name) VALUES
			('proj-id-0', 'proj-0'),
			('proj-id-1', 'proj-1');

		INSERT INTO issuer_nodes
			(id, project_id, key_index, keyset, label, created_at)
		VALUES
			-- insert in reverse chronological order, to ensure that ListAssetGroups
			-- is performing a sort.
			('ag-id-0', 'proj-id-0', 0, '{}', 'ag-0', now()),
			('ag-id-1', 'proj-id-0', 1, '{}', 'ag-1', now() - '1 day'::interval),

			('ag-id-2', 'proj-id-1', 2, '{}', 'ag-2', now());
	`)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	examples := []struct {
		projID string
		want   []*AssetGroup
	}{
		{
			"proj-id-0",
			[]*AssetGroup{
				{ID: "ag-id-1", Blockchain: "sandbox", Label: "ag-1"},
				{ID: "ag-id-0", Blockchain: "sandbox", Label: "ag-0"},
			},
		},
		{
			"proj-id-1",
			[]*AssetGroup{
				{ID: "ag-id-2", Blockchain: "sandbox", Label: "ag-2"},
			},
		},
	}

	for _, ex := range examples {
		t.Log("Example:", ex.projID)

		got, err := ListAssetGroups(ctx, ex.projID)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("asset groups:\ngot:  %v\nwant: %v", got, ex.want)
		}
	}
}

func TestGetAssetGroup(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO projects (id, name) VALUES
			('proj-id-0', 'proj-0');

		INSERT INTO issuer_nodes (id, project_id, key_index, keyset, label) VALUES
			('ag-id-0', 'proj-id-0', 0, '{}', 'ag-0');
	`)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	examples := []struct {
		id      string
		want    *AssetGroup
		wantErr error
	}{
		{
			"ag-id-0",
			&AssetGroup{ID: "ag-id-0", Label: "ag-0", Blockchain: "sandbox"},
			nil,
		},
		{
			"nonexistent",
			nil,
			pg.ErrUserInputNotFound,
		},
	}

	for _, ex := range examples {
		t.Log("Example:", ex.id)

		got, gotErr := GetAssetGroup(ctx, ex.id)

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("asset group:\ngot:  %v\nwant: %v", got, ex.want)
		}

		if errors.Root(gotErr) != ex.wantErr {
			t.Errorf("get asset group error:\ngot:  %v\nwant: %v", errors.Root(gotErr), ex.wantErr)
		}
	}
}
