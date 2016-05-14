package asset_test

import (
	"reflect"
	"testing"
	"time"

	"chain/core/appdb"
	. "chain/core/asset"
	"chain/core/asset/assettest"
	"chain/core/txbuilder"
	"chain/core/txdb"
	"chain/cos/bc"
	"chain/cos/hdkey"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/database/sql"
	"chain/errors"
	"chain/testutil"
)

func TestAccountSourceReserve(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store := txdb.NewStore(pg.FromContext(ctx).(*sql.DB))
	_, err := assettest.InitializeSigningGenerator(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	accID := assettest.CreateAccountFixture(ctx, t, "", "", nil)
	asset := assettest.CreateAssetFixture(ctx, t, "", "asset-0", "")
	out := assettest.IssueAssetsFixture(ctx, t, asset, 2, accID)

	assetAmount1 := &bc.AssetAmount{
		AssetID: asset,
		Amount:  1,
	}
	source := NewAccountSource(ctx, assetAmount1, accID, nil, nil)

	got, err := source.Reserver.Reserve(ctx, assetAmount1, time.Minute)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	want := &txbuilder.ReserveResult{
		Items: []*txbuilder.ReserveResultItem{{
			TxInput: &bc.TxInput{
				Previous:    out.Outpoint,
				AssetAmount: out.TxOutput.AssetAmount,
				PrevScript:  out.TxOutput.Script,
			},
			TemplateInput: nil,
		}},
		Change: []*txbuilder.Destination{{
			AssetAmount: bc.AssetAmount{AssetID: asset, Amount: 1},
		}},
	}

	if len(got.Items) != 1 {
		t.Fatalf("expected 1 result utxo")
	}

	// generated address can change based on test ordering, so ignore in comparison
	got.Items[0].TemplateInput = nil

	ar, ok := got.Change[0].Receiver.(*AccountReceiver)
	if !ok {
		t.Fatalf("expected change destination to have AccountReceiver")
	}

	if ar.Addr().AccountID != accID {
		t.Errorf("got receiver addr account = %v want %v", ar.Addr().AccountID, accID)
	}

	// clear out to not compare generated addresses
	got.Change[0].Receiver = nil

	if !reflect.DeepEqual(got, want) {
		t.Errorf("reserve result\ngot:\n\t%+v\nwant:\n\t%+v", got, want)
		t.Errorf("reserve item\ngot:\n\t%+v\nwant:\n\t%+v", got.Items[0], want.Items[0])
		t.Errorf("reserve txin\ngot:\n\t%+v\nwant:\n\t%+v", got.Items[0].TxInput, want.Items[0].TxInput)
		t.Errorf("reserve change\ngot:\n\t%+v\nwant:\n\t%+v", got.Change, want.Change)
	}
}

func TestAccountSourceReserveIdempotency(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store := txdb.NewStore(pg.FromContext(ctx).(*sql.DB))
	_, err := assettest.InitializeSigningGenerator(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	var (
		accID        = assettest.CreateAccountFixture(ctx, t, "", "", nil)
		asset        = assettest.CreateAssetFixture(ctx, t, "", "asset-0", "")
		_            = assettest.IssueAssetsFixture(ctx, t, asset, 2, accID)
		_            = assettest.IssueAssetsFixture(ctx, t, asset, 2, accID)
		assetAmount1 = &bc.AssetAmount{
			AssetID: asset,
			Amount:  1,
		}

		// An idempotency key that both reservations should use.
		clientToken1 = "a-unique-idempotency-key"
		clientToken2 = "another-unique-idempotency-key"
		wantSrc      = NewAccountSource(ctx, assetAmount1, accID, nil, &clientToken1)
		gotSrc       = NewAccountSource(ctx, assetAmount1, accID, nil, &clientToken1)
		separateSrc  = NewAccountSource(ctx, assetAmount1, accID, nil, &clientToken2)
	)

	reserveFunc := func(source *txbuilder.Source) *txbuilder.ReserveResult {
		got, err := source.Reserver.Reserve(ctx, assetAmount1, time.Minute)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}
		if len(got.Items) != 1 {
			t.Fatalf("expected 1 result utxo")
		}
		// generated address can change based on test ordering, so ignore in comparison
		got.Items[0].TemplateInput = nil

		ar, ok := got.Change[0].Receiver.(*AccountReceiver)
		if !ok {
			t.Fatalf("expected change destination to have AccountReceiver")
		}
		if ar.Addr().AccountID != accID {
			t.Errorf("got receiver addr account = %v want %v", ar.Addr().AccountID, accID)
		}
		// clear out to not compare generated addresses
		got.Change[0].Receiver = nil
		return got
	}

	var (
		got      = reserveFunc(gotSrc)
		want     = reserveFunc(wantSrc)
		separate = reserveFunc(separateSrc)
	)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reserve result\ngot:\n\t%+v\nwant:\n\t%+v", got, want)
		t.Errorf("reserve item\ngot:\n\t%+v\nwant:\n\t%+v", got.Items[0], want.Items[0])
		t.Errorf("reserve txin\ngot:\n\t%+v\nwant:\n\t%+v", got.Items[0].TxInput, want.Items[0].TxInput)
		t.Errorf("reserve change\ngot:\n\t%+v\nwant:\n\t%+v", got.Change, want.Change)
	}

	// The third reservation attempt should be distinct and not the same as the first two.
	if reflect.DeepEqual(separate, want) {
		t.Errorf("reserve result\ngot:\n\t%+v\ndo not want:\n\t%+v", separate, want)
		t.Errorf("reserve item\ngot:\n\t%+v\ndo not want:\n\t%+v", separate.Items[0], want.Items[0])
		t.Errorf("reserve txin\ngot:\n\t%+v\ndo not want:\n\t%+v", separate.Items[0].TxInput, want.Items[0].TxInput)
		t.Errorf("reserve change\ngot:\n\t%+v\ndo not want:\n\t%+v", separate.Change, want.Change)
	}
}

