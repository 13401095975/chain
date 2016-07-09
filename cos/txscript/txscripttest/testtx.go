package txscripttest

import (
	"fmt"

	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/cos/state"
	"chain/cos/txscript"
)

var testTxHash = bc.Hash{}

// NewTestTx constructs a fresh TestTx.
func NewTestTx() *TestTx {
	return &TestTx{
		view: state.NewMemView(nil, nil),
	}
}

// TestTx is used to build a bc.TxData and corresponding state.MemView
// in order to test the execution of a pkscript.
type TestTx struct {
	view      *state.MemView
	data      bc.TxData
	utxoIndex uint32
}

// AddInput adds a new input to the transaction and adds a corresponding
// utxo to the view.
func (tx *TestTx) AddInput(assetAmount bc.AssetAmount, pkscript, sigscript []byte) *TestTx {
	prevOutpoint := bc.Outpoint{
		Hash:  testTxHash,
		Index: tx.utxoIndex,
	}
	tx.utxoIndex++

	// Save the utxo outpoint to the view.
	tx.view.AddUTXO(&state.Output{
		TxOutput: bc.TxOutput{
			AssetAmount: assetAmount,
			Script:      pkscript,
		},
		Outpoint: prevOutpoint,
	})

	// Add the tx input to the current transaction.
	tx.data.Inputs = append(tx.data.Inputs, &bc.TxInput{
		Previous:        prevOutpoint,
		AssetAmount:     assetAmount,
		PrevScript:      pkscript,
		SignatureScript: sigscript,
	})
	return tx
}

// AddOutput adds a new output to the transaction.
func (tx *TestTx) AddOutput(assetAmount bc.AssetAmount, pkscript []byte) *TestTx {
	tx.data.Outputs = append(tx.data.Outputs, &bc.TxOutput{
		AssetAmount: assetAmount,
		Script:      pkscript,
	})
	return tx
}

// Execute constructs a new txscript.Engine and executes the pkscript for
// the input at the provided index.
func (tx *TestTx) Execute(ctx context.Context, inputIndex int) error {
	if inputIndex >= len(tx.data.Inputs) {
		return fmt.Errorf("input index %d; tx only has %d inputs", inputIndex, len(tx.data.Inputs))
	}

	input := tx.data.Inputs[inputIndex]
	vm, err := txscript.NewEngine(ctx, input.PrevScript, &tx.data, inputIndex, txscript.ScriptVerifyMinimalData)
	if err != nil {
		return err
	}
	return vm.Execute()
}
