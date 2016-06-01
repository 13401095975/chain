package txdb

import (
	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/cos/state"
	"chain/database/pg"
	"chain/database/sql"
	"chain/errors"
	"chain/net/trace/span"
)

// loadPoolOutputs returns the outputs in 'load' that can be found.
// If some are not found, they will be absent from the map
// (not an error).
func loadPoolOutputs(ctx context.Context, dbtx *sql.Tx, load []bc.Outpoint) (map[bc.Outpoint]*state.Output, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var (
		txHashes []string
		indexes  []uint32
	)
	for _, p := range load {
		txHashes = append(txHashes, p.Hash.String())
		indexes = append(indexes, p.Index)
	}

	const loadQ = `
		SELECT tx_hash, index, asset_id, amount, script, metadata
		  FROM utxos_status
		  WHERE NOT confirmed
		    AND (tx_hash, index) IN (SELECT unnest($1::text[]), unnest($2::integer[]))
	`
	outs := make(map[bc.Outpoint]*state.Output)
	err := pg.ForQueryRows(pg.NewContext(ctx, dbtx), loadQ, pg.Strings(txHashes), pg.Uint32s(indexes), func(hash bc.Hash, index uint32, assetID bc.AssetID, amount uint64, script, metadata []byte) {
		o := &state.Output{
			Outpoint: bc.Outpoint{Hash: hash, Index: index},
			TxOutput: bc.TxOutput{
				AssetAmount: bc.AssetAmount{AssetID: assetID, Amount: amount},
				Script:      script,
				Metadata:    metadata,
			},
		}
		outs[o.Outpoint] = o
	})
	if err != nil {
		return nil, err
	}
	return outs, err
}

// utxoSet holds a set of utxo record values
// to be inserted into the db.
type utxoSet struct {
	txHash   pg.Strings
	index    pg.Uint32s
	assetID  pg.Strings
	amount   pg.Int64s
	script   pg.Byteas
	metadata pg.Byteas
}

func addToUTXOSet(set *utxoSet, out *Output) {
	set.txHash = append(set.txHash, out.Outpoint.Hash.String())
	set.index = append(set.index, out.Outpoint.Index)
	set.assetID = append(set.assetID, out.AssetID.String())
	set.amount = append(set.amount, int64(out.Amount))
	set.script = append(set.script, out.Script)
	set.metadata = append(set.metadata, out.Metadata)
}

func insertPoolTx(ctx context.Context, db pg.DB, tx *bc.Tx) error {
	const q = `INSERT INTO pool_txs (tx_hash, data) VALUES ($1, $2)`
	_, err := db.Exec(ctx, q, tx.Hash, tx)
	return errors.Wrap(err)
}

func insertPoolOutputs(ctx context.Context, db pg.DB, insert []*Output) error {
	var outs utxoSet
	for _, o := range insert {
		addToUTXOSet(&outs, o)
	}

	const q1 = `
		INSERT INTO utxos (
			tx_hash, index, asset_id, amount,
			script, metadata
		)
		SELECT
			unnest($1::text[]),
			unnest($2::bigint[]),
			unnest($3::text[]),
			unnest($4::bigint[]),
			unnest($5::bytea[]),
			unnest($6::bytea[])
	`
	_, err := db.Exec(ctx, q1,
		outs.txHash,
		outs.index,
		outs.assetID,
		outs.amount,
		outs.script,
		outs.metadata,
	)
	return err
}

// CountPoolTxs returns the total number of unconfirmed transactions.
func (s *Store) CountPoolTxs(ctx context.Context) (uint64, error) {
	const q = `SELECT count(tx_hash) FROM pool_txs`
	var res uint64
	err := s.db.QueryRow(ctx, q).Scan(&res)
	return res, errors.Wrap(err)
}
