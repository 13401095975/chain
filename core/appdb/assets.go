package appdb

import (
	"database/sql"
	"time"

	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/cos/hdkey"
	"chain/database/pg"
	chainjson "chain/encoding/json"
	"chain/errors"
	"chain/metrics"
)

// Asset represents an asset type in the blockchain.
// It is made up of extended keys, and paths (indexes) within those keys.
type Asset struct {
	Hash            bc.AssetID // the raw Asset ID
	IssuerNodeID    string
	Label           string
	Keys            []*hdkey.XKey
	INIndex, AIndex []uint32
	RedeemScript    []byte
	GenesisHash     bc.Hash // TODO: Normalize field names to match spec? ("InitialBlock" and "IssuanceProgram")
	IssuanceScript  []byte
	Definition      []byte
	ClientToken     *string
}

// AssetSummary is a summary of an Asset, including data commonly exposed
// via API responses.
type AssetSummary struct {
	ID         bc.AssetID
	Label      string
	Definition chainjson.HexBytes
}

// AssetOwner indicates either an account or a manager node.
type AssetOwner int

// Valid values for AssetOwner.
const (
	OwnerAccount AssetOwner = iota
	OwnerManagerNode
)

type assetLookupQuery struct {
	hash         bc.AssetID
	issuerNodeID string
	clientToken  string
}

// AssetByID loads an asset from the database using its ID. If an asset has
// been archived, this function will return ErrArchived.
func AssetByID(ctx context.Context, hash bc.AssetID) (*Asset, error) {
	defer metrics.RecordElapsed(time.Now())
	asset, err := lookupAsset(ctx, assetLookupQuery{hash: hash})
	return asset, errors.WithDetailf(err, "asset id=%v", hash.String())
}

// AssetByClientToken loads an asset from the database using its issuer node id
// and its client token.
func AssetByClientToken(ctx context.Context, issuerNodeID string, clientToken string) (*Asset, error) {
	defer metrics.RecordElapsed(time.Now())
	query := assetLookupQuery{
		issuerNodeID: issuerNodeID,
		clientToken:  clientToken,
	}
	asset, err := lookupAsset(ctx, query)
	return asset, errors.WithDetailf(err, "asset issuer id=%s, client token=%s", issuerNodeID, clientToken)
}

func lookupAsset(ctx context.Context, query assetLookupQuery) (*Asset, error) {
	const baseQ = `
		SELECT assets.id, assets.keyset, redeem_script, genesis_hash, assets.label, issuer_node_id,
			key_index(issuer_nodes.key_index), key_index(assets.key_index), definition,
			issuance_script, assets.archived, assets.client_token
		FROM assets
		INNER JOIN issuer_nodes ON issuer_nodes.id=assets.issuer_node_id
		WHERE
	`
	var (
		xpubs    []string
		archived bool
		a        = &Asset{}
		q        = baseQ
		args     []interface{}
	)

	if query.issuerNodeID != "" && query.clientToken != "" {
		q = q + "assets.issuer_node_id = $1 AND assets.client_token = $2"
		args = []interface{}{query.issuerNodeID, query.clientToken}
	} else {
		q = q + "assets.id = $1"
		args = []interface{}{query.hash.String()}
	}

	err := pg.QueryRow(ctx, q, args...).Scan(
		&a.Hash,
		(*pg.Strings)(&xpubs),
		&a.RedeemScript,
		&a.GenesisHash,
		&a.Label,
		&a.IssuerNodeID,
		(*pg.Uint32s)(&a.INIndex),
		(*pg.Uint32s)(&a.AIndex),
		&a.Definition,
		&a.IssuanceScript,
		&archived,
		&a.ClientToken,
	)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	if archived {
		err = ErrArchived
	}
	if err != nil {
		return nil, err
	}

	a.Keys, err = stringsToKeys(xpubs)
	if err != nil {
		return nil, errors.Wrap(err, "parsing keys")
	}

	return a, nil
}

// InsertAsset adds the asset to the database. If the asset has a client token,
// and there already exists an asset for the same issuer node with that client
// token, InsertAsset will lookup and return the existing asset instead.
func InsertAsset(ctx context.Context, asset *Asset) (*Asset, error) {
	defer metrics.RecordElapsed(time.Now())
	const q = `
		INSERT INTO assets (id, issuer_node_id, key_index, keyset, redeem_script, genesis_hash, issuance_script, label, definition, client_token)
		VALUES($1, $2, to_key_index($3), $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
	`

	res, err := pg.Exec(ctx, q,
		asset.Hash.String(),
		asset.IssuerNodeID,
		pg.Uint32s(asset.AIndex),
		pg.Strings(keysToStrings(asset.Keys)),
		asset.RedeemScript,
		asset.GenesisHash,
		asset.IssuanceScript,
		asset.Label,
		asset.Definition,
		asset.ClientToken,
	)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrap(err, "retrieving rows affected")
	}
	if rowsAffected == 0 && asset.ClientToken != nil {
		// There is already an asset for this issuer node with the provided client
		// token. We should return the existing asset.
		asset, err = AssetByClientToken(ctx, asset.IssuerNodeID, *asset.ClientToken)
		return asset, errors.Wrap(err, "retrieving existing asset")
	}
	return asset, err
}

