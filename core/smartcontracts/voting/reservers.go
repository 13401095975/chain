package voting

import (
	"errors"
	"time"

	"golang.org/x/net/context"

	"chain/core/appdb"
	"chain/core/txbuilder"
	"chain/cos/bc"
	"chain/cos/hdkey"
	"chain/cos/txscript"
	"chain/crypto/hash256"
)

// rightsReserver implements txbuilder.Reserver for assets in the voting
// rights holding contract
type rightsReserver struct {
	outpoint       bc.Outpoint
	clause         rightsContractClause
	output         rightScriptData
	intermediaries []intermediateHolder
	prevScript     []byte
	holderAddr     *appdb.Address
	adminAddr      *appdb.Address
}

// intermediateHolder represents a previous holder. When recalling a token,
// you must provide all intermediate holders between the recall point and the
// current utxo.
type intermediateHolder struct {
	script   []byte
	deadline int64
}

// hash returns Hash256(Hash256(script) + Hash256(deadline)). This hash is
// used within the chain of ownership hash chain. When invoking the recall
// clause of the contract, it's necessary to provide these hashes for all
// intermediate holders between the recall holder and the current holder
// to prove prior ownership.
func (ih intermediateHolder) hash() bc.Hash {
	scriptHash := hash256.Sum(ih.script)
	deadlineHash := hash256.Sum(txscript.Int64ToScriptBytes(ih.deadline))

	data := make([]byte, 0, len(scriptHash)+len(deadlineHash))
	data = append(data, scriptHash[:]...)
	data = append(data, deadlineHash[:]...)
	return hash256.Sum(data)
}

// Reserve builds a ReserveResult including the sigscript suffix to satisfy
// the existing UTXO's right holding contract. Reserve satisfies the
// txbuilder.Reserver interface.
func (r rightsReserver) Reserve(ctx context.Context, assetAmount *bc.AssetAmount, ttl time.Duration) (*txbuilder.ReserveResult, error) {
	var (
		sigscript []*txbuilder.SigScriptComponent
		addrs     []appdb.Address
	)
	if r.holderAddr != nil {
		addrs = append(addrs, *r.holderAddr)
	}
	if r.adminAddr != nil {
		addrs = append(addrs, *r.adminAddr)
	}

	for _, addr := range addrs {
		sigscript = append(sigscript,
			&txbuilder.SigScriptComponent{
				Type:     "signature",
				Required: addr.SigsRequired,
				Signatures: txbuilder.InputSigs(
					hdkey.Derive(addr.Keys, appdb.ReceiverPath(&addr, addr.Index)),
				),
			}, &txbuilder.SigScriptComponent{
				Type:   "script",
				Script: txscript.AddDataToScript(nil, addr.RedeemScript),
			})
	}

	var inputs []txscript.Item

	// Add clause-specific parameters:
	switch r.clause {
	case clauseAuthenticate:
		// No clause-specific parameters.
	case clauseTransfer:
		inputs = append(inputs, txscript.DataItem(r.output.HolderScript))
	case clauseDelegate:
		inputs = append(inputs, txscript.NumItem(r.output.Deadline))
		inputs = append(inputs, txscript.BoolItem(r.output.Delegatable))
		inputs = append(inputs, txscript.DataItem(r.output.HolderScript))
	case clauseRecall:
		for _, i := range r.intermediaries {
			h := i.hash()
			inputs = append(inputs, txscript.DataItem(h[:]))
		}
		inputs = append(inputs, txscript.NumItem(int64(len(r.intermediaries))))
		inputs = append(inputs, txscript.DataItem(r.output.HolderScript))
		inputs = append(inputs, txscript.NumItem(r.output.Deadline))
		inputs = append(inputs, txscript.DataItem(r.output.OwnershipChain[:]))
	case clauseOverride, clauseCancel:
		// TODO(jackson): Implement.
		return nil, errors.New("unimplemented")
	}
	inputs = append(inputs, txscript.NumItem(r.clause))

	script, err := txscript.RedeemP2C(r.prevScript, rightsHoldingContract, inputs)

	if err != nil {
		return nil, err
	}
	sigscript = append(sigscript, &txbuilder.SigScriptComponent{
		Type:   "script",
		Script: script,
	})

	result := &txbuilder.ReserveResult{
		Items: []*txbuilder.ReserveResultItem{
			{
				TxInput: &bc.TxInput{
					Previous:    r.outpoint,
					AssetAmount: *assetAmount,
					PrevScript:  r.prevScript,
				},
				TemplateInput: &txbuilder.Input{
					AssetAmount:   *assetAmount,
					SigComponents: sigscript,
				},
			},
		},
	}
	return result, nil
}

type tokenReserver struct {
	outpoint    bc.Outpoint
	clause      tokenContractClause
	output      tokenScriptData
	rightScript []byte
	prevScript  []byte
	secret      []byte
	adminAddr   *appdb.Address
}

// Reserve builds a ReserveResult including the sigscript suffix to satisfy
// the existing UTXO's token holding contract. Reserve satisfies the
// txbuilder.Reserver interface.
func (r tokenReserver) Reserve(ctx context.Context, assetAmount *bc.AssetAmount, ttl time.Duration) (*txbuilder.ReserveResult, error) {
	var sigscript []*txbuilder.SigScriptComponent

	if r.adminAddr != nil {
		sigscript = append(sigscript,
			&txbuilder.SigScriptComponent{
				Type:     "signature",
				Required: r.adminAddr.SigsRequired,
				Signatures: txbuilder.InputSigs(
					hdkey.Derive(r.adminAddr.Keys, appdb.ReceiverPath(r.adminAddr, r.adminAddr.Index)),
				),
			}, &txbuilder.SigScriptComponent{
				Type:   "script",
				Script: txscript.AddDataToScript(nil, r.adminAddr.RedeemScript),
			},
		)
	}

	var inputs []txscript.Item

	switch r.clause {
	case clauseRegister:
		inputs = append(inputs, txscript.DataItem(r.rightScript))
	case clauseVote:
		inputs = append(inputs, txscript.NumItem(r.output.Vote))
		inputs = append(inputs, txscript.DataItem(r.secret))
		inputs = append(inputs, txscript.DataItem(r.rightScript))
	case clauseFinish, clauseRetire:
		// No clause-specific parameters.
	case clauseReset:
		inputs = append(inputs, txscript.NumItem(r.output.State))
		inputs = append(inputs, txscript.DataItem(r.output.SecretHash[:]))
	}
	inputs = append(inputs, txscript.NumItem(int64(r.clause)))

	script, err := txscript.RedeemP2C(r.prevScript, tokenHoldingContract, inputs)
	if err != nil {
		return nil, err
	}

	sigscript = append(sigscript, &txbuilder.SigScriptComponent{
		Type:   "script",
		Script: script,
	})

	result := &txbuilder.ReserveResult{
		Items: []*txbuilder.ReserveResultItem{
			{
				TxInput: &bc.TxInput{
					Previous:    r.outpoint,
					AssetAmount: *assetAmount,
					PrevScript:  r.prevScript,
				},
				TemplateInput: &txbuilder.Input{
					AssetAmount:   *assetAmount,
					SigComponents: sigscript,
				},
			},
		},
	}
	return result, nil
}
