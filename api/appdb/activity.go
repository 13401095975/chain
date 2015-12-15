package appdb

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/database/sql"
	"chain/errors"
	"chain/fedchain-sandbox/txscript"
	"chain/fedchain/bc"
	"chain/metrics"
	"chain/strings"
)

// Errors return by functions in this file
var (
	ErrInvalidIssuanceActivity = errors.New("cannot generate activity for invalid issuance")
)

// WriteActivity generates formatted activity history for the given transaction.
//
// Change flags on outputs are important to activity formatting. WriteActivity
// will check the addresses table to determine if specific addresses used as
// change, but at present, the addresses table is not comprehensive. The
// outIsChange parameter is provided to supplement the address table. It is
// typically populated using data bundled with a transaction template.
func WriteActivity(ctx context.Context, tx *bc.Tx, outIsChange map[int]bool, txTime time.Time) error {
	defer metrics.RecordElapsed(time.Now())
	_ = pg.FromContext(ctx).(pg.Tx) // panics if not in a db transaction

	// Fetch UTXO data for all outputs involved in the transaction.
	ins, outs, err := GetActUTXOs(ctx, tx)
	if err != nil {
		return errors.Wrap(err, "get tx utxos")
	}

	// Add change flags to outputs
	err = markChangeOuts(ctx, outs, outIsChange)
	if err != nil {
		return errors.Wrap(err, "mark change outs")
	}

	// Extract IDs for all resources involved in the transaction. The lists
	// should not contain duplicates.
	assetIDs, accountIDs, managerNodeIDs, managerNodeAccounts := getIDsFromUTXOs(append(ins, outs...))

	// Gather additional data on relevant accounts.
	actAccounts, err := GetActAccounts(ctx, accountIDs)
	if err != nil {
		return errors.Wrap(err, "get accounts")
	}

	accountLabels := make(map[string]string)
	for _, a := range actAccounts {
		accountLabels[a.ID] = a.Label
	}

	// Gather additional data on relevant assets.
	actAssets, err := GetActAssets(ctx, assetIDs)
	if err != nil {
		return errors.Wrap(err, "get assets")
	}

	assetLabels := make(map[string]string)
	for _, a := range actAssets {
		assetLabels[a.ID] = a.Label
	}

	// We'll use the transaction hash several times, so we'll keep it around.
	txHash := tx.Hash.String()

	// Manager node activity
	for _, managerNodeID := range managerNodeIDs {
		r := coalesceActivity(ins, outs, managerNodeAccounts[managerNodeID])
		inAct, outAct := createActEntries(r, assetLabels, accountLabels)

		data, err := serializeActvity(txHash, txTime, inAct, outAct)
		if err != nil {
			return errors.Wrap(err, "serialize activity")
		}

		err = writeManagerNodeActivity(ctx, managerNodeID, txHash, data, managerNodeAccounts[managerNodeID])
		if err != nil {
			return errors.Wrap(err, "writing activity for manager node", managerNodeID)
		}
	}

	// Issuance activity
	if isIssuance(tx) {
		// Only one asset may be issued per transaction.
		if len(actAssets) != 1 {
			return errors.Wrap(ErrInvalidIssuanceActivity, "asset count:", len(actAssets))
		}

		var visibleAccounts []string
		for _, a := range actAccounts {
			if a.ProjID == actAssets[0].ProjID {
				visibleAccounts = append(visibleAccounts, a.ID)
			}
		}

		r := coalesceActivity(ins, outs, visibleAccounts)
		inAct, outAct := createActEntries(r, assetLabels, accountLabels)

		data, err := serializeActvity(txHash, txTime, inAct, outAct)
		if err != nil {
			return errors.Wrap(err, "serialize activity")
		}

		err = writeIssuanceActivity(ctx, actAssets[0], txHash, data)
		if err != nil {
			return errors.Wrap(err, "writing activity for issuer node", actAssets[0].IssuerNodeID)
		}
	}

	return nil
}

