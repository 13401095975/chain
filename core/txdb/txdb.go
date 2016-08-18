package txdb

import (
	"bytes"
	"context"
	"sort"
	"strconv"

	"chain/cos/bc"
	"chain/database/pg"
	"chain/database/sql"
	"chain/errors"
	"chain/log"
	"chain/net/trace/span"
	"chain/strings"
)

// New creates a Store and Pool backed by the txdb with the provided
// db handle.
func New(db *sql.DB) (*Store, *Pool) {
	return NewStore(db), NewPool(db)
}

// getBlockchainTxs looks up transactions by their hashes in the blockchain.
func getBlockchainTxs(ctx context.Context, db pg.DB, hashes ...bc.Hash) (bcTxs map[bc.Hash]*bc.Tx, err error) {
	hashStrings := make([]string, 0, len(hashes))
	for _, h := range hashes {
		hashStrings = append(hashStrings, h.String())
	}
	sort.Strings(hashStrings)
	hashStrings = strings.Uniq(hashStrings)
	const q = `
		SELECT t.tx_hash, t.data
		FROM txs t
		INNER JOIN blocks_txs b ON b.tx_hash = t.tx_hash
		WHERE t.tx_hash = ANY($1)
	`
	bcTxs = make(map[bc.Hash]*bc.Tx, len(hashes))
	err = pg.ForQueryRows(pg.NewContext(ctx, db), q, pg.Strings(hashStrings), func(hash bc.Hash, data bc.TxData) {
		tx := &bc.Tx{TxData: data, Hash: hash}
		bcTxs[hash] = tx
	})
	return bcTxs, errors.Wrap(err, "get txs query")
}

// getPoolTxs looks up transactions by their hashes in the pending tx pool.
func getPoolTxs(ctx context.Context, db pg.DB, hashes ...bc.Hash) (poolTxs map[bc.Hash]*bc.Tx, err error) {
	hashStrings := make([]string, 0, len(hashes))
	for _, h := range hashes {
		hashStrings = append(hashStrings, h.String())
	}
	sort.Strings(hashStrings)
	hashStrings = strings.Uniq(hashStrings)
	const q = `
		SELECT t.tx_hash, t.data
		FROM pool_txs t
		WHERE t.tx_hash = ANY($1)
	`
	poolTxs = make(map[bc.Hash]*bc.Tx, len(hashes))
	err = pg.ForQueryRows(pg.NewContext(ctx, db), q, pg.Strings(hashStrings), func(hash bc.Hash, data bc.TxData) {
		tx := &bc.Tx{TxData: data, Hash: hash}
		poolTxs[hash] = tx
	})
	return poolTxs, errors.Wrap(err, "get txs query")
}

// dumpPoolTxs returns all of the pending transactions in the pool.
func dumpPoolTxs(ctx context.Context, db pg.DB) ([]*bc.Tx, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	const q = `SELECT tx_hash, data FROM pool_txs ORDER BY sort_id`
	var txs []*bc.Tx
	err := pg.ForQueryRows(pg.NewContext(ctx, db), q, func(hash bc.Hash, data bc.TxData) {
		txs = append(txs, &bc.Tx{TxData: data, Hash: hash})
	})
	if err != nil {
		return nil, err
	}
	txs = topSort(ctx, txs)
	return txs, nil
}

func (s *Store) GetTxBlockHeader(ctx context.Context, hash bc.Hash) (*bc.BlockHeader, error) {
	const q = `
		SELECT header
		FROM blocks b
		JOIN blocks_txs bt ON b.block_hash = bt.block_hash
		WHERE bt.tx_hash=$1
	`
	b := new(bc.BlockHeader)
	err := s.db.QueryRow(ctx, q, hash).Scan(b)
	if err == sql.ErrNoRows {
		return nil, nil // tx "not being in a block" is not an error
	}
	return b, errors.Wrap(err, "select query")
}

// insertTx inserts tx into txs. It returns true if the insert query inserted the
// transaction. It returns false if the transaction already existed and the query
// had no effect.
func insertTx(ctx context.Context, dbtx *sql.Tx, tx *bc.Tx) (bool, error) {
	const q = `INSERT INTO txs (tx_hash, data) VALUES($1, $2) ON CONFLICT DO NOTHING`
	res, err := dbtx.Exec(ctx, q, tx.Hash, tx)
	if err != nil {
		return false, errors.Wrap(err, "insert query")
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "insert query rows affected")
	}
	return affected > 0, nil
}

