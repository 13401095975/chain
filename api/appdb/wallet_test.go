package appdb

import (
	"reflect"
	"testing"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/database/pg/pgtest"
)

func TestCreateWallet(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, sampleAppFixture)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	id, err := CreateWallet(ctx, "app-id-0", "foo", []*Key{dummyXPub})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if id == "" {
		t.Errorf("got empty wallet id")
	}
}

func TestWalletBalance(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO utxos (txid, index, asset_id, amount, address_id, bucket_id, wallet_id)
		VALUES ('t0', 0, 'a1', 10, 'add0', 'b0', 'w1'),
		       ('t1', 1, 'a1', 5, 'add0', 'b0', 'w1'),
		       ('t2', 2, 'a2', 20, 'add0', 'b1', 'w1');
	`)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	bals, err := WalletBalance(ctx, "w1")
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	want := []*Balance{
		{
			AssetID:   "a1",
			Confirmed: 15,
			Total:     15,
		},
		{
			AssetID:   "a2",
			Confirmed: 20,
			Total:     20,
		},
	}

	if !reflect.DeepEqual(want, bals) {
		t.Errorf("got=%v want=%v", bals, want)
	}
}

func TestListWallets(t *testing.T) {
	dbtx := pgtest.TxWithSQL(t, `
		INSERT INTO applications (id, name) VALUES
			('app-id-0', 'app-0'),
			('app-id-1', 'app-1');

		INSERT INTO wallets (id, application_id, key_index, label) VALUES
			('wallet-id-0', 'app-id-0', 0, 'wallet-0'),
			('wallet-id-1', 'app-id-0', 1, 'wallet-1'),
			('wallet-id-2', 'app-id-1', 2, 'wallet-2');
	`)
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)

	examples := []struct {
		appID string
		want  []*Wallet
	}{
		{
			"app-id-0",
			[]*Wallet{
				{ID: "wallet-id-0", Blockchain: "sandbox", Label: "wallet-0"},
				{ID: "wallet-id-1", Blockchain: "sandbox", Label: "wallet-1"},
			},
		},
		{
			"app-id-1",
			[]*Wallet{
				{ID: "wallet-id-2", Blockchain: "sandbox", Label: "wallet-2"},
			},
		},
	}

	for _, ex := range examples {
		t.Log("Example:", ex.appID)

		got, err := ListWallets(ctx, ex.appID)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, ex.want) {
			t.Errorf("wallets:\ngot:  %v\nwant: %v", got, ex.want)
		}
	}
}
