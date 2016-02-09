package appdb_test

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	. "chain/api/appdb"
	"chain/api/asset"
	"chain/api/asset/assettest"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
	"chain/fedchain/bc"
	"chain/testutil"
)

func TestInsertManagerNode(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		_ = newTestManagerNode(t, ctx, nil, "foo")
	})
}

func TestGetManagerNode(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		proj := newTestProject(t, ctx, "foo", nil)
		mn, err := InsertManagerNode(ctx, proj.ID, "manager-node-0", []*hdkey.XKey{dummyXPub}, []*hdkey.XKey{dummyXPrv}, 0, 1)

		if err != nil {
			t.Fatalf("unexpected error on InsertManagerNode: %v", err)
		}
		examples := []struct {
			id      string
			want    *ManagerNode
			wantErr error
		}{
			{
				mn.ID,
				&ManagerNode{
					ID:    mn.ID,
					Label: "manager-node-0",
					Keys: []*NodeKey{
						{
							Type: "node",
							XPub: dummyXPub,
							XPrv: dummyXPrv,
						},
					},
					SigsReqd: 1,
				},
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

			got, gotErr := GetManagerNode(ctx, ex.id)

			if !reflect.DeepEqual(got, ex.want) {
				t.Errorf("managerNode:\ngot:  %v\nwant: %v", got, ex.want)
			}

			if errors.Root(gotErr) != ex.wantErr {
				t.Errorf("get managerNode error:\ngot:  %v\nwant: %v", errors.Root(gotErr), ex.wantErr)
			}
		}
	})
}

func TestAccountsWithAsset(t *testing.T) {
	ctx := pgtest.NewContext(t)
	defer pgtest.Finish(ctx)

	assettest.CreateGenesisBlockFixture(ctx, t)

	asset1 := assettest.CreateAssetFixture(ctx, t, "", "", "")
	asset2 := assettest.CreateAssetFixture(ctx, t, "", "", "")
	mn0 := assettest.CreateManagerNodeFixture(ctx, t, "", "manager-node-0", nil, nil)
	mn1 := assettest.CreateManagerNodeFixture(ctx, t, "", "manager-node-1", nil, nil)
	acc0 := assettest.CreateAccountFixture(ctx, t, mn0, "account-0", nil)
	acc1 := assettest.CreateAccountFixture(ctx, t, mn0, "account-1", nil)
	acc2 := assettest.CreateAccountFixture(ctx, t, mn1, "account-2", nil)

	assettest.IssueAssetsFixture(ctx, t, asset1, 5, acc0)
	assettest.IssueAssetsFixture(ctx, t, asset1, 5, acc0)
	assettest.IssueAssetsFixture(ctx, t, asset1, 5, acc1)
	out1 := assettest.IssueAssetsFixture(ctx, t, asset2, 5, acc1)
	assettest.IssueAssetsFixture(ctx, t, asset1, 5, acc2)

	_, err := asset.MakeBlock(ctx, asset.BlockKey)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	assettest.IssueAssetsFixture(ctx, t, asset1, 1, acc0)
	out2 := assettest.IssueAssetsFixture(ctx, t, asset1, 1, acc0)

	tx := &bc.Tx{TxData: bc.TxData{Inputs: []*bc.TxInput{
		{Previous: out1.Outpoint},
		{Previous: out2.Outpoint},
	}}}
	err = store.ApplyTx(ctx, tx)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	cases := []struct {
		assetID  bc.AssetID
		prev     string
		limit    int
		want     []*AccountBalanceItem
		wantLast string
	}{{
		assetID: asset1,
		prev:    "",
		limit:   50,
		want: []*AccountBalanceItem{
			{acc0, 10, 11},
			{acc1, 5, 5},
		},
		wantLast: acc1,
	}, {
		assetID: asset1,
		prev:    acc0,
		limit:   50,
		want: []*AccountBalanceItem{
			{acc1, 5, 5},
		},
		wantLast: acc1,
	}, {
		assetID: asset1,
		prev:    "",
		limit:   1,
		want: []*AccountBalanceItem{
			{acc0, 10, 11},
		},
		wantLast: acc0,
	}, {
		assetID:  asset1,
		prev:     acc1,
		limit:    50,
		want:     nil,
		wantLast: "",
	}, {
		assetID: asset2,
		prev:    "",
		limit:   50,
		want: []*AccountBalanceItem{
			{acc1, 5, 0},
		},
		wantLast: acc1,
	}}
	for _, c := range cases {
		got, gotLast, err := AccountsWithAsset(ctx, mn0, c.assetID.String(), c.prev, c.limit)
		if err != nil {
			t.Errorf("AccountsWithAsset(%q, %d) unexpected error = %q", c.prev, c.limit, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("AccountsWithAsset(%q, %d) = %+v want %+v", c.prev, c.limit, got, c.want)
		}
		if gotLast != c.wantLast {
			t.Errorf("AccountsWithAsset(%q, %d) last = %q want %q", c.prev, c.limit, gotLast, c.wantLast)
		}
	}
}

func TestListManagerNodes(t *testing.T) {
	ctx := pgtest.NewContext(t)
	defer pgtest.Finish(ctx)

	proj0 := assettest.CreateProjectFixture(ctx, t, "", "")
	proj1 := assettest.CreateProjectFixture(ctx, t, "", "")
	mn0 := assettest.CreateManagerNodeFixture(ctx, t, proj0, "manager-node-0", nil, nil)
	mn1 := assettest.CreateManagerNodeFixture(ctx, t, proj0, "manager-node-1", nil, nil)
	mn2 := assettest.CreateManagerNodeFixture(ctx, t, proj1, "manager-node-2", nil, nil)

	examples := []struct {
		projID string
		want   []*ManagerNode
	}{
		{
			proj0,
			[]*ManagerNode{
				{ID: mn0, Label: "manager-node-0"},
				{ID: mn1, Label: "manager-node-1"},
			},
		},
		{
			proj1,
			[]*ManagerNode{
				{ID: mn2, Label: "manager-node-2"},
			},
		},
	}

	for _, ex := range examples {
		t.Log("Example:", ex.projID)

		got, err := ListManagerNodes(ctx, ex.projID)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("managerNodes:\ngot:  %v\nwant: %v", got, ex.want)
		}
	}
}

