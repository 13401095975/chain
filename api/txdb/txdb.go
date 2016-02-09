package txdb

import (
	"bytes"
	"database/sql"
	"sort"
	"time"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/state"
	"chain/metrics"
	"chain/net/trace/span"
	"chain/strings"
)

// PoolTxs returns the pooled transactions
// in topological order.
// If max is negative, there is no limit.
// TODO(jeffomatic) - at some point in the future, will we want to keep this
// cached in an in-memory pool, a la btcd's TxMemPool?
func PoolTxs(ctx context.Context) ([]*bc.Tx, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	const q = `SELECT tx_hash, data FROM pool_txs ORDER BY sort_id`
	rows, err := pg.FromContext(ctx).Query(ctx, q)
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var txs []*bc.Tx
	for rows.Next() {
		var hash bc.Hash
		var data bc.TxData
		err := rows.Scan(&hash, &data)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		txs = append(txs, &bc.Tx{TxData: data, Hash: hash, Stored: true})
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	txs = topSort(ctx, txs)
	return txs, nil
}

// GetTxs looks up transactions by their hashes
// in the block chain and in the pool.
func GetTxs(ctx context.Context, hashes ...string) (map[string]*bc.Tx, error) {
	sort.Strings(hashes)
	hashes = strings.Uniq(hashes)
	const q = `SELECT tx_hash, data FROM txs WHERE tx_hash=ANY($1)`
	rows, err := pg.FromContext(ctx).Query(ctx, q, pg.Strings(hashes))
	if err != nil {
		return nil, errors.Wrap(err, "get txs query")
	}
	defer rows.Close()

	txs := make(map[string]*bc.Tx, len(hashes))
	for rows.Next() {
		var hash bc.Hash
		var data bc.TxData
		err = rows.Scan(&hash, &data)
		if err != nil {
			return nil, errors.Wrap(err, "rows scan")
		}
		txs[hash.String()] = &bc.Tx{TxData: data, Hash: hash, Stored: true}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows end")
	}
	if len(txs) < len(hashes) {
		return nil, errors.Wrap(pg.ErrUserInputNotFound, "missing tx")
	}
	return txs, nil
}

func GetTxBlockHeader(ctx context.Context, hash string) (*bc.BlockHeader, error) {
	const q = `
		SELECT header
		FROM blocks b
		JOIN blocks_txs bt ON b.block_hash = bt.block_hash
		WHERE bt.tx_hash=$1
	`
	b := new(bc.BlockHeader)
	err := pg.FromContext(ctx).QueryRow(ctx, q, hash).Scan(b)
	if err == sql.ErrNoRows {
		return nil, nil // tx "not being in a block" is not an error
	}
	return b, errors.Wrap(err, "select query")
}

// InsertTx inserts tx into txs.
func InsertTx(ctx context.Context, tx *bc.Tx) error {
	const q = `INSERT INTO txs (tx_hash, data) VALUES($1, $2)`
	_, err := pg.FromContext(ctx).Exec(ctx, q, tx.Hash, tx)
	return errors.Wrap(err, "insert query")
}

// LatestBlock returns the most recent block.
func LatestBlock(ctx context.Context) (*bc.Block, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)
	const q = `SELECT data FROM blocks ORDER BY height DESC LIMIT 1`
	b := new(bc.Block)
	err := pg.FromContext(ctx).QueryRow(ctx, q).Scan(b)
	if err == sql.ErrNoRows {
		return nil, errors.Wrap(err, "blocks table is empty; please seed with genesis block")
	}
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	return b, nil
}

func InsertBlock(ctx context.Context, block *bc.Block) ([]bc.Hash, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	const q = `
		INSERT INTO blocks (block_hash, height, data, header)
		VALUES ($1, $2, $3, $4)
	`
	_, err := pg.FromContext(ctx).Exec(ctx, q, block.Hash(), block.Height, block, &block.BlockHeader)
	if err != nil {
		return nil, errors.Wrap(err, "insert query")
	}

	newHashes, err := insertBlockTxs(ctx, block)
	return newHashes, errors.Wrap(err, "inserting txs")
}

