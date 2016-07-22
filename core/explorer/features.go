package explorer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"

	"chain/core/asset"
	"chain/cos/bc"
	"chain/cos/state"
	"chain/database/pg"
	"chain/errors"
	chainlog "chain/log"
	"chain/net/http/httpjson"
)

func (e *Explorer) indexHistoricalBlock(ctx context.Context, block *bc.Block) {
	var (
		newTxHashes   pg.Strings
		newIndexes    pg.Uint32s
		newAssetIDs   pg.Strings
		newAmounts    pg.Uint64s
		newScripts    pg.Byteas
		newMetadatas  pg.Byteas
		spentTxHashes pg.Strings
		spentIndexes  pg.Uint32s
	)

	// TODO(bobg): Rename timespan to timespanMS.

	const insertQ = `
			INSERT INTO explorer_outputs (tx_hash, index, asset_id, amount, script, metadata, timespan)
				SELECT UNNEST($1::TEXT[]), UNNEST($2::INTEGER[]), UNNEST($3::TEXT[]), UNNEST($4::BIGINT[]), UNNEST($5::BYTEA[]), UNNEST($6::BYTEA[]), INT8RANGE($7, NULL)
				ON CONFLICT (tx_hash, index) DO NOTHING
		`
	const updateQ = `
			UPDATE explorer_outputs SET timespan = INT8RANGE(LOWER(timespan), $3)
				WHERE (tx_hash, index) IN (SELECT unnest($1::text[]), unnest($2::integer[]))
		`
	const deleteQ = `
			DELETE FROM explorer_outputs
				WHERE (tx_hash, index) IN (SELECT unnest($1::text[]), unnest($2::integer[]))
		`

	var stateOuts []*state.Output

	for _, tx := range block.Transactions {
		txHash := tx.Hash
		txHashStr := txHash.String()
		for _, txin := range tx.Inputs {
			if txin.IsIssuance() {
				continue
			}
			outpoint := txin.Outpoint()
			spentTxHashes = append(spentTxHashes, outpoint.Hash.String())
			spentIndexes = append(spentIndexes, outpoint.Index)
		}
		for index, txout := range tx.Outputs {
			newTxHashes = append(newTxHashes, txHashStr)
			newIndexes = append(newIndexes, uint32(index))
			newAssetIDs = append(newAssetIDs, txout.AssetID.String())
			newAmounts = append(newAmounts, txout.Amount)
			newScripts = append(newScripts, txout.ControlProgram)
			newMetadatas = append(newMetadatas, txout.ReferenceData)

			if e.isManager {
				stateOut := &state.Output{
					TxOutput: *txout,
					Outpoint: bc.Outpoint{
						Hash:  txHash,
						Index: uint32(index),
					},
				}
				stateOuts = append(stateOuts, stateOut)
			}
		}
	}
	_, err := pg.Exec(ctx, insertQ, newTxHashes, newIndexes, newAssetIDs, newAmounts, newScripts, newMetadatas, block.TimestampMS)
	if err != nil {
		chainlog.Error(ctx, errors.Wrap(err, "inserting to explorer_outputs"))
		return // or panic?
	}
	if e.historical {
		_, err = pg.Exec(ctx, updateQ, spentTxHashes, spentIndexes, block.TimestampMS)
		if err != nil {
			chainlog.Error(ctx, errors.Wrap(err, "updating explorer_outputs"))
			return // or panic?
		}
	} else {
		_, err = pg.Exec(ctx, deleteQ, spentTxHashes, spentIndexes)
		if err != nil {
			chainlog.Error(ctx, errors.Wrap(err, "deleting explorer_outputs"))
			return // or panic?
		}
	}

	if e.isManager {
		txdbOutputs, err := asset.LoadAccountInfo(ctx, stateOuts)
		if err != nil {
			chainlog.Error(ctx, errors.Wrap(err, "loading account info for explorer_outputs"))
			return // or panic?
		}
		var (
			accountIDs pg.Strings
			txHashes   pg.Strings
			indexes    pg.Uint32s
		)
		for _, out := range txdbOutputs {
			accountIDs = append(accountIDs, out.AccountID)
			txHashes = append(txHashes, out.Outpoint.Hash.String())
			indexes = append(indexes, out.Outpoint.Index)
		}

		const annotateQ = `
				UPDATE explorer_outputs h SET account_id = t.account_id
					FROM (SELECT unnest($1::text[]) AS account_id, unnest($2::text[]) AS tx_hash, unnest($3::integer[]) AS index) t
					WHERE h.tx_hash = t.tx_hash AND h.index = t.index
			`
		_, err = pg.Exec(ctx, annotateQ, accountIDs, txHashes, indexes)
		if err != nil {
			chainlog.Error(ctx, errors.Wrap(err, "annotating explorer_outputs with account info"))
			return // or panic?
		}
	}

	if e.historical && e.maxAge > 0 && time.Since(e.lastPrune) >= 24*time.Hour {
		now := time.Now()
		before := now.Add(-e.maxAge)
		beforeMillis := before.UnixNano() / int64(time.Millisecond)
		_, err := pg.Exec(ctx, "DELETE FROM explorer_outputs WHERE UPPER(timespan) < $1", beforeMillis)
		if err == nil {
			e.lastPrune = now
		} else {
			chainlog.Error(ctx, errors.Wrap(err, "pruning explorer_outputs"))
		}
	}
}