func ManagerNodeActivity(ctx context.Context, managerNodeID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT id, data FROM activity
		WHERE manager_node_id=$1 AND (($2 = '') OR (id < $2))
		ORDER BY id DESC LIMIT $3
	`

	rows, err := pg.FromContext(ctx).Query(ctx, q, managerNodeID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func AccountActivity(ctx context.Context, accountID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT a.id, a.data
		FROM activity AS a
		LEFT JOIN activity_accounts AS aa
		ON a.id=aa.activity_id
		WHERE aa.account_id=$1 AND (($2 = '') OR (a.id < $2))
		ORDER BY a.id DESC LIMIT $3
	`

	rows, err := pg.FromContext(ctx).Query(ctx, q, accountID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func IssuerNodeActivity(ctx context.Context, inodeID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT id, data FROM issuance_activity
		WHERE issuer_node_id = $1 AND (($2 = '') OR (id < $2))
		ORDER BY id DESC LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, inodeID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func AssetActivity(ctx context.Context, assetID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT ia.id, ia.data
		FROM issuance_activity AS ia
		LEFT JOIN issuance_activity_assets AS j
		ON ia.id = j.issuance_activity_id
		WHERE j.asset_id = $1 AND (($2 = '') OR (ia.id < $2))
		ORDER BY ia.id DESC LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, assetID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func activityItemsFromRows(rows *sql.Rows) (items []*json.RawMessage, last string, err error) {
	for rows.Next() {
		var a []byte
		err := rows.Scan(&last, &a)
		if err != nil {
			err = errors.Wrap(err, "row scan")
			return nil, "", err
		}

		items = append(items, (*json.RawMessage)(&a))
	}

	if rows.Err() != nil {
		err = errors.Wrap(rows.Err(), "rows")
		return nil, "", err
	}

	return items, last, nil
}

func ManagerNodeTxActivity(ctx context.Context, managerNodeID, txID string) (*json.RawMessage, error) {
	q := `
		SELECT data FROM activity
		WHERE manager_node_id=$1 AND txid=$2
	`

	var a []byte
	err := pg.FromContext(ctx).QueryRow(ctx, q, managerNodeID, txID).Scan(&a)
	if err == sql.ErrNoRows {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "transaction id: %v", txID)
	}
	return (*json.RawMessage)(&a), err
}

type ActUTXO struct {
	AssetID       string
	Amount        uint64
	ManagerNodeID string
	AccountID     string
	Addr          string
	Script        []byte
	IsChange      bool
}

type ActAsset struct {
	ID           string
	Label        string
	IssuerNodeID string
	ProjID       string
}

type ActAccount struct {
	ID            string
	Label         string
	ManagerNodeID string
	ProjID        string
}

type txRawActivity struct {
	insByAsset    map[string]map[string]int64
	insByAccount  map[string]map[string]int64
	outsByAsset   map[string]map[string]int64
	outsByAccount map[string]map[string]int64
}

type actEntry struct {
	Address      string `json:"address,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	AccountLabel string `json:"account_label,omitempty"`

	Amount     int64  `json:"amount"`
	AssetID    string `json:"asset_id"`
	AssetLabel string `json:"asset_label"`
}

type actEntryOrder []actEntry

func (a actEntryOrder) Len() int      { return len(a) }
func (a actEntryOrder) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a actEntryOrder) Less(i, j int) bool {
	// Show account items first
	if a[i].AccountLabel != "" && a[j].AccountLabel == "" {
		return true
	}
	if a[i].AccountLabel == "" && a[j].AccountLabel != "" {
		return false
	}

	// Sort by account ID, address, asset ID, and amount
	if a[i].AccountLabel != a[j].AccountLabel {
		return a[i].AccountLabel < a[j].AccountLabel
	}
	if a[i].Address != a[j].Address {
		return a[i].Address < a[j].Address
	}
	if a[i].AssetLabel != a[j].AssetLabel {
		return a[i].AssetLabel < a[j].AssetLabel
	}

	// If coalescing similar assets within the same account or address space is
	// successful, we shouldn't ever get here.
	return a[i].Amount < a[j].Amount
}

type actItem struct {
	TxHash  string     `json:"transaction_id"`
	Time    time.Time  `json:"transaction_time"`
	Inputs  []actEntry `json:"inputs"`
	Outputs []actEntry `json:"outputs"`
}

// GetActUTXOs returns information about outputs from both sides of a transaciton.
func GetActUTXOs(ctx context.Context, tx *bc.Tx) (ins, outs []*ActUTXO, err error) {
	var (
		txHashStr  = tx.Hash.String()
		isIssuance = tx.IsIssuance()

		hashes  []string
		indexes []uint32
	)

	if !isIssuance {
		for _, in := range tx.Inputs {
			hashes = append(hashes, in.Previous.Hash.String())
			indexes = append(indexes, in.Previous.Index)
		}
	}

	for i := range tx.Outputs {
		hashes = append(hashes, txHashStr)
		indexes = append(indexes, uint32(i))
	}

	// Both confirmed (blockchain) utxos and unconfirmed (pool) utxos
	const q = `
		WITH outpoints AS (SELECT unnest($1::text[]), unnest($2::bigint[]))
			SELECT tx_hash, index,
				asset_id, amount, script,
				account_id, manager_node_id
			FROM utxos
			WHERE (tx_hash, index) IN (TABLE outpoints)
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, pg.Strings(hashes), pg.Uint32s(indexes))
	if err != nil {
		return nil, nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	all := make(map[bc.Outpoint]*ActUTXO)
	for rows.Next() {
		var (
			hash  bc.Hash
			index uint32
			utxo  = new(ActUTXO)
		)
		err := rows.Scan(
			&hash, &index,
			&utxo.AssetID, &utxo.Amount, &utxo.Script,
			&utxo.AccountID, &utxo.ManagerNodeID,
		)
		if err != nil {
			return nil, nil, errors.Wrap(err, "row scan")
		}

		addr, err := txscript.PkScriptAddr(utxo.Script)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "get addr from script: %x", utxo.Script)
		}
		utxo.Addr = addr.String()

		all[bc.Outpoint{Hash: hash, Index: index}] = utxo
	}
	if rows.Err() != nil {
		return nil, nil, errors.Wrap(rows.Err(), "end row scan loop")
	}

	if len(all) != len(hashes) {
		err := fmt.Errorf("found %d utxos for %d outpoints", len(all), len(hashes))
		return nil, nil, errors.Wrap(err)
	}

	if !isIssuance {
		for _, in := range tx.Inputs {
			ins = append(ins, all[in.Previous])
		}
	}

	for i := range tx.Outputs {
		op := bc.Outpoint{Hash: tx.Hash, Index: uint32(i)}
		outs = append(outs, all[op])
	}

	return ins, outs, nil
}

// markChangeOuts sets the change flag on a set of transaction UTXOs. It checks
// both the outIsChange parameter and the addresses table.
func markChangeOuts(ctx context.Context, utxos []*ActUTXO, outIsChange map[int]bool) error {
	var (
		unknownAddrs  []string
		unknownByAddr = make(map[string]*ActUTXO)
	)
	for i, u := range utxos {
		if outIsChange[i] {
			u.IsChange = true
		} else {
			unknownAddrs = append(unknownAddrs, u.Addr)
			unknownByAddr[u.Addr] = u
		}
	}

	const q = `
		SELECT address
		FROM addresses
		WHERE address IN (SELECT unnest($1::text[]))
		AND is_change = true
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, pg.Strings(unknownAddrs))
	if err != nil {
		return errors.Wrap(err, "select query")
	}
	defer rows.Close()

	for rows.Next() {
		var addr string
		err := rows.Scan(&addr)
		if err != nil {
			return errors.Wrap(err, "row scan")
		}

		unknownByAddr[addr].IsChange = true
	}

	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "end row scan loop")
	}

	return nil
}

// getIDsFromUTXOs extracts lists of unique identifiers present from the
// specified UTXOs. It is useful for determining the range of resources involved
// in a transaction.
func getIDsFromUTXOs(utxos []*ActUTXO) (
	assetIDs []string, // list of unique asset IDs
	accountIDs []string, // list of unique account IDs
	managerNodeIDs []string, // list of unique manager node IDs
	managerNodeAccounts map[string][]string, // map of manager node IDs to unique account IDs
) {
	managerNodeAccounts = make(map[string][]string)
	for _, u := range utxos {
		if u != nil {
			assetIDs = append(assetIDs, u.AssetID)

			// outputs with pure address receivers will not have account IDs or manager node IDs.
			if u.AccountID != "" {
				accountIDs = append(accountIDs, u.AccountID)
				managerNodeIDs = append(managerNodeIDs, u.ManagerNodeID)
				managerNodeAccounts[u.ManagerNodeID] = append(managerNodeAccounts[u.ManagerNodeID], u.AccountID)
			}
		}
	}

	sort.Strings(assetIDs)
	sort.Strings(accountIDs)
	sort.Strings(managerNodeIDs)

	assetIDs = strings.Uniq(assetIDs)
	accountIDs = strings.Uniq(accountIDs)
	managerNodeIDs = strings.Uniq(managerNodeIDs)

	for managerNodeID, accounts := range managerNodeAccounts {
		sort.Strings(accounts)
		managerNodeAccounts[managerNodeID] = strings.Uniq(accounts)
	}

	return assetIDs, accountIDs, managerNodeIDs, managerNodeAccounts
}

func GetActAssets(ctx context.Context, assetIDs []string) ([]*ActAsset, error) {
	q := `
		SELECT a.id, a.label, i.id, i.project_id
		FROM assets a
		JOIN issuer_nodes i ON a.issuer_node_id = i.id
		WHERE a.id = ANY($1)
		ORDER BY a.id
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, pg.Strings(assetIDs))
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var res []*ActAsset
	for rows.Next() {
		a := new(ActAsset)
		err := rows.Scan(&a.ID, &a.Label, &a.IssuerNodeID, &a.ProjID)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		res = append(res, a)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	return res, nil
}

func GetActAccounts(ctx context.Context, accountIDs []string) ([]*ActAccount, error) {
	q := `
		SELECT acc.id, acc.label, acc.manager_node_id, mn.project_id
		FROM accounts acc
		JOIN manager_nodes mn ON acc.manager_node_id = mn.id
		WHERE acc.id = ANY($1)
		ORDER BY acc.id
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, pg.Strings(accountIDs))
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var res []*ActAccount
	for rows.Next() {
		a := new(ActAccount)
		err := rows.Scan(&a.ID, &a.Label, &a.ManagerNodeID, &a.ProjID)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		res = append(res, a)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	return res, nil
}

func coalesceActivity(ins, outs []*ActUTXO, visibleAccounts []string) txRawActivity {
	// create lookup tables for account visibility and change addresses
	isAccountVis := make(map[string]bool)
	for _, bid := range visibleAccounts {
		isAccountVis[bid] = true
	}

	res := txRawActivity{
		insByAsset:    make(map[string]map[string]int64),
		insByAccount:  make(map[string]map[string]int64),
		outsByAsset:   make(map[string]map[string]int64),
		outsByAccount: make(map[string]map[string]int64),
	}

	// Pool all inputs by address, or by account if the account is visible.
	for _, u := range ins {
		if isAccountVis[u.AccountID] {
			if res.insByAccount[u.AccountID] == nil {
				res.insByAccount[u.AccountID] = make(map[string]int64)
			}
			res.insByAccount[u.AccountID][u.AssetID] += int64(u.Amount)
		} else {
			if res.insByAsset[u.Addr] == nil {
				res.insByAsset[u.Addr] = make(map[string]int64)
			}
			res.insByAsset[u.Addr][u.AssetID] += int64(u.Amount)
		}
	}

	// Pool all outputs by address, or by account if the account is visible.
	for _, u := range outs {
		if isAccountVis[u.AccountID] {
			// Rather than create a discrete output for a change address, we
			// should deduct the value of the output from the corresponding
			// value in the input. To determine whether to do this, we'll use
			// the following heuristics:
			// 1. The output is paid to a change address.
			// 2. There is a corresponding input for the same account and asset.
			// 3. The input's value is greater than the output.

			if u.IsChange &&
				res.insByAccount[u.AccountID] != nil &&
				res.insByAccount[u.AccountID][u.AssetID] > int64(u.Amount) {
				res.insByAccount[u.AccountID][u.AssetID] -= int64(u.Amount)
			} else {
				if res.outsByAccount[u.AccountID] == nil {
					res.outsByAccount[u.AccountID] = make(map[string]int64)
				}
				res.outsByAccount[u.AccountID][u.AssetID] += int64(u.Amount)
			}
		} else {
			if res.outsByAsset[u.Addr] == nil {
				res.outsByAsset[u.Addr] = make(map[string]int64)
			}
			res.outsByAsset[u.Addr][u.AssetID] += int64(u.Amount)
		}
	}

	return res
}

// createActEntries takes coalesced activity entries and replaces address IDs
// with addresses, and attaches asset and account labels. It ensures the result
// is sorted in a consistent order.
func createActEntries(
	r txRawActivity,
	assetLabels map[string]string,
	accountLabels map[string]string,
) (ins, outs []actEntry) {
	for addr, assetAmts := range r.insByAsset {
		for assetID, amt := range assetAmts {
			ins = append(ins, actEntry{
				Address:    addr,
				AssetID:    assetID,
				AssetLabel: assetLabels[assetID],
				Amount:     amt,
			})
		}
	}

	for accountID, assetAmts := range r.insByAccount {
		for assetID, amt := range assetAmts {
			ins = append(ins, actEntry{
				AccountID:    accountID,
				AccountLabel: accountLabels[accountID],
				AssetID:      assetID,
				AssetLabel:   assetLabels[assetID],
				Amount:       amt,
			})
		}
	}

	for addr, assetAmts := range r.outsByAsset {
		for assetID, amt := range assetAmts {
			outs = append(outs, actEntry{
				Address:    addr,
				AssetID:    assetID,
				AssetLabel: assetLabels[assetID],
				Amount:     amt,
			})
		}
	}

	for accountID, assetAmts := range r.outsByAccount {
		for assetID, amt := range assetAmts {
			outs = append(outs, actEntry{
				AccountID:    accountID,
				AccountLabel: accountLabels[accountID],
				AssetID:      assetID,
				AssetLabel:   assetLabels[assetID],
				Amount:       amt,
			})
		}
	}

	sort.Sort(actEntryOrder(ins))
	sort.Sort(actEntryOrder(outs))

	return ins, outs
}

// TODO(jeffomatic): This is identical to asset.isIssuance, but is copied here
// to avoid circular dependencies betwen the two packages. This should probably
// be moved to the fedchain(-sandbox?)/wire package at some point.
func isIssuance(msg *bc.Tx) bool {
	if len(msg.Inputs) == 1 && msg.Inputs[0].IsIssuance() {
		if len(msg.Outputs) == 0 {
			return false
		}
		assetID := msg.Outputs[0].AssetID
		for _, out := range msg.Outputs {
			if out.AssetID != assetID {
				return false
			}
		}
		return true
	}
	return false
}

func serializeActvity(txHash string, txTime time.Time, ins, outs []actEntry) ([]byte, error) {
	if ins == nil {
		ins = []actEntry{}
	}
	if outs == nil {
		outs = []actEntry{}
	}

	return json.Marshal(actItem{
		TxHash:  txHash,
		Time:    txTime.UTC(),
		Inputs:  ins,
		Outputs: outs,
	})
}

func writeManagerNodeActivity(ctx context.Context, managerNodeID, txHash string, data []byte, accountIDs []string) error {
	aq := `
		INSERT INTO activity (manager_node_id, txid, data)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	var id string
	err := pg.FromContext(ctx).QueryRow(ctx, aq, managerNodeID, txHash, data).Scan(&id)
	if err != nil {
		return errors.Wrap(err, "insert activity")
	}

	accountq := `
		INSERT INTO activity_accounts (activity_id, account_id)
		VALUES ($1, unnest($2::text[]))
	`
	_, err = pg.FromContext(ctx).Exec(ctx, accountq, id, pg.Strings(accountIDs))
	if err != nil {
		return errors.Wrap(err, "insert activity for account")
	}

	return nil
}

func writeIssuanceActivity(ctx context.Context, a *ActAsset, txHash string, data []byte) error {
	iaq := `
		INSERT INTO issuance_activity (issuer_node_id, txid, data)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	var id string
	err := pg.FromContext(ctx).QueryRow(ctx, iaq, a.IssuerNodeID, txHash, data).Scan(&id)
	if err != nil {
		return errors.Wrap(err, "insert issuance activity")
	}

	assetq := `
		INSERT INTO issuance_activity_assets (issuance_activity_id, asset_id)
		VALUES ($1, $2)
	`
	_, err = pg.FromContext(ctx).Exec(ctx, assetq, id, a.ID)
	if err != nil {
		return errors.Wrap(err, "insert issuance activity for asset")
	}

	return nil
}