func TestAccountDestinationPKScript(t *testing.T) {
	ctx := pgtest.NewContext(t)

	acc := assettest.CreateAccountFixture(ctx, t, "", "", nil)

	// Test account output pk script (address creation)
	dest, err := NewAccountDestination(ctx, &bc.AssetAmount{Amount: 1}, acc, nil)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}
	got := dest.PKScript()

	receiver := dest.Receiver
	accountReceiver, ok := receiver.(*AccountReceiver)
	if !ok {
		t.Log(errors.Stack(err))
		t.Fatal("receiver is not an AccountReceiver")
	}
	addr := accountReceiver.Addr()
	want, _, err := hdkey.Scripts(addr.Keys, appdb.ReceiverPath(addr, addr.Index), addr.SigsRequired)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}
	testutil.ExpectScriptEqual(t, got, want, "AccountDestination pk script")
}

func TestAccountSourceWithTxHash(t *testing.T) {
	ctx := pgtest.NewContext(t)
	store := txdb.NewStore(pg.FromContext(ctx).(*sql.DB))
	_, err := assettest.InitializeSigningGenerator(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	var (
		acc      = assettest.CreateAccountFixture(ctx, t, "", "", nil)
		asset    = assettest.CreateAssetFixture(ctx, t, "", "asset-0", "")
		assetAmt = bc.AssetAmount{AssetID: asset, Amount: 1}
		utxos    = 4
		srcTxs   []bc.Hash
	)

	for i := 0; i < utxos; i++ {
		o := assettest.IssueAssetsFixture(ctx, t, asset, 1, acc)
		srcTxs = append(srcTxs, o.Outpoint.Hash)
	}

	for i := 0; i < utxos; i++ {
		theTxHash := srcTxs[i]
		source := NewAccountSource(ctx, &assetAmt, acc, &theTxHash, nil)

		gotRes, err := source.Reserver.Reserve(ctx, &assetAmt, time.Minute)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}

		if len(gotRes.Items) != 1 {
			t.Fatalf("expected 1 result utxo")
		}

		got := gotRes.Items[0].TxInput.Previous
		want := bc.Outpoint{Hash: theTxHash, Index: 0}
		if got != want {
			t.Errorf("reserved utxo outpoint got=%v want=%v", got, want)
		}
	}
}

func TestBreakupChange(t *testing.T) {
	got := BreakupChange(1)
	want := []uint64{1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}

	// Now do a lot of BreakupChange calls and expect that at least one
	// of them will create two or more pieces.  Check that in all cases
	// the pieces add up to the input.
	var anyMultiples bool
	for i := 0; i < 100; i++ {
		got := BreakupChange(100)
		var sum uint64
		for _, n := range got {
			sum += n
		}
		if sum != 100 {
			t.Errorf("sum of %v is %d, not 100", got, sum)
		}
		if len(got) > 1 {
			anyMultiples = true
		}
	}

	if !anyMultiples {
		t.Errorf("no calls produced multiple change pieces, that's very unlikely")
	}
}