// HistoricalBalancesByAccount queries the explorer_outputs table
// for outputs in the given account at the given time and sums them by
// assetID.  If the assetID parameter is non-nil, the output is
// constrained to the balance of that asset only.
func (e *Explorer) HistoricalBalancesByAccount(ctx context.Context, accountID string, timestamp time.Time, assetID *bc.AssetID, prev string, limit int) ([]bc.AssetAmount, string, error) {
	if limit > 0 && assetID != nil {
		return nil, "", errors.New("cannot set both pagination and asset id filter")
	}

	q := "SELECT asset_id, SUM(amount) FROM explorer_outputs WHERE account_id = $1 AND timespan @> $2::int8"
	args := []interface{}{
		accountID,
		int64(timestamp.UnixNano() / int64(time.Millisecond)),
	}

	if assetID != nil {
		q += " AND asset_id = $3"
		args = append(args, *assetID)
	} else if limit > 0 {
		q += " AND asset_id > $3"
		args = append(args, prev)
	}
	q += " GROUP BY asset_id"
	if limit > 0 {
		q += fmt.Sprintf(" ORDER BY asset_id LIMIT %d", limit)
	}

	var (
		output []bc.AssetAmount
		last   string
	)
	args = append(args, func(assetID bc.AssetID, amount uint64) {
		output = append(output, bc.AssetAmount{AssetID: assetID, Amount: amount})
	})
	err := pg.ForQueryRows(pg.NewContext(ctx, e.db), q, args...)
	if err != nil {
		return nil, "", err
	}

	if limit > 0 && len(output) > 0 {
		last = output[len(output)-1].AssetID.String()
	}
	return output, last, nil
}

// ListHistoricalOutputsByAsset returns an array of every UTXO that contains assetID at timestamp.
// When paginating, it takes a limit as well as `prev`, the last UTXO returned on the previous call.
// ListHistoricalOutputsByAsset expects prev to be of the format "hash:index".
func (e *Explorer) ListHistoricalOutputsByAsset(ctx context.Context, assetID bc.AssetID, timestamp time.Time, prev string, limit int) ([]*TxOutput, string, error) {
	if !e.historical && !timestamp.IsZero() {
		return nil, "", errors.WithDetail(httpjson.ErrBadRequest, "historical outputs aren't enabled on this core")
	} else if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return e.listHistoricalOutputsByAssetAndAccount(ctx, assetID, "", timestamp, prev, limit)
}

func (e *Explorer) listHistoricalOutputsByAssetAndAccount(ctx context.Context, assetID bc.AssetID, accountID string, timestamp time.Time, prev string, limit int) ([]*TxOutput, string, error) {
	tsMillis := timestamp.UnixNano() / int64(time.Millisecond)
	prevs := strings.Split(prev, ":")
	var (
		prevHash  string
		prevIndex int64
		err       error
	)

	if len(prevs) != 2 {
		// tolerate malformed/empty cursors
		prevHash = ""
		prevIndex = -1
	} else {
		prevHash = prevs[0]
		prevIndex, err = strconv.ParseInt(prevs[1], 10, 64)
		if err != nil {
			prevIndex = -1
		}
	}

	conditions := []string{
		"asset_id = $1",
		"timespan @> $2::int8",
		"tx_hash >= $3",
		"(tx_hash != $3 OR index > $4)", // prev index only matters if we're in the same tx
	}
	args := []interface{}{
		assetID, tsMillis, prevHash, prevIndex,
	}
	if accountID != "" {
		conditions = append(conditions, "account_id = $5")
		args = append(args, accountID)
	}

	var limitClause string
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}

	var (
		res  []*state.Output
		last string
	)
	args = append(args, func(hash bc.Hash, index uint32, amount uint64, script, metadata []byte) {
		outpt := bc.Outpoint{Hash: hash, Index: index}
		t := bc.NewTxOutput(assetID, amount, script, metadata)
		o := &state.Output{
			Outpoint: outpt,
			TxOutput: *t,
		}
		res = append(res, o)
	})
	q := fmt.Sprintf("SELECT tx_hash, index, amount, script, metadata FROM explorer_outputs WHERE %s %s", strings.Join(conditions, " AND "), limitClause)

	err = pg.ForQueryRows(pg.NewContext(ctx, e.db), q, args...)
	if err != nil {
		return nil, "", err
	}

	if len(res) > 0 {
		last = res[len(res)-1].Outpoint.String()
	}
	return stateOutsToTxOuts(res), last, nil
}
