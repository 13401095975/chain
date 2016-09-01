package protocol

import (
	"context"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"chain/crypto/ed25519"
	"chain/errors"
	"chain/protocol/bc"
	"chain/protocol/mempool"
	"chain/protocol/memstore"
	"chain/protocol/state"
	"chain/protocol/vm"
	"chain/testutil"
)

func TestGetBlock(t *testing.T) {
	ctx := context.Background()

	emptyPool := mempool.New()
	noBlocks := memstore.New()
	oneBlock := memstore.New()
	oneBlock.SaveBlock(ctx, &bc.Block{})
	oneBlock.SaveSnapshot(ctx, 1, state.Empty())

	cases := []struct {
		store   Store
		want    *bc.Block
		wantErr error
	}{
		{noBlocks, nil, nil},
		{oneBlock, &bc.Block{}, nil},
	}

	for _, test := range cases {
		c, err := NewChain(ctx, test.store, emptyPool, nil)
		if err != nil {
			testutil.FatalErr(t, err)
		}
		got, gotErr := c.GetBlock(ctx, c.Height())

		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("got latest = %+v want %+v", got, test.want)
		}

		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("got latest err = %q want %q", gotErr, test.wantErr)
		}
	}
}

func TestNoTimeTravel(t *testing.T) {
	ctx := context.Background()
	c, err := NewChain(ctx, memstore.New(), mempool.New(), nil)
	if err != nil {
		t.Fatal(err)
	}

	c.setHeight(1)
	c.setHeight(2)

	c.setHeight(1) // don't go backward
	if c.height.n != 2 {
		t.Fatalf("c.height.n = %d want 2", c.height.n)
	}
}

func TestWaitForBlockSoon(t *testing.T) {
	ctx := context.Background()
	store := memstore.New()
	block1 := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:           1,
			ConsensusProgram: []byte{byte(vm.OP_TRUE)},
		},
	}
	block2 := &bc.Block{
		BlockHeader: bc.BlockHeader{
			PreviousBlockHash: block1.Hash(),
			Height:            2,
			ConsensusProgram:  []byte{byte(vm.OP_TRUE)},
		},
	}
	block3 := &bc.Block{
		BlockHeader: bc.BlockHeader{
			PreviousBlockHash: block2.Hash(),
			Height:            3,
			ConsensusProgram:  []byte{byte(vm.OP_TRUE)},
		},
	}
	store.SaveBlock(ctx, block1)
	store.SaveSnapshot(ctx, 1, state.Empty())
	c, err := NewChain(ctx, store, mempool.New(), nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	ch := waitForBlockChan(c, 1)
	select {
	case err := <-ch:
		if err != nil {
			t.Errorf("got err %q waiting for block 0", err)
		}
	case <-time.After(10 * time.Millisecond):
		t.Errorf("timed out waiting for block 0")
	}

	ch = waitForBlockChan(c, 5)
	select {
	case err := <-ch:
		if err != ErrTheDistantFuture {
			t.Errorf("got %q waiting for block 5, expected %q", err, ErrTheDistantFuture)
		}
	case <-time.After(10 * time.Millisecond):
		t.Errorf("timed out waiting for block 5")
	}

	ch = waitForBlockChan(c, 2)

	select {
	case <-ch:
		t.Errorf("WaitForBlockSoon should wait")
	default:
	}

	err = c.CommitBlock(ctx, block2, state.Empty())
	if err != nil {
		testutil.FatalErr(t, err)
	}

	select {
	case <-ch:
		t.Errorf("WaitForBlockSoon should wait")
	default:
	}

	err = c.CommitBlock(ctx, block3, state.Empty())
	if err != nil {
		testutil.FatalErr(t, err)
	}

	select {
	case err := <-ch:
		if err != nil {
			t.Errorf("got err %q waiting for block 3", err)
		}
	case <-time.After(10 * time.Millisecond):
		t.Errorf("timed out waiting for block 3")
	}
}

func waitForBlockChan(c *Chain, height uint64) chan error {
	ch := make(chan error)
	go func() {
		err := c.WaitForBlockSoon(height)
		ch <- err
	}()
	return ch
}

