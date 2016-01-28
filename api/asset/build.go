package asset

import (
	"time"

	"golang.org/x/net/context"

	"chain/api/txdb"
	"chain/api/utxodb"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/state"
)

// All UTXOs in the system.
var utxoDB = utxodb.New(sqlUTXODB{})

// Build builds or adds on to a transaction.
// Initially, inputs are left unconsumed, and destinations unsatisfied.
// Build partners then satisfy and consume inputs and destinations.
// The final party must ensure that the transaction is
// balanced before calling finalize.
func Build(ctx context.Context, prev *TxTemplate, sources []*Source, dests []*Destination, metadata []byte, ttl time.Duration) (*TxTemplate, error) {
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

func build(ctx context.Context, sources []*Source, dests []*Destination, metadata []byte, ttl time.Duration) (*TxTemplate, error) {
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

	receivers := make([]Receiver, 0, len(dests))
	for _, dest := range dests {
		receivers = append(receivers, dest.Receiver)
	}

	appTx := &TxTemplate{
		Unsigned:   tx,
		BlockChain: "sandbox",
		Inputs:     inputs,
		OutRecvs:   receivers,
	}

	return appTx, nil
}

func combine(txs ...*TxTemplate) (*TxTemplate, error) {
	if len(txs) == 0 {
		return nil, errors.New("must pass at least one tx")
	}
	completeWire := &bc.TxData{Version: bc.CurrentTransactionVersion}
	complete := &TxTemplate{BlockChain: txs[0].BlockChain, Unsigned: completeWire}

	for _, tx := range txs {
		if tx.BlockChain != complete.BlockChain {
			return nil, errors.New("all txs must be the same BlockChain")
		}

		complete.Inputs = append(complete.Inputs, tx.Inputs...)
		complete.OutRecvs = append(complete.OutRecvs, tx.OutRecvs...)

		for _, txin := range tx.Unsigned.Inputs {
			completeWire.Inputs = append(completeWire.Inputs, txin)
		}
		for _, txout := range tx.Unsigned.Outputs {
			completeWire.Outputs = append(completeWire.Outputs, txout)
		}
	}

	return complete, nil
}

func setSignatureData(ctx context.Context, tpl *TxTemplate) error {
	txSet := []*bc.Tx{bc.NewTx(*tpl.Unsigned)}
	bcView, err := txdb.NewViewForPrevouts(ctx, txSet)
	if err != nil {
		return errors.Wrap(err, "loading utxos")
	}
	poolView, err := txdb.NewPoolViewForPrevouts(ctx, txSet)
	if err != nil {
		return errors.Wrap(err, "loading utxos")
	}
	view := state.MultiReader(poolView, bcView)

	hashCache := &bc.SigHashCache{}

	for i, in := range tpl.Unsigned.Inputs {
		var assetAmount bc.AssetAmount
		if !in.IsIssuance() {
			unspent := view.Output(ctx, in.Previous)
			if unspent == nil {
				return errors.New("could not load previous output")
			}
			assetAmount = unspent.AssetAmount
		}
		tpl.Inputs[i].SignatureData = tpl.Unsigned.HashForSigCached(i, assetAmount, bc.SigHashAll, hashCache)
	}
	return nil
}

// CancelReservations cancels any existing reservations
// for the given outpoints.
func CancelReservations(ctx context.Context, outpoints []bc.Outpoint) {
	utxoDB.Cancel(ctx, outpoints)
}
