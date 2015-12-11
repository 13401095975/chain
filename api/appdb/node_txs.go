package appdb

import (
	"database/sql"
	"encoding/json"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
)

func WriteIssuerTx(ctx context.Context, txHash string, data []byte, iNodeID string, asset string) error {
	issuerQ := `
		INSERT INTO issuer_txs (issuer_node_id, txid, data)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	var id string
	err := pg.FromContext(ctx).QueryRow(issuerQ, iNodeID, txHash, data).Scan(&id)
	if err != nil {
		return errors.Wrap(err, "insert issuer tx")
	}

	assetQ := `
		INSERT INTO issuer_txs_assets (issuer_tx_id, asset_id)
		VALUES ($1, $2)
	`
	_, err = pg.FromContext(ctx).Exec(assetQ, id, asset)
	return errors.Wrap(err, "insert issuer tx for asset")
}

func WriteManagerTx(ctx context.Context, txHash string, data []byte, mNodeID string, accounts []string) error {
	managerQ := `
		INSERT INTO manager_txs (manager_node_id, txid, data)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	var id string
	err := pg.FromContext(ctx).QueryRow(managerQ, mNodeID, txHash, data).Scan(&id)
	if err != nil {
		return errors.Wrap(err, "insert manager tx")
	}

	accountQ := `
		INSERT INTO manager_txs_accounts (manager_tx_id, account_id)
		VALUES ($1, unnest($2::text[]))
	`
	_, err = pg.FromContext(ctx).Exec(accountQ, id, pg.Strings(accounts))
	return errors.Wrap(err, "insert manager tx for account")
}

func ManagerTxs(ctx context.Context, managerNodeID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT id, data FROM manager_txs
		WHERE manager_node_id=$1 AND (($2 = '') OR (id < $2))
		ORDER BY id DESC LIMIT $3
	`

	rows, err := pg.FromContext(ctx).Query(q, managerNodeID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func AccountTxs(ctx context.Context, accountID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT mt.id, mt.data
		FROM manager_txs AS mt
		LEFT JOIN manager_txs_accounts AS a
		ON mt.id=a.manager_tx_id
		WHERE a.account_id=$1 AND (($2 = '') OR (mt.id < $2))
		ORDER BY mt.id DESC LIMIT $3
	`

	rows, err := pg.FromContext(ctx).Query(q, accountID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func IssuerTxs(ctx context.Context, inodeID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT id, data FROM issuer_txs
		WHERE issuer_node_id = $1 AND (($2 = '') OR (id < $2))
		ORDER BY id DESC LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(q, inodeID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func AssetTxs(ctx context.Context, assetID string, prev string, limit int) ([]*json.RawMessage, string, error) {
	q := `
		SELECT it.id, it.data
		FROM issuer_txs AS it
		LEFT JOIN issuer_txs_assets AS a
		ON it.id = a.issuer_tx_id
		WHERE a.asset_id = $1 AND (($2 = '') OR (it.id < $2))
		ORDER BY it.id DESC LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(q, assetID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	return activityItemsFromRows(rows)
}

func ManagerTx(ctx context.Context, managerNodeID, txID string) (*json.RawMessage, error) {
	q := `
		SELECT data FROM manager_txs
		WHERE manager_node_id=$1 AND txid=$2
	`

	var a []byte
	err := pg.FromContext(ctx).QueryRow(q, managerNodeID, txID).Scan(&a)
	if err == sql.ErrNoRows {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "transaction id: %v", txID)
	}
	return (*json.RawMessage)(&a), err
}