func TestGenerateBlock(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(233400000, 0)
	c, b1 := newTestChain(t, now)

	initialBlockHash := b1.Hash()
	assetID := bc.ComputeAssetID(nil, initialBlockHash, 1)

	txs := []*bc.Tx{
		bc.NewTx(bc.TxData{
			Version: 1,
			Inputs: []*bc.TxInput{
				bc.NewIssuanceInput(now, now.Add(time.Hour), initialBlockHash, 50, nil, nil, [][]byte{
					nil,
					mustDecodeHex("30450221009037e1d39b7d59d24eba8012baddd5f4ab886a51b46f52b7c479ddfa55eeb5c5022076008409243475b25dfba6db85e15cf3d74561a147375941e4830baa69769b5101"),
					mustDecodeHex("51210210b002870438af79b829bc22c4505e14779ef0080c411ad497d7a0846ee0af6f51ae")}),
			},
			Outputs: []*bc.TxOutput{
				bc.NewTxOutput(assetID, 50, mustDecodeHex("a9145881cd104f8d64635751ac0f3c0decf9150c110687"), nil),
			},
		}),
		bc.NewTx(bc.TxData{
			Version: 1,
			Inputs: []*bc.TxInput{
				bc.NewIssuanceInput(now, now.Add(time.Hour), initialBlockHash, 50, nil, nil, [][]byte{
					nil,
					mustDecodeHex("3045022100f3bcffcfd6a1ce9542b653500386cd0ee7b9c86c59390ca0fc0238c0ebe3f1d6022065ac468a51a016842660c3a616c99a9aa5109a3bad1877ba3e0f010f3972472e01"),
					mustDecodeHex("51210210b002870438af79b829bc22c4505e14779ef0080c411ad497d7a0846ee0af6f51ae"),
				}),
			},
			Outputs: []*bc.TxOutput{
				bc.NewTxOutput(assetID, 50, mustDecodeHex("a914c171e443e05b953baa7b7d834028ed91e47b4d0b87"), nil),
			},
		}),
	}
	for _, tx := range txs {
		err := c.pool.Insert(ctx, tx)
		if err != nil {
			t.Log(errors.Stack(err))
			t.Fatal(err)
		}
	}

	got, _, err := c.GenerateBlock(ctx, b1, state.Empty(), now)
	if err != nil {
		t.Fatalf("err got = %v want nil", err)
	}

	// TODO(bobg): verify these hashes are correct
	var wantTxRoot, wantAssetsRoot bc.Hash
	copy(wantTxRoot[:], mustDecodeHex("29a11bc539b34ffc05725e0ddc99edce3d054fd02741a21c5ea53d38d7014a09"))
	copy(wantAssetsRoot[:], mustDecodeHex("dddc627ae1e1f0872ca2b80d4d13866cae2ccbdb177fce8c6dd75d371d946cda"))

	want := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Version:                bc.NewBlockVersion,
			Height:                 2,
			PreviousBlockHash:      b1.Hash(),
			TransactionsMerkleRoot: wantTxRoot,
			AssetsMerkleRoot:       wantAssetsRoot,
			TimestampMS:            bc.Millis(now),
			ConsensusProgram:       b1.ConsensusProgram,
		},
		Transactions: txs,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("generated block:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestValidateBlockForSig(t *testing.T) {
	initialBlock, err := NewInitialBlock(testutil.TestPubs, 1, time.Now())
	if err != nil {
		t.Fatal("unexpected error ", err)
	}

	ctx := context.Background()
	c, err := NewChain(ctx, memstore.New(), mempool.New(), nil)
	if err != nil {
		t.Fatal("unexpected error ", err)
	}

	err = c.ValidateBlockForSig(ctx, initialBlock)
	if err != nil {
		t.Error("unexpected error ", err)
	}
}

// newTestChain returns a new Chain using memstore and mempool for storage,
// along with an initial block b1 (with a 0/0 multisig program).
// It commits b1 before returning.
func newTestChain(tb testing.TB, ts time.Time) (c *Chain, b1 *bc.Block) {
	ctx := context.Background()
	c, err := NewChain(ctx, memstore.New(), mempool.New(), nil)
	if err != nil {
		testutil.FatalErr(tb, err)
	}
	b1, err = NewInitialBlock(nil, 0, ts)
	if err != nil {
		testutil.FatalErr(tb, err)
	}
	err = c.CommitBlock(ctx, b1, state.Empty())
	if err != nil {
		testutil.FatalErr(tb, err)
	}
	return c, b1
}

func signBlock(t testing.TB, b *bc.Block, keys []ed25519.PrivateKey) {
	var sigs [][]byte
	for _, key := range keys {
		hash := b.HashForSig()
		sig := ed25519.Sign(key, hash[:])
		sigs = append(sigs, sig)
	}
	b.Witness = sigs
}

func privToPub(privs []ed25519.PrivateKey) []ed25519.PublicKey {
	var public []ed25519.PublicKey
	for _, priv := range privs {
		public = append(public, priv.Public().(ed25519.PublicKey))
	}
	return public
}

func newPrivKey(t *testing.T) ed25519.PrivateKey {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func mustDecodeHex(s string) []byte {
	data, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return data
}
