package txbuilder

import (
	"time"

	"golang.org/x/net/context"

	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/hdkey"
	"chain/fedchain/txscript"
)

// Build builds or adds on to a transaction.
// Initially, inputs are left unconsumed, and destinations unsatisfied.
// Build partners then satisfy and consume inputs and destinations.
// The final party must ensure that the transaction is
// balanced before calling finalize.
func Build(ctx context.Context, prev *Template, sources []*Source, dests []*Destination, metadata []byte, ttl time.Duration) (*Template, error) {
	if ttl < time.Minute {
		ttl = time.Minute
	}
	tpl, err := build(ctx, sources, dests, metadata, ttl)
	if err != nil {
		return nil, err
	}
	if prev != nil {
		tpl, err = combine(prev, tpl)
		if err != nil {
			return nil, err
		}
	}

	err = setSignatureData(ctx, tpl)
	if err != nil {
		return nil, err
	}

	return tpl, nil
}

func build(ctx context.Context, sources []*Source, dests []*Destination, metadata []byte, ttl time.Duration) (*Template, error) {
	tx := &bc.TxData{
		Version:  bc.CurrentTransactionVersion,
		Metadata: metadata,
	}

	var inputs []*Input

	for _, source := range sources {
		reserveResult, err := source.Reserve(ctx, ttl)
		if err != nil {
			return nil, errors.Wrap(err, "reserve")
		}
		for _, item := range reserveResult.Items {
			tx.Inputs = append(tx.Inputs, item.TxInput)
			inputs = append(inputs, item.TemplateInput)
		}
		if reserveResult.Change != nil {
			dests = append(dests, reserveResult.Change)
		}
	}

	for _, dest := range dests {
		output := &bc.TxOutput{
			AssetAmount: bc.AssetAmount{AssetID: dest.AssetID, Amount: dest.Amount},
			Script:      dest.PKScript(),
			Metadata:    dest.Metadata,
		}
		tx.Outputs = append(tx.Outputs, output)
	}

	appTx := &Template{
		Unsigned:   tx,
		BlockChain: "sandbox",
		Inputs:     inputs,
	}

	return appTx, nil
}

func combine(txs ...*Template) (*Template, error) {
	if len(txs) == 0 {
		return nil, errors.New("must pass at least one tx")
	}
	completeWire := &bc.TxData{Version: bc.CurrentTransactionVersion}
	complete := &Template{BlockChain: txs[0].BlockChain, Unsigned: completeWire}

	for _, tx := range txs {
		if tx.BlockChain != complete.BlockChain {
			return nil, errors.New("all txs must be the same BlockChain")
		}

		complete.Inputs = append(complete.Inputs, tx.Inputs...)

		for _, txin := range tx.Unsigned.Inputs {
			completeWire.Inputs = append(completeWire.Inputs, txin)
		}
		for _, txout := range tx.Unsigned.Outputs {
			completeWire.Outputs = append(completeWire.Outputs, txout)
		}
	}

	return complete, nil
}

func setSignatureData(ctx context.Context, tpl *Template) error {
	hashCache := &bc.SigHashCache{}
	for i, in := range tpl.Inputs {
		aa := in.AssetAmount
		in.SignatureData = tpl.Unsigned.HashForSigCached(i, aa, bc.SigHashAll, hashCache)
	}
	return nil
}

// AssembleSignatures takes a filled in Template
// and adds the signatures to the template's unsigned transaction,
// creating a fully-signed transaction.
func AssembleSignatures(txTemplate *Template) (*bc.Tx, error) {
	msg := txTemplate.Unsigned
	for i, input := range txTemplate.Inputs {
		sigsAdded := 0
		sigsReqd, err := getSigsRequired(input.SigScriptSuffix)
		if err != nil {
			return nil, err
		}
		builder := txscript.NewScriptBuilder()
		if len(input.Sigs) > 0 {
			builder.AddOp(txscript.OP_FALSE)
		}
		for _, sig := range input.Sigs {
			if len(sig.DER) > 0 {
				builder.AddData(sig.DER)
				sigsAdded++
				if sigsAdded == sigsReqd {
					break
				}
			}
		}
		script, err := builder.Script()
		if err != nil {
			return nil, errors.Wrap(err)
		}
		msg.Inputs[i].SignatureScript = append(script, input.SigScriptSuffix...)
	}
	return bc.NewTx(*msg), nil
}

// InputSigs takes a set of keys
// and creates a matching set of Input Signatures
// for a Template
func InputSigs(keys []*hdkey.Key) (sigs []*Signature) {
	for _, k := range keys {
		sigs = append(sigs, &Signature{
			XPub:           k.Root.String(),
			DerivationPath: k.Path,
		})
	}
	return sigs
}

func getSigsRequired(script []byte) (sigsReqd int, err error) {
	sigsReqd = 1
	if txscript.GetScriptClass(script) == txscript.MultiSigTy {
		_, sigsReqd, err = txscript.CalcMultiSigStats(script)
		if err != nil {
			return 0, err
		}
	}
	return sigsReqd, nil
}
