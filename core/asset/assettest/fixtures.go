package assettest

import (
	"context"
	"testing"
	"time"

	"chain/core/account"
	"chain/core/asset"
	"chain/core/txbuilder"
	"chain/encoding/json"
	"chain/errors"
	"chain/protocol"
	"chain/protocol/bc"
	"chain/protocol/state"
	"chain/testutil"
)

func CreateAccountFixture(ctx context.Context, t testing.TB, keys []string, quorum int, alias string, tags map[string]interface{}) string {
	if keys == nil {
		keys = []string{testutil.TestXPub.String()}
	}
	if quorum == 0 {
		quorum = len(keys)
	}
	acc, err := account.Create(ctx, keys, quorum, alias, tags, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	return acc.ID
}

func CreateAccountControlProgramFixture(ctx context.Context, t testing.TB, accID string) []byte {
	if accID == "" {
		accID = CreateAccountFixture(ctx, t, nil, 0, "", nil)
	}
	controlProgram, err := account.CreateControlProgram(ctx, accID)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	return controlProgram
}

func CreateAssetFixture(ctx context.Context, t testing.TB, keys []string, quorum int, def map[string]interface{}, alias string, tags map[string]interface{}) bc.AssetID {
	if len(keys) == 0 {
		keys = []string{testutil.TestXPub.String()}
	}

	if quorum == 0 {
		quorum = len(keys)
	}
	var initialBlockHash bc.Hash

	asset, err := asset.Define(ctx, keys, quorum, def, initialBlockHash, alias, tags, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	return asset.AssetID
}

func IssueAssetsFixture(ctx context.Context, t testing.TB, c *protocol.Chain, assetID bc.AssetID, amount uint64, accountID string) state.Output {
	if accountID == "" {
		accountID = CreateAccountFixture(ctx, t, nil, 0, "", nil)
	}
	dest := NewAccountControlAction(bc.AssetAmount{AssetID: assetID, Amount: amount}, accountID, nil)

	assetAmount := bc.AssetAmount{AssetID: assetID, Amount: amount}

	src := NewIssueAction(assetAmount, nil) // does not support reference data
	tpl, err := txbuilder.Build(ctx, nil, []txbuilder.Action{dest, src}, nil, bc.Millis(time.Now().Add(time.Minute)))
	if err != nil {
		testutil.FatalErr(t, err)
	}

	SignTxTemplate(t, tpl, testutil.TestXPrv)

	tx, err := txbuilder.FinalizeTx(ctx, c, tpl)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	return state.Output{
		Outpoint: bc.Outpoint{Hash: tx.Hash, Index: 0},
		TxOutput: *tx.Outputs[0],
	}
}

func Issue(ctx context.Context, t testing.TB, c *protocol.Chain, assetID bc.AssetID, amount uint64, actions []txbuilder.Action) *bc.Tx {
	assetAmount := bc.AssetAmount{AssetID: assetID, Amount: amount}
	actions = append(actions, NewIssueAction(assetAmount, nil))

	txTemplate, err := txbuilder.Build(
		ctx,
		nil,
		actions,
		nil,
		bc.Millis(time.Now().Add(time.Minute)),
	)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}
	SignTxTemplate(t, txTemplate, nil)
	tx, err := txbuilder.FinalizeTx(ctx, c, txTemplate)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	return tx
}

func Transfer(ctx context.Context, t testing.TB, c *protocol.Chain, actions []txbuilder.Action) *bc.Tx {
	template, err := txbuilder.Build(ctx, nil, actions, nil, bc.Millis(time.Now().Add(time.Minute)))
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	SignTxTemplate(t, template, testutil.TestXPrv)

	tx, err := txbuilder.FinalizeTx(ctx, c, template)
	if err != nil {
		t.Log(errors.Stack(err))
		t.Fatal(err)
	}

	return tx
}

func NewIssueAction(assetAmount bc.AssetAmount, referenceData json.Map) *asset.IssueAction {
	return &asset.IssueAction{
		Params: struct {
			bc.AssetAmount
			TTL        time.Duration
			MinTime    *time.Time `json:"min_time"`
			AssetAlias string     `json:"asset_alias"`
		}{assetAmount, 0, nil, ""},
		ReferenceData: referenceData,
	}
}

func NewAccountSpendAction(amt bc.AssetAmount, accountID string, txHash *bc.Hash, txOut *uint32, refData json.Map, constraints []txbuilder.Constraint) *account.SpendAction {
	return &account.SpendAction{
		Params: struct {
			bc.AssetAmount
			AccountID    string        `json:"account_id"`
			TxHash       *bc.Hash      `json:"transaction_id"`
			TxOut        *uint32       `json:"position"`
			TTL          time.Duration `json:"reservation_ttl"`
			AccountAlias string        `json:"account_alias"`
			AssetAlias   string        `json:"asset_alias"`
		}{
			AssetAmount:  amt,
			AssetAlias:   "",
			TxHash:       txHash,
			TxOut:        txOut,
			AccountID:    accountID,
			AccountAlias: "",
		},
		Constraints:   constraints,
		ReferenceData: refData,
	}
}

func NewAccountControlAction(amt bc.AssetAmount, accountID string, refData json.Map) *account.ControlAction {
	return &account.ControlAction{
		Params: struct {
			bc.AssetAmount
			AccountID    string `json:"account_id"`
			AccountAlias string `json:"account_alias"`
			AssetAlias   string `json:"asset_alias"`
		}{amt, accountID, "", ""},
		ReferenceData: refData,
	}
}