func TestUpdateManagerNode(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		managerNode := newTestManagerNode(t, ctx, nil, "foo")
		newLabel := "bar"

		err := UpdateManagerNode(ctx, managerNode.ID, &newLabel)
		if err != nil {
			t.Errorf("Unexpected error %v", err)
		}

		managerNode, err = GetManagerNode(ctx, managerNode.ID)
		if err != nil {
			t.Errorf("Unexpected error %v", err)
		}
		if managerNode.Label != newLabel {
			t.Errorf("Expected %s, got %s", newLabel, managerNode.Label)
		}
	})
}

// Test that calling UpdateManagerNode with no new label is a no-op.
func TestUpdateManagerNodeNoUpdate(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		managerNode := newTestManagerNode(t, ctx, nil, "foo")
		err := UpdateManagerNode(ctx, managerNode.ID, nil)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}

		managerNode, err = GetManagerNode(ctx, managerNode.ID)
		if err != nil {
			t.Errorf("Unexpected error %v", err)
		}
		if managerNode.Label != "foo" {
			t.Errorf("Expected foo, got %s", managerNode.Label)
		}
	})
}

func TestArchiveManagerNode(t *testing.T) {
	withContext(t, "", func(ctx context.Context) {
		managerNode := newTestManagerNode(t, ctx, nil, "foo")
		account := newTestAccount(t, ctx, managerNode, "bar")
		err := ArchiveManagerNode(ctx, managerNode.ID)
		if err != nil {
			t.Errorf("could not archive manager node with id %s: %v", managerNode.ID, err)
		}

		var archived bool
		checkQ := `SELECT archived FROM manager_nodes WHERE id = $1`
		err = pg.FromContext(ctx).QueryRow(ctx, checkQ, managerNode.ID).Scan(&archived)

		if !archived {
			t.Errorf("expected manager node %s to be archived", managerNode.ID)
		}

		checkAccountQ := `SELECT archived FROM accounts WHERE id = $1`
		err = pg.FromContext(ctx).QueryRow(ctx, checkAccountQ, account.ID).Scan(&archived)
		if !archived {
			t.Errorf("expected child account %s to be archived", account.ID)
		}

	})
}
