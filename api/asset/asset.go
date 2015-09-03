// Package asset provides business logic for manipulating assets.
package asset

import (
	"bytes"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/errors"
	"chain/fedchain/wire"
)

func Issue(ctx context.Context, assetID string, outs []Output) (*Tx, error) {
	tx := wire.NewMsgTx()
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(new(wire.Hash32), 0), []byte{}))

	asset, err := appdb.AssetByID(ctx, assetID)
	if err != nil {
		return nil, errors.Wrap(err, "get asset by ID")
	}

	err = addAssetIssuanceOutputs(tx, asset, outs)
	if err != nil {
		return nil, errors.Wrap(err, "add issuance outputs")
	}

	var buf bytes.Buffer
	tx.Serialize(&buf)
	appTx := &Tx{
		Unsigned:   buf.Bytes(),
		BlockChain: "sandbox", // TODO(tess): make this BlockChain: blockchain.FromContext(ctx)
		Inputs:     []*Input{issuanceInput(asset)},
	}
	return appTx, nil
}

type Output struct {
	Address  string
	BucketID string
	Amount   int64
}

func addAssetIssuanceOutputs(tx *wire.MsgTx, asset *appdb.Asset, outs []Output) error {
	for _, out := range outs {
		if out.BucketID != "" {
			// TODO(erykwalder): actually generate an address
			// This address doesn't mean anything, it was grabbed from the internet.
			// We don't have its private key.
			out.Address = "1ByEd6DMfTERyT4JsVSLDoUcLpJTD93ifq"
		}

		addr, err := btcutil.DecodeAddress(out.Address, &chaincfg.MainNetParams)
		if err != nil {
			return err
		}
		pkScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			return err
		}

		tx.AddTxOut(wire.NewTxOut(asset.Hash, out.Amount, pkScript))
	}
	return nil
}

// issuanceInput returns an Input that can be used
// to issue units of asset 'a'.
func issuanceInput(a *appdb.Asset) *Input {
	return &Input{
		WalletID:     a.WalletID,
		RedeemScript: a.RedeemScript,
		Sigs:         issuanceSigs(a),
	}
}

func issuanceSigs(a *appdb.Asset) (sigs []*Signature) {
	for _, key := range a.Keys {
		signer := &Signature{
			XPubHash:       key.ID,
			DerivationPath: assetIssuanceDerivationPath(key, a),
		}
		sigs = append(sigs, signer)
	}
	return sigs
}

func assetIssuanceDerivationPath(key *appdb.Key, asset *appdb.Asset) []uint32 {
	return append([]uint32{appdb.CustomerAssetsNamespace}, asset.AIndex...)
}