// ListAssets returns a paginated list of AssetSummaries
// belonging to the given issuer node, along with a sortable id
// for last asset, used to retrieve the next page.
func ListAssets(ctx context.Context, inodeID string, prev string, limit int) ([]*AssetSummary, string, error) {
	q := `
		SELECT id, label, definition, sort_id
		FROM assets
		WHERE issuer_node_id = $1 AND ($2='' OR sort_id<$2) AND NOT archived
		ORDER BY sort_id DESC
		LIMIT $3
	`
	var (
		assets  []*AssetSummary
		lastOut string
	)
	err := pg.ForQueryRows(ctx, q, inodeID, prev, limit, func(
		id bc.AssetID,
		label string,
		definition []byte,
		last string,
	) {
		assets = append(assets, &AssetSummary{
			ID:         id,
			Label:      label,
			Definition: definition,
		})
		lastOut = last
	})
	if err != nil {
		return nil, "", err
	}
	return assets, lastOut, nil
}

// GetAssets returns an AssetSummary for the given asset IDs. If the given
// asset IDs are not found, they will not be included in the response.
func GetAssets(ctx context.Context, assetIDs []string) (map[string]*AssetSummary, error) {
	const q = `
		SELECT id, label, definition
		FROM assets
		WHERE id IN (SELECT unnest($1::text[]))
	`

	res := make(map[string]*AssetSummary)
	err := pg.ForQueryRows(ctx, q, pg.Strings(assetIDs), func(id bc.AssetID, label string, def chainjson.HexBytes) {
		assetDefinition := make([]byte, len(def))
		copy(assetDefinition, def[:])
		res[id.String()] = &AssetSummary{
			ID:         id,
			Label:      label,
			Definition: assetDefinition,
		}
	})
	return res, errors.Wrap(err)
}

// GetAsset returns an AssetSummary for the given asset id.
func GetAsset(ctx context.Context, assetID string) (*AssetSummary, error) {
	assets, err := GetAssets(ctx, []string{assetID})
	if err != nil {
		return nil, err
	}
	a, ok := assets[assetID]
	if !ok {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "asset ID: %q", assetID)
	}
	return a, nil
}

// UpdateAsset updates the label of an asset.
func UpdateAsset(ctx context.Context, assetID string, label *string) error {
	if label == nil {
		return nil
	}
	const q = `UPDATE assets SET label = $2 WHERE id = $1`
	_, err := pg.Exec(ctx, q, assetID, *label)
	return errors.Wrap(err, "update query")
}

// ArchiveAsset marks an asset as archived. Once an asset has been archived, it
// cannot be issued, and it won't show up in listAsset responses.
func ArchiveAsset(ctx context.Context, assetID string) error {
	const q = `UPDATE assets SET archived = true WHERE id = $1`
	_, err := pg.Exec(ctx, q, assetID)
	return errors.Wrap(err, "archive query")
}

// AssetBalQuery is a parameter struct passed in to AssetBalance
type AssetBalQuery struct {
	Owner     AssetOwner
	AccountID string
	// Set the following for the full paginated list
	Prev  string
	Limit int

	// EXPERIMENTAL - implemented for Glitterco
	// Set the following for a filtered list of assets
	AssetIDs []string
}

// AssetBalance fetches the balances of assets contained in an account.
// It returns a slice of Balances and the last asset ID in the page.
// Each Balance contains an asset ID, a confirmed balance,
// and a total balance. Assets are sorted by their IDs.
func AssetBalance(ctx context.Context, abq *AssetBalQuery) ([]*Balance, string, error) {
	paginating := abq.Limit > 0
	if paginating && len(abq.AssetIDs) > 0 {
		return nil, "", errors.New("cannot set both pagination and asset id filter")
	} else if !paginating && len(abq.AssetIDs) == 0 {
		return nil, "", errors.New("must have limit or asset id filter")
	}

	filter := "a.asset_id=ANY($2)"
	limitQ := ""
	params := []interface{}{abq.AccountID, pg.Strings(abq.AssetIDs)}
	if paginating {
		filter = "($2='' OR a.asset_id>$2)"
		limitQ = "LIMIT $3"
		params = []interface{}{abq.AccountID, abq.Prev, abq.Limit}
	}

	q := `
		WITH combined_utxos AS (
			SELECT a.amount, a.asset_id, a.tx_hash, a.index,
			manager_node_id, account_id,
			confirmed_in IS NOT NULL as confirmed,
			reservation_id IS NOT NULL as spent_in_pool
			FROM account_utxos a
			WHERE account_id=$1 AND ` + filter + `
		), amounts AS (
			SELECT
				(CASE WHEN confirmed THEN amount ELSE 0 END) as confirmed_amount,
				(CASE WHEN NOT spent_in_pool THEN amount ELSE 0 END) as total_amount,
				asset_id FROM combined_utxos
				WHERE confirmed OR NOT spent_in_pool
		)
		SELECT sum(confirmed_amount), sum(total_amount), asset_id
		FROM amounts
		GROUP BY asset_id
		ORDER BY asset_id ASC

	` + limitQ

	rows, err := pg.Query(ctx, q, params...)
	if err != nil {
		return nil, "", errors.Wrap(err, "balance query")
	}
	defer rows.Close()

	var (
		res  []*Balance
		last string
	)
	for rows.Next() {
		var bal Balance
		err = rows.Scan(&bal.Confirmed, &bal.Total, &bal.AssetID)
		if err != nil {
			return nil, "", errors.Wrap(err, "row scan")
		}
		res = append(res, &bal)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errors.Wrap(err, "rows end")
	}

	if paginating && len(res) == abq.Limit {
		last = res[len(res)-1].AssetID.String()
	}
	return res, last, nil
}
