package asset

import (
	"runtime"
	"time"

	"golang.org/x/net/context"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"

	"chain/api/appdb"
	"chain/api/txdb"
	"chain/api/utxodb"
	"chain/database/pg"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/fedchain/state"
	"chain/fedchain/txscript"
	"chain/fedchain/validation"
	"chain/log"
	"chain/net/trace/span"
)

// MaxBlockTxs limits the number of transactions
// included in each block.
const MaxBlockTxs = 10000

// ErrBadBlock is returned when a block is invalid.
var ErrBadBlock = errors.New("invalid block")

// BlockKey is the private key used to sign blocks.
var BlockKey *btcec.PrivateKey

// MakeBlocks runs forever,
// attempting to make one block per period.
// The caller should call it exactly once.
func MakeBlocks(ctx context.Context, period time.Duration) {
	for range time.Tick(period) {
		makeBlock(ctx)
	}
}

func makeBlock(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			log.Write(ctx,
				log.KeyMessage, "panic",
				log.KeyError, err,
				log.KeyStack, buf,
			)
		}
	}()
	MakeBlock(ctx, BlockKey)
}

// MakeBlock creates a new bc.Block and updates the txpool/utxo state.
func MakeBlock(ctx context.Context, key *btcec.PrivateKey) (*bc.Block, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	b, err := GenerateBlock(ctx, time.Now())
	if err != nil {
		log.Error(ctx, errors.Wrap(err, "generate"))
		return nil, err
	}
	if len(b.Transactions) == 0 {
		return nil, nil // don't bother making an empty block
	}
	err = SignBlock(b, key)
	if err != nil {
		log.Error(ctx, errors.Wrap(err, "sign"))
		return nil, err
	}
	err = ApplyBlock(ctx, b)
	if err != nil {
		log.Error(ctx, errors.Wrap(err, "apply"))
		return nil, err
	}
	log.Messagef(ctx, "made block %s height %d with %d txs", b.Hash(), b.Height, len(b.Transactions))
	return b, nil
}

func SignBlock(b *bc.Block, key *btcec.PrivateKey) error {
	// assumes multisig output script
	hash := b.HashForSig()

	dat, err := key.Sign(hash[:])
	if err != nil {
		return err
	}
	sig := append(dat.Serialize(), 1) // append hashtype -- unused for blocks

	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_0) // required because of bug in OP_CHECKMULTISIG
	builder.AddData(sig)
	script, err := builder.Script()
	if err != nil {
		return err
	}

	b.SignatureScript = script

	return nil
}

// GenerateBlock creates a new bc.Block using the current tx pool and blockchain
// state.
// TODO - receive parameters for script config.
func GenerateBlock(ctx context.Context, now time.Time) (*bc.Block, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	ts := uint64(now.Unix())

	prevBlock, err := txdb.LatestBlock(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "fetch latest block")
	}

	if ts < prevBlock.Timestamp {
		return nil, errors.New("timestamp is earlier than prevblock timestamp")
	}

	txs, err := txdb.PoolTxs(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "get pool TXs")
	}
	if len(txs) > MaxBlockTxs {
		txs = txs[:MaxBlockTxs]
	}

	block := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Version:           bc.NewBlockVersion,
			Height:            prevBlock.Height + 1,
			PreviousBlockHash: prevBlock.Hash(),

			// TODO: Calculate merkle hashes of txs and blockchain state.
			//TxRoot:
			//StateRoot:

			// It's possible to generate a block whose timestamp is prior to the
			// previous block, but we won't validate that here.
			Timestamp: ts,

			// TODO: Generate SignatureScript
			OutputScript: prevBlock.OutputScript,
		},
	}

	poolView := NewMemView()
	bcView, err := txdb.NewViewForPrevouts(ctx, txs)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	view := state.Compose(poolView, bcView)
	ctx = span.NewContextSuffix(ctx, "-validate-all")
	defer span.Finish(ctx)
	for _, tx := range txs {
		if validation.ValidateTxInputs(ctx, view, tx) == nil {
			validation.ApplyTx(ctx, view, tx)
			block.Transactions = append(block.Transactions, tx)
		}
	}
	return block, nil
}

func outpoints(outs []*txdb.Output) (p []bc.Outpoint) {
	for _, o := range outs {
		p = append(p, o.Outpoint)
	}
	return p
}

