package auditor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"golang.org/x/net/context"

	"chain/api/txdb"
	"chain/database/pg"
	chainjson "chain/encoding/json"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/txscript"
)

// ListBlocksItem is returned by ListBlocks
type ListBlocksItem struct {
	ID      bc.Hash   `json:"id"`
	Height  uint64    `json:"height"`
	Time    time.Time `json:"time"`
	TxCount int       `json:"transaction_count"`
}

// ListBlocks returns an array of ListBlocksItems
// as well as a pagination pointer for the last item
// in the list.
func ListBlocks(ctx context.Context, prev string, limit int) ([]ListBlocksItem, string, error) {
	blocks, err := txdb.ListBlocks(ctx, prev, limit)
	if err != nil {
		return nil, "", err
	}

	var (
		list []ListBlocksItem
		last string
	)
	for _, b := range blocks {
		list = append(list, ListBlocksItem{b.Hash(), b.Height, b.Time(), len(b.Transactions)})
	}
	if len(list) == limit && limit > 0 {
		last = fmt.Sprintf("%d", list[len(list)-1].Height)
	}

	return list, last, nil
}

// BlockSummary is returned by GetBlockSummary
type BlockSummary struct {
	ID      bc.Hash   `json:"id"`
	Height  uint64    `json:"height"`
	Time    time.Time `json:"time"`
	TxCount int       `json:"transaction_count"`
	TxIDs   []bc.Hash `json:"transaction_ids"`
}

// GetBlockSummary returns header data for the requested block.
func GetBlockSummary(ctx context.Context, hash string) (*BlockSummary, error) {
	block, err := txdb.GetBlock(ctx, hash)
	if err != nil {
		return nil, err
	}

	txHashes := make([]bc.Hash, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		txHashes = append(txHashes, tx.Hash())
	}

	return &BlockSummary{
		ID:      block.Hash(),
		Height:  block.Height,
		Time:    block.Time(),
		TxCount: len(block.Transactions),
		TxIDs:   txHashes,
	}, nil
}

// Tx is returned by GetTx
type Tx struct {
	ID       bc.Hash            `json:"id"`
	Inputs   []*TxInput         `json:"inputs"`
	Outputs  []*TxOutput        `json:"outputs"`
	Metadata chainjson.HexBytes `json:"metadata,omitempty"`
}

// TxInput is an input in a Tx
type TxInput struct {
	Type     string                 `json:"type"`
	TxID     bc.Hash                `json:"transaction_id,omitempty"`
	TxOut    uint32                 `json:"transaction_output,omitempty"`
	AssetID  bc.AssetID             `json:"asset_id"`
	AssetDef map[string]interface{} `json:"asset_definition,omitempty"`
	Amount   uint64                 `json:"amount"`
	Metadata chainjson.HexBytes     `json:"metadata,omitempty"`
}

// TxOutput is an output in a Tx
type TxOutput struct {
	AssetID  bc.AssetID         `json:"asset_id"`
	Amount   uint64             `json:"amount"`
	Address  string             `json:"address"`
	Metadata chainjson.HexBytes `json:"metadata,omitempty"`
}

// GetTx returns a transaction with additional details added.
func GetTx(ctx context.Context, txID string) (*Tx, error) {
	txs, err := txdb.GetTxs(ctx, txID)
	if err != nil {
		return nil, err
	}
	tx := txs[txID]

	resp := &Tx{
		ID:       tx.Hash(),
		Metadata: tx.Metadata,
	}

	if tx.IsIssuance() {
		if len(tx.Outputs) == 0 {
			return nil, errors.New("invalid transaction")
		}
		var totalOut uint64
		for _, out := range tx.Outputs {
			totalOut += out.Value
		}

		var assetDef map[string]interface{}
		json.Unmarshal(tx.Inputs[0].Metadata, &assetDef) // fine if it doesn't work

		resp.Inputs = append(resp.Inputs, &TxInput{
			Type:     "issuance",
			AssetID:  tx.Outputs[0].AssetID,
			Amount:   totalOut,
			Metadata: tx.Inputs[0].Metadata,
			AssetDef: assetDef,
		})
	} else {
		var inHashes []string
		for _, in := range tx.Inputs {
			inHashes = append(inHashes, in.Previous.Hash.String())
		}
		txs, err = txdb.GetTxs(ctx, inHashes...)
		if err != nil {
			if errors.Root(err) == pg.ErrUserInputNotFound {
				err = sql.ErrNoRows
			}
			return nil, errors.Wrap(err, "fetching inputs")
		}
		for _, in := range tx.Inputs {
			prev := txs[in.Previous.Hash.String()].Outputs[in.Previous.Index]
			resp.Inputs = append(resp.Inputs, &TxInput{
				Type:     "transfer",
				AssetID:  prev.AssetID,
				Amount:   prev.Value,
				TxID:     in.Previous.Hash,
				TxOut:    in.Previous.Index,
				Metadata: in.Metadata,
			})
		}
	}

	for _, out := range tx.Outputs {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(out.Script, &chaincfg.MainNetParams)
		if err != nil {
			return nil, err
		}
		var addrStrs []string
		for _, addr := range addrs {
			addrStrs = append(addrStrs, addr.String())
		}
		resp.Outputs = append(resp.Outputs, &TxOutput{
			AssetID:  out.AssetID,
			Amount:   out.Value,
			Address:  strings.Join(addrStrs, ","),
			Metadata: out.Metadata,
		})
	}

	return resp, nil
}
