package account

import (
	"context"
	"encoding/json"
	"time"

	"chain/core/account/utxodb"
	"chain/core/signers"
	"chain/core/txbuilder"
	chainjson "chain/encoding/json"
	"chain/errors"
	"chain/protocol/bc"
)

func NewSpendAction(amt bc.AssetAmount, accountID string, txHash *bc.Hash, txOut *uint32, refData chainjson.Map, clientToken *string) txbuilder.Action {
	return &spendAction{
		AssetAmount:   amt,
		TxHash:        txHash,
		TxOut:         txOut,
		AccountID:     accountID,
		ReferenceData: refData,
		ClientToken:   clientToken,
	}
}

func DecodeSpendAction(data []byte) (txbuilder.Action, error) {
	a := new(spendAction)
	err := json.Unmarshal(data, a)
	return a, err
}

type spendAction struct {
	bc.AssetAmount
	AccountID     string        `json:"account_id"`
	TxHash        *bc.Hash      `json:"transaction_id"`
	TxOut         *uint32       `json:"position"`
	TTL           time.Duration `json:"reservation_ttl"`
	ReferenceData chainjson.Map `json:"reference_data"`
	ClientToken   *string       `json:"client_token"`
}

func (a *spendAction) Build(ctx context.Context) (*txbuilder.BuildResult, error) {
	ttl := a.TTL
	if ttl == 0 {
		ttl = time.Minute
	}
	maxTime := time.Now().Add(ttl)

	acct, err := findByID(ctx, a.AccountID)
	if err != nil {
		return nil, errors.Wrap(err, "get account info")
	}

	utxodbSource := utxodb.Source{
		AssetID:     a.AssetID,
		Amount:      a.Amount,
		AccountID:   a.AccountID,
		TxHash:      a.TxHash,
		OutputIndex: a.TxOut,
		ClientToken: a.ClientToken,
	}
	utxodbSources := []utxodb.Source{utxodbSource}
	reserved, change, err := utxodb.Reserve(ctx, utxodbSources, maxTime)
	if err != nil {
		return nil, errors.Wrap(err, "reserving utxos")
	}

	var (
		txins      []*bc.TxInput
		tplInsts   []*txbuilder.SigningInstruction
		changeOuts []*bc.TxOutput
	)

	for _, r := range reserved {
		txInput, sigInst, err := utxoToInputs(ctx, acct, r, a.ReferenceData)
		if err != nil {
			return nil, errors.Wrap(err, "creating inputs")
		}

		txins = append(txins, txInput)
		tplInsts = append(tplInsts, sigInst)
	}
	if len(change) > 0 {
		acp, err := CreateControlProgram(ctx, a.AccountID, true)
		if err != nil {
			return nil, errors.Wrap(err, "creating control program")
		}
		changeOuts = append(changeOuts, bc.NewTxOutput(a.AssetID, change[0].Amount, acp, nil))
	}

	return &txbuilder.BuildResult{
		Inputs:              txins,
		Outputs:             changeOuts,
		SigningInstructions: tplInsts,
		MaxTimeMS:           bc.Millis(maxTime),
	}, nil
}

func NewSpendUTXOAction(outpoint bc.Outpoint, ttl time.Duration) txbuilder.Action {
	return &spendUTXOAction{
		TxHash: outpoint.Hash,
		TxOut:  outpoint.Index,
		TTL:    ttl,
	}
}

func DecodeSpendUTXOAction(data []byte) (txbuilder.Action, error) {
	a := new(spendUTXOAction)
	err := json.Unmarshal(data, a)
	return a, err
}

type spendUTXOAction struct {
	TxHash bc.Hash       `json:"transaction_id"`
	TxOut  uint32        `json:"position"`
	TTL    time.Duration `json:"reservation_ttl"`

	ReferenceData chainjson.Map `json:"reference_data"`
	ClientToken   *string       `json:"client_token"`
}

func (a *spendUTXOAction) Build(ctx context.Context) (*txbuilder.BuildResult, error) {
	ttl := a.TTL
	if ttl == 0 {
		ttl = time.Minute
	}
	maxTime := time.Now().Add(ttl)

	r, err := utxodb.ReserveUTXO(ctx, a.TxHash, a.TxOut, a.ClientToken, maxTime)
	if err != nil {
		return nil, err
	}

	acct, err := findByID(ctx, r.AccountID)
	if err != nil {
		return nil, err
	}

	txInput, sigInst, err := utxoToInputs(ctx, acct, r, a.ReferenceData)
	if err != nil {
		return nil, err
	}

	return &txbuilder.BuildResult{
		Inputs:              []*bc.TxInput{txInput},
		SigningInstructions: []*txbuilder.SigningInstruction{sigInst},
		MaxTimeMS:           bc.Millis(maxTime),
	}, nil
}

func utxoToInputs(ctx context.Context, account *signers.Signer, u *utxodb.UTXO, refData []byte) (
	*bc.TxInput,
	*txbuilder.SigningInstruction,
	error,
) {
	txInput := bc.NewSpendInput(u.Hash, u.Index, nil, u.AssetID, u.Amount, u.Script, refData)

	sigInst := &txbuilder.SigningInstruction{
		AssetAmount: u.AssetAmount,
	}

	path := signers.Path(account, signers.AccountKeySpace, u.ControlProgramIndex)
	keyIDs := txbuilder.KeyIDs(account.XPubs, path)

	sigInst.AddWitnessKeys(keyIDs, account.Quorum)

	return txInput, sigInst, nil
}

func NewControlAction(amt bc.AssetAmount, accountID string, refData chainjson.Map) txbuilder.Action {
	return &controlAction{
		AssetAmount:   amt,
		AccountID:     accountID,
		ReferenceData: refData,
	}
}

func DecodeControlAction(data []byte) (txbuilder.Action, error) {
	a := new(controlAction)
	err := json.Unmarshal(data, a)
	return a, err
}

type controlAction struct {
	bc.AssetAmount
	AccountID     string        `json:"account_id"`
	ReferenceData chainjson.Map `json:"reference_data"`
}

func (a *controlAction) Build(ctx context.Context) (*txbuilder.BuildResult, error) {
	acp, err := CreateControlProgram(ctx, a.AccountID, false)
	if err != nil {
		return nil, err
	}
	out := bc.NewTxOutput(a.AssetID, a.Amount, acp, a.ReferenceData)
	return &txbuilder.BuildResult{Outputs: []*bc.TxOutput{out}}, nil
}