func ApplyBlock(ctx context.Context, block *bc.Block) error {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	delta, err := applyBlock(ctx, block)
	if err != nil {
		return errors.Wrap(err)
	}

	// When applying block outputs to the reserver,
	// do not apply an output that has already been
	// spent in the mempool.
	poolView, err := txdb.NewPoolView(ctx, outpoints(delta))
	if err != nil {
		return errors.Wrap(err)
	}
	var resvDelta []*txdb.Output
	for _, o := range delta {
		po := poolView.Output(ctx, o.Outpoint)
		if o.Spent || po == nil {
			resvDelta = append(resvDelta, o)
		}
	}
	applyToReserver(ctx, resvDelta)

	conflictTxs, err := rebuildPool(ctx, block)
	if err != nil {
		return errors.Wrap(err)
	}

	conflictOuts, err := getRestoreableOutputs(ctx, conflictTxs)
	if err != nil {
		return errors.Wrap(err)
	}

	applyToReserver(ctx, conflictOuts)
	return nil
}

// applyBlock returns a delta for the reserver:
//   - deleted outputs
//   - inserted outputs (not previously part of the pool)
func applyBlock(ctx context.Context, block *bc.Block) ([]*txdb.Output, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	delta, adps, err := validateBlock(ctx, block)
	if err != nil {
		return nil, errors.Wrap(err, "block validation")
	}

	err = txdb.InsertBlock(ctx, block)
	if err != nil {
		return nil, errors.Wrap(err, "insert block")
	}

	err = txdb.InsertAssetDefinitionPointers(ctx, adps)
	if err != nil {
		return nil, errors.Wrap(err, "insert ADPs")
	}

	err = txdb.InsertAssetDefinitions(ctx, block)
	if err != nil {
		return nil, errors.Wrap(err, "writing asset definitions")
	}

	err = loadAccountInfo(ctx, delta)
	if err != nil {
		return nil, errors.Wrap(err, "block outputs")
	}

	err = txdb.RemoveBlockSpentOutputs(ctx, delta)
	if err != nil {
		return nil, errors.Wrap(err, "remove block spent outputs")
	}

	delta, err = txdb.InsertBlockOutputs(ctx, block, delta)
	if err != nil {
		return nil, errors.Wrap(err, "insert block outputs")
	}

	err = appdb.UpdateIssuances(ctx, issuedAssets(block.Transactions), true)
	if err != nil {
		return nil, errors.Wrap(err, "update issuances")
	}

	return delta, nil
}

func rebuildPool(ctx context.Context, block *bc.Block) ([]*bc.Tx, error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "pool update dbtx begin")
	}
	defer dbtx.Rollback(ctx)

	txInBlock := make(map[bc.Hash]bool)
	for _, tx := range block.Transactions {
		txInBlock[tx.Hash] = true
	}

	var (
		conflictTxs          []*bc.Tx
		deleteTxs            []*bc.Tx
		deleteTxHashes       []string
		deleteInputTxHashes  []string
		deleteInputTxIndexes []uint32
	)

	txs, err := txdb.PoolTxs(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	poolView := NewMemView()
	bcView, err := txdb.NewViewForPrevouts(ctx, txs)
	if err != nil {
		return nil, errors.Wrap(err, "blockchain view")
	}
	view := state.Compose(poolView, bcView)
	for _, tx := range txs {
		txErr := validation.ValidateTxInputs(ctx, view, tx)
		// Have to explicitly check that tx is not in block
		// because issuance transactions are always valid, even duplicates.
		// TODO(erykwalder): Remove this check when issuances become unique
		if txErr == nil && !txInBlock[tx.Hash] {
			validation.ApplyTx(ctx, view, tx)
		} else {
			deleteTxs = append(deleteTxs, tx)
			deleteTxHashes = append(deleteTxHashes, tx.Hash.String())
			for _, in := range tx.Inputs {
				if in.IsIssuance() {
					continue
				}
				deleteInputTxHashes = append(deleteInputTxHashes, in.Previous.Hash.String())
				deleteInputTxIndexes = append(deleteInputTxIndexes, in.Previous.Index)
			}

			if !txInBlock[tx.Hash] {
				conflictTxs = append(conflictTxs, tx)
				// This should never happen in sandbox, unless a reservation expired
				// before the original tx was finalized.
				log.Messagef(ctx, "deleting conflict tx %v because %q", tx.Hash, txErr)
				for i, in := range tx.Inputs {
					out := view.Output(ctx, in.Previous)
					if out == nil {
						log.Messagef(ctx, "conflict tx %v missing input %d (%v)", tx.Hash, in.Previous)
						continue
					}
					if out.Spent {
						log.Messagef(ctx, "conflict tx %v spent input %d (%v) inblock=%v inpool=%v",
							tx.Hash, i, in.Previous, bcView.Output(ctx, in.Previous), poolView.Output(ctx, in.Previous))
					}
				}
			}
		}
	}

	db := pg.FromContext(ctx)

	// Delete pool_txs
	const txq = `DELETE FROM pool_txs WHERE tx_hash IN (SELECT unnest($1::text[]))`
	_, err = db.Exec(ctx, txq, pg.Strings(deleteTxHashes))
	if err != nil {
		return nil, errors.Wrap(err, "delete from pool_txs")
	}

	// Delete pool outputs
	const outq = `DELETE FROM utxos WHERE NOT confirmed AND tx_hash IN (SELECT unnest($1::text[]))`
	_, err = db.Exec(ctx, outq, pg.Strings(deleteTxHashes))
	if err != nil {
		return nil, errors.Wrap(err, "delete from utxos")
	}

	// Delete pool_inputs
	const inq = `
		DELETE FROM pool_inputs
		WHERE (tx_hash, index) IN (
			SELECT unnest($1::text[]), unnest($2::integer[])
		)
	`
	_, err = db.Exec(ctx, inq, pg.Strings(deleteInputTxHashes), pg.Uint32s(deleteInputTxIndexes))
	if err != nil {
		return nil, errors.Wrap(err, "delete from pool_inputs")
	}

	// Update issuance totals
	deltas := issuedAssets(deleteTxs)
	for aid, v := range deltas {
		deltas[aid] = -v // reverse polarity, we want decrements
	}
	err = appdb.UpdateIssuances(ctx, deltas, false)
	if err != nil {
		return nil, errors.Wrap(err, "undo pool issuances")
	}

	err = dbtx.Commit(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "pool update dbtx commit")
	}
	return conflictTxs, nil
}

