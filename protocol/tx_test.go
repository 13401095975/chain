package protocol

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/sha3"

	"chain/crypto/ed25519"
	"chain/protocol/bc"
	"chain/protocol/state"
	"chain/protocol/vm"
	"chain/protocol/vmutil"
	"chain/testutil"
)

func TestIdempotentAddTx(t *testing.T) {
	ctx := context.Background()
	c, b1 := newTestChain(t, time.Now())

	issueTx, _, _ := issue(t, nil, nil, 1)

	err := c.AddTx(ctx, issueTx)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	// still idempotent after block lands
	block, tree, err := c.GenerateBlock(ctx, b1, state.Empty(), time.Now())
	if err != nil {
		testutil.FatalErr(t, err)
	}

	err = c.CommitBlock(ctx, block, tree)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	err = c.AddTx(ctx, issueTx)
	if err != nil {
		testutil.FatalErr(t, err)
	}
}

func TestAddTx(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestChain(t, time.Now())

	issueTx, _, dest1 := issue(t, nil, nil, 1)
	err := c.AddTx(ctx, issueTx)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	transferTx := transfer(t, stateOut(issueTx, 0), dest1, newDest(t))

	err = c.AddTx(ctx, transferTx)
	if err != nil {
		testutil.FatalErr(t, err)
	}
}

type testDest struct {
	privKey ed25519.PrivateKey
}

func newDest(t testing.TB) *testDest {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	return &testDest{
		privKey: priv,
	}
}

func (d *testDest) sign(t testing.TB, tx *bc.TxData, index int) {
	prog := []byte{byte(vm.OP_TRUE)}
	h := sha3.Sum256(prog)
	sig := ed25519.Sign(d.privKey, h[:])
	tx.Inputs[index].InputWitness = [][]byte{sig, prog}
}

func (d testDest) controlProgram() []byte {
	pub := d.privKey.Public().(ed25519.PublicKey)
	return vmutil.P2DPMultiSigProgram([]ed25519.PublicKey{pub}, 1)
}

type testAsset struct {
	bc.AssetID
	testDest
}

func newAsset(t testing.TB) *testAsset {
	dest := newDest(t)
	assetID := bc.ComputeAssetID(dest.controlProgram(), bc.Hash{}, 1)

	return &testAsset{
		AssetID:  assetID,
		testDest: *dest,
	}
}

func issue(t testing.TB, asset *testAsset, dest *testDest, amount uint64) (*bc.Tx, *testAsset, *testDest) {
	if asset == nil {
		asset = newAsset(t)
	}
	if dest == nil {
		dest = newDest(t)
	}
	tx := &bc.TxData{
		Version: bc.CurrentTransactionVersion,
		Inputs: []*bc.TxInput{
			bc.NewIssuanceInput(time.Now(), time.Now().Add(time.Hour), bc.Hash{}, amount, asset.controlProgram(), nil, nil),
		},
		Outputs: []*bc.TxOutput{
			bc.NewTxOutput(asset.AssetID, amount, dest.controlProgram(), nil),
		},
	}
	asset.sign(t, tx, 0)

	return bc.NewTx(*tx), asset, dest
}

func transfer(t testing.TB, out *state.Output, from, to *testDest) *bc.Tx {
	tx := &bc.TxData{
		Version: bc.CurrentTransactionVersion,
		Inputs: []*bc.TxInput{
			bc.NewSpendInput(out.Hash, out.Index, nil, out.AssetID, out.Amount, out.ControlProgram, nil),
		},
		Outputs: []*bc.TxOutput{
			bc.NewTxOutput(out.AssetID, out.Amount, to.controlProgram(), nil),
		},
	}
	from.sign(t, tx, 0)

	return bc.NewTx(*tx)
}

func stateOut(tx *bc.Tx, index int) *state.Output {
	return &state.Output{
		TxOutput: *tx.Outputs[index],
		Outpoint: bc.Outpoint{Hash: tx.Hash, Index: uint32(index)},
	}
}