func insertBlockTxs(ctx context.Context, block *bc.Block) ([]bc.Hash, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var (
		hashInBlock []string // all txs in block
		hashHist    []string // historical txs not already stored
		data        [][]byte // parallel with hashHist
	)
	for _, tx := range block.Transactions {
		hashInBlock = append(hashInBlock, tx.Hash.String())
		if !tx.Stored {
			var buf bytes.Buffer
			_, err := tx.WriteTo(&buf)
			if err != nil {
				return nil, errors.Wrap(err, "serializing tx")
			}
			data = append(data, buf.Bytes())
			hashHist = append(hashHist, tx.Hash.String())
		}
	}

	const txQ = `
		WITH t AS (SELECT unnest($1::text[]) tx_hash, unnest($2::bytea[]) dat)
		INSERT INTO txs (tx_hash, data)
		SELECT tx_hash, dat FROM t
		WHERE t.tx_hash NOT IN (SELECT tx_hash FROM txs)
		RETURNING tx_hash;
	`
	var newHashes []bc.Hash
	rows, err := pg.FromContext(ctx).Query(ctx, txQ, pg.Strings(hashHist), pg.Byteas(data))
	if err != nil {
		return nil, errors.Wrap(err, "insert txs")
	}
	for rows.Next() {
		var hash bc.Hash
		err := rows.Scan(&hash)
		if err != nil {
			return nil, errors.Wrap(err, "rows scan")
		}
		newHashes = append(newHashes, hash)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows err check")
	}

	const blockTxQ = `
		INSERT INTO blocks_txs (tx_hash, block_hash)
		SELECT unnest($1::text[]), $2;
	`
	_, err = pg.FromContext(ctx).Exec(ctx, blockTxQ, pg.Strings(hashInBlock), block.Hash())
	return nil, errors.Wrap(err, "insert block txs")
}

// ListBlocks returns a list of the most recent blocks,
// potentially offset by a previous query's results.
func ListBlocks(ctx context.Context, prev string, limit int) ([]*bc.Block, error) {
	const q = `
		SELECT data FROM blocks WHERE ($1='' OR height<$1::bigint)
		ORDER BY height DESC LIMIT $2
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, prev, limit)
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()
	var blocks []*bc.Block
	for rows.Next() {
		var block bc.Block
		err := rows.Scan(&block)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		blocks = append(blocks, &block)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows loop")
	}
	return blocks, nil
}

// GetBlock fetches a block by its hash
func GetBlock(ctx context.Context, hash string) (*bc.Block, error) {
	const q = `SELECT data FROM blocks WHERE block_hash=$1`
	block := new(bc.Block)
	err := pg.FromContext(ctx).QueryRow(ctx, q, hash).Scan(block)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	return block, errors.WithDetailf(err, "block hash=%v", hash)
}

func RemoveBlockSpentOutputs(ctx context.Context, delta []*state.Output) error {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var (
		txHashes []string
		ids      []uint32
	)
	for _, out := range delta {
		if !out.Spent {
			continue
		}
		txHashes = append(txHashes, out.Outpoint.Hash.String())
		ids = append(ids, out.Outpoint.Index)
	}

	const q = `
		DELETE FROM utxos
		WHERE (tx_hash, index) IN (SELECT unnest($1::text[]), unnest($2::integer[]))
	`
	_, err := pg.FromContext(ctx).Exec(ctx, q, pg.Strings(txHashes), pg.Uint32s(ids))
	if err != nil {
		return errors.Wrap(err, "delete query")
	}

	return nil
}

// InsertBlockOutputs updates utxos to mark
// unconfirmed records as confirmed and to insert new
// records as necessary, one for each unspent item
// in delta.
//
// It returns a new list containing all spent items
// from delta, plus all newly-inserted unspent outputs
// from delta, omitting the updated items.
func InsertBlockOutputs(ctx context.Context, delta []*state.Output) error {
	defer metrics.RecordElapsed(time.Now())
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	db := pg.FromContext(ctx)

	var outs utxoSet
	for _, out := range delta {
		if out.Spent {
			continue
		}
		addToUTXOSet(&outs, &Output{Output: *out})
	}

	// Insert the ones not upgraded above.
	const insertQ1 = `
		WITH new_utxos AS (
			SELECT
				unnest($1::text[]) AS tx_hash,
				unnest($2::bigint[]) AS index,
				unnest($3::text[]),
				unnest($4::bigint[]),
				unnest($5::bytea[]),
				unnest($6::bytea[]),
				unnest($7::bytea[])
		)
		INSERT INTO utxos (
			tx_hash, index, asset_id, amount,
			script, contract_hash, metadata
		)
		SELECT * FROM new_utxos n WHERE NOT EXISTS
			(SELECT 1 FROM utxos u WHERE (n.tx_hash, n.index) = (u.tx_hash, u.index))
	`

	_, err := db.Exec(ctx, insertQ1,
		outs.txHash,
		outs.index,
		outs.assetID,
		outs.amount,
		outs.script,
		outs.contractHash,
		outs.metadata,
	)
	if err != nil {
		return errors.Wrap(err, "insert into utxos")
	}

	const insertQ2 = `
		INSERT INTO blocks_utxos (tx_hash, index)
		    SELECT unnest($1::text[]), unnest($2::bigint[])
	`
	_, err = db.Exec(ctx, insertQ2, outs.txHash, outs.index)
	return errors.Wrap(err, "insert into blocks_utxos")
}

// CountBlockTxs returns the total number of confirmed transactions.
// TODO: Instead running a count query, we should increment a value each time a
// new block lands.
func CountBlockTxs(ctx context.Context) (uint64, error) {
	const q = `SELECT count(tx_hash) FROM blocks_txs`
	var res uint64
	err := pg.FromContext(ctx).QueryRow(ctx, q).Scan(&res)
	return res, errors.Wrap(err)
}