func getRestoreableOutputs(ctx context.Context, txs []*bc.Tx) (outs []*txdb.Output, err error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	poolView, err := txdb.NewPoolViewForPrevouts(ctx, txs)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	bcView, err := txdb.NewViewForPrevouts(ctx, txs)
	if err != nil {
		return nil, errors.Wrap(err, "load prev outs from conflicting txs")
	}

	// undo conflicting txs in reserver
	view := state.MultiReader(poolView, bcView)
	for _, tx := range txs {
		for _, in := range tx.Inputs {
			if in.IsIssuance() {
				continue
			}
			o := view.Output(ctx, in.Previous)
			if o == nil || o.Spent {
				continue
			}
			outs = append(outs, &txdb.Output{Output: *o})
		}

		for i, out := range tx.Outputs {
			op := bc.Outpoint{Hash: tx.Hash, Index: uint32(i)}
			outs = append(outs, &txdb.Output{
				Output: state.Output{
					TxOutput: *out,
					Outpoint: op,
					Spent:    true,
				},
			})
		}
	}

	err = loadAccountInfo(ctx, outs)
	if err != nil {
		return nil, errors.Wrap(err, "load conflict out account info")
	}

	return outs, nil
}

func applyToReserver(ctx context.Context, outs []*txdb.Output) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var del, ins []*utxodb.UTXO
	for _, out := range outs {
		u := &utxodb.UTXO{
			AccountID: out.AccountID,
			AssetID:   out.AssetID,
			Amount:    out.Amount,
			Outpoint:  out.Outpoint,
			AddrIndex: out.AddrIndex,
		}
		if out.Spent {
			del = append(del, u)
		} else {
			ins = append(ins, u)
		}
	}
	utxoDB.Apply(del, ins)
}

// loadAccountInfo returns annotated UTXO data (outputs + account mappings) for
// addresses known to this manager node. It is only concerned with outputs that
// actually have account mappings, which come from either the utxos or
// addresses tables.
func loadAccountInfo(ctx context.Context, outs []*txdb.Output) error {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	var (
		hashes            []string
		indexes           []uint32
		scripts           [][]byte
		outpointsByScript = make(map[string]bc.Outpoint)
		outputs           = make(map[bc.Outpoint]*txdb.Output)
	)
	for _, out := range outs {
		outputs[out.Outpoint] = out
		hashes = append(hashes, out.Outpoint.Hash.String())
		indexes = append(indexes, out.Outpoint.Index)

		scripts = append(scripts, out.Script)
		outpointsByScript[string(out.Script)] = out.Outpoint
	}

	// addresses table

	const addrq = `
		SELECT pk_script, manager_node_id, account_id, key_index(key_index)
		FROM addresses
		WHERE pk_script IN (SELECT unnest($1::bytea[]))
	`
	rows, err := pg.FromContext(ctx).Query(ctx, addrq, pg.Byteas(scripts))
	if err != nil {
		return errors.Wrap(err, "addresses select query")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			script         []byte
			mnodeID, accID string
			addrIndex      []uint32
		)
		err := rows.Scan(&script, &mnodeID, &accID, (*pg.Uint32s)(&addrIndex))
		if err != nil {
			return errors.Wrap(err, "addresses row scan")
		}
		out := outputs[outpointsByScript[string(script)]]
		out.ManagerNodeID = mnodeID
		out.AccountID = accID
		copy(out.AddrIndex[:], addrIndex)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "addresses end row scan loop")
	}

	// utxos table - both confirmed (blockchain) and unconfirmed (pool)

	const utxoq = `
		SELECT tx_hash, index, manager_node_id, account_id, key_index(addr_index)
		FROM utxos
		WHERE (tx_hash, index) IN (SELECT unnest($1::text[]), unnest($2::integer[]))
	`
	rows, err = pg.FromContext(ctx).Query(ctx, utxoq, pg.Strings(hashes), pg.Uint32s(indexes))
	if err != nil {
		return errors.Wrap(err, "utxos select query")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			op             bc.Outpoint
			mnodeID, accID string
			addrIndex      []uint32
		)
		err := rows.Scan(&op.Hash, &op.Index, &mnodeID, &accID, (*pg.Uint32s)(&addrIndex))
		if err != nil {
			return errors.Wrap(err, "utxos row scan")
		}
		out := outputs[op]
		out.ManagerNodeID = mnodeID
		out.AccountID = accID
		copy(out.AddrIndex[:], addrIndex)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "utxos end row scan loop")
	}

	return nil
}