func insertBlock(ctx context.Context, dbtx *sql.Tx, block *bc.Block) error {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	const q = `
		INSERT INTO blocks (block_hash, height, data, header)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (block_hash) DO NOTHING
	`
	_, err := dbtx.Exec(ctx, q, block.Hash(), block.Height, block, &block.BlockHeader)
	if err != nil {
		return errors.Wrap(err, "insert query")
	}

	err = insertBlockTxs(ctx, dbtx, block)
	return errors.Wrap(err, "inserting txs")
}

func insertBlockTxs(ctx context.Context, dbtx *sql.Tx, block *bc.Block) error {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var (
		hashes   []string // all txs in block
		blockPos []int32  // position of txs in block
		data     [][]byte // parallel with hashes
	)
	for i, tx := range block.Transactions {
		hashes = append(hashes, tx.Hash.String())
		blockPos = append(blockPos, int32(i))
		var buf bytes.Buffer
		_, err := tx.WriteTo(&buf)
		if err != nil {
			return errors.Wrap(err, "serializing tx")
		}
		data = append(data, buf.Bytes())
	}

	const txQ = `
		WITH t AS (SELECT unnest($1::text[]) tx_hash, unnest($2::bytea[]) dat)
		INSERT INTO txs (tx_hash, data)
		SELECT tx_hash, dat FROM t
		ON CONFLICT DO NOTHING;
	`
	_, err := pg.Exec(pg.NewContext(ctx, dbtx), txQ, pg.Strings(hashes), pg.Byteas(data))
	if err != nil {
		return errors.Wrap(err, "insert txs")
	}

	const blockTxQ = `
		INSERT INTO blocks_txs (tx_hash, block_pos, block_hash, block_height)
		SELECT unnest($1::text[]), unnest($2::int[]), $3, $4;
	`
	_, err = dbtx.Exec(
		ctx,
		blockTxQ,
		pg.Strings(hashes),
		pg.Int32s(blockPos),
		block.Hash(),
		block.Height,
	)
	return errors.Wrap(err, "insert block txs")
}

// ListBlocks returns a list of the most recent blocks,
// potentially offset by a previous query's results.
func (s *Store) ListBlocks(ctx context.Context, prev string, limit int) ([]*bc.Block, error) {
	return listBlocks(ctx, s.db, prev, limit)
}

func listBlocks(ctx context.Context, db pg.DB, prev string, limit int) ([]*bc.Block, error) {
	const q = `
		SELECT data FROM blocks WHERE ($1='' OR height<$1::bigint)
		ORDER BY height DESC LIMIT $2
	`
	var blocks []*bc.Block
	err := pg.ForQueryRows(pg.NewContext(ctx, db), q, prev, limit, func(b bc.Block) {
		blocks = append(blocks, &b)
	})
	return blocks, err
}

// GetBlockByHash fetches a block by its hash.
func (s *Store) GetBlockByHash(ctx context.Context, hash string) (*bc.Block, error) {
	return getBlockByHash(ctx, s.db, hash)
}

func getBlockByHash(ctx context.Context, db pg.DB, hash string) (*bc.Block, error) {
	const q = `SELECT data FROM blocks WHERE block_hash=$1`
	block := new(bc.Block)
	err := db.QueryRow(ctx, q, hash).Scan(block)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	return block, errors.WithDetailf(err, "block hash=%v", hash)
}

// CountBlockTxs returns the total number of confirmed transactions.
// TODO: Instead running a count query, we should increment a value each time a
// new block lands.
func (s *Store) CountBlockTxs(ctx context.Context) (uint64, error) {
	const q = `SELECT count(tx_hash) FROM blocks_txs`
	var res uint64
	err := s.db.QueryRow(ctx, q).Scan(&res)
	return res, errors.Wrap(err)
}

func ListenBlocks(ctx context.Context, dbURL string) (<-chan uint64, error) {
	listener, err := pg.NewListener(ctx, dbURL, "newblock")
	if err != nil {
		return nil, err
	}

	c := make(chan uint64)
	go func() {
		defer func() {
			listener.Close()
			close(c)
		}()

		for {
			select {
			case <-ctx.Done():
				return

			case n := <-listener.Notify:
				height, err := strconv.ParseUint(n.Extra, 10, 64)
				if err != nil {
					log.Error(ctx, errors.Wrap(err, "parsing db notification payload"))
					return
				}
				c <- height
			}
		}
	}()

	return c, nil
}