// validateBlock performs validation on an incoming block, in advance of
// applying the block to the txdb.
func validateBlock(ctx context.Context, block *bc.Block) (outs []*txdb.Output, adps map[bc.AssetID]*bc.AssetDefinitionPointer, err error) {
	ctx = span.NewContext(ctx)
	defer span.Finish(ctx)

	bcView, err := txdb.NewViewForPrevouts(ctx, block.Transactions)
	if err != nil {
		return nil, nil, errors.Wrap(err, "txdb")
	}
	mv := NewMemView()

	prevBlock, err := txdb.LatestBlock(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "loading previous block")
	}

	err = validation.ValidateBlockHeader(ctx, prevBlock, block)
	if err != nil {
		return nil, nil, errors.Wrap(err, "validating block header")
	}

	if isSignedByTrustedHost(block, []*btcec.PublicKey{BlockKey.PubKey()}) {
		validation.ApplyBlock(ctx, state.Compose(mv, bcView), block)
	} else {
		err = validation.ValidateAndApplyBlock(ctx, state.Compose(mv, bcView), prevBlock, block)
		if err != nil {
			return nil, nil, errors.Wrapf(ErrBadBlock, "validate block: %v", err)
		}
	}

	for _, out := range mv.Outs {
		outs = append(outs, out)
	}

	return outs, mv.ADPs, nil
}

func isSignedByTrustedHost(block *bc.Block, trustedKeys []*btcec.PublicKey) bool {
	sigs, err := txscript.PushedData(block.SignatureScript)
	if err != nil {
		return false
	}

	hash := block.HashForSig()
	for _, sig := range sigs {
		if len(sig) == 0 {
			continue
		}
		parsedSig, err := btcec.ParseSignature(sig, btcec.S256())
		if err != nil { // could be arbitrary push data
			continue
		}
		for _, pubk := range trustedKeys {
			if parsedSig.Verify(hash[:], pubk) {
				return true
			}
		}
	}

	return false
}

func issuedAssets(txs []*bc.Tx) map[bc.AssetID]int64 {
	issued := make(map[bc.AssetID]int64)
	for _, tx := range txs {
		if !tx.IsIssuance() {
			continue
		}
		for _, out := range tx.Outputs {
			issued[out.AssetID] += int64(out.Amount)
		}
	}
	return issued
}

// UpsertGenesisBlock creates a genesis block iff it does not exist.
func UpsertGenesisBlock(ctx context.Context) (*bc.Block, error) {
	script, err := generateBlockScript([]*btcec.PublicKey{BlockKey.PubKey()}, 1)
	if err != nil {
		return nil, err
	}

	b := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Version:      bc.NewBlockVersion,
			Timestamp:    uint64(time.Now().Unix()),
			OutputScript: script,
		},
	}

	const q = `
		INSERT INTO blocks (block_hash, height, data, header)
		SELECT $1, $2, $3, $4
		WHERE NOT EXISTS (SELECT 1 FROM blocks WHERE height=$2)
	`
	_, err = pg.FromContext(ctx).Exec(ctx, q, b.Hash(), b.Height, b, &b.BlockHeader)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return b, nil
}

func generateBlockScript(keys []*btcec.PublicKey, nSigs int) ([]byte, error) {
	var addrs []*btcutil.AddressPubKey
	for _, key := range keys {
		keyData := key.SerializeCompressed()
		addr, err := btcutil.NewAddressPubKey(keyData, &chaincfg.MainNetParams)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return txscript.MultiSigScript(addrs, nSigs)
}
