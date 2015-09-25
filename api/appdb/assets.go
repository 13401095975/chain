package appdb

import (
	"database/sql"
	"time"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
	"chain/fedchain-sandbox/wire"
	"chain/metrics"
)

// ErrBadAsset is an error that means the string
// used as an asset id was not a valid base58 id.
var ErrBadAsset = errors.New("invalid asset")

// Asset represents an asset type in the blockchain.
// It is made up of extended keys, and paths (indexes) within those keys.
// Assets belong to wallets.
type Asset struct {
	Hash            wire.Hash20 // the raw Asset ID
	GroupID         string
	Label           string
	Keys            []*hdkey.XKey
	AGIndex, AIndex []uint32
	RedeemScript    []byte
}

// AssetResponse is a JSON-serializable version of Asset, intended for use in
// API responses.
type AssetResponse struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Circulation uint64 `json:"circulation"`
}

// AssetByID loads an asset from the database using its ID.
func AssetByID(ctx context.Context, id string) (*Asset, error) {
	defer metrics.RecordElapsed(time.Now())
	const q = `
		SELECT assets.keyset, redeem_script, asset_group_id,
			key_index(asset_groups.key_index), key_index(assets.key_index)
		FROM assets
		INNER JOIN asset_groups ON asset_groups.id=assets.asset_group_id
		WHERE assets.id=$1
	`
	var (
		xpubs []string
		a     = new(Asset)
	)
	var err error
	a.Hash, err = wire.NewHash20FromStr(id)
	if err != nil {
		return nil, errors.WithDetailf(ErrBadAsset, "asset id=%v", id)
	}
	err = pg.FromContext(ctx).QueryRow(q, id).Scan(
		(*pg.Strings)(&xpubs),
		&a.RedeemScript,
		&a.GroupID,
		(*pg.Uint32s)(&a.AGIndex),
		(*pg.Uint32s)(&a.AIndex),
	)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	if err != nil {
		return nil, errors.WithDetailf(err, "asset id=%v", id)
	}

	a.Keys, err = xpubsToKeys(xpubs)
	if err != nil {
		return nil, errors.Wrap(err, "parsing keys")
	}

	return a, nil
}

// InsertAsset adds the asset to the database
func InsertAsset(ctx context.Context, asset *Asset) error {
	defer metrics.RecordElapsed(time.Now())
	const q = `
		INSERT INTO assets (id, asset_group_id, key_index, keyset, redeem_script, label)
		VALUES($1, $2, to_key_index($3), $4, $5, $6)
	`

	_, err := pg.FromContext(ctx).Exec(q,
		asset.Hash.String(),
		asset.GroupID,
		pg.Uint32s(asset.AIndex),
		pg.Strings(keysToXPubs(asset.Keys)),
		asset.RedeemScript,
		asset.Label,
	)
	return err
}

// ListAssets returns a paginated list of AssetResponses
// belonging to the given asset group, along with a sortable id
// for last asset, used to retrieve the next page.
func ListAssets(ctx context.Context, groupID string, prev string, limit int) ([]*AssetResponse, string, error) {
	q := `
		SELECT id, label, issued, sort_id
		FROM assets
		WHERE asset_group_id = $1 AND ($2='' OR sort_id<$2)
		ORDER BY sort_id DESC
		LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(q, groupID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "select query")
	}

	var (
		assets []*AssetResponse
		last   string
	)
	for rows.Next() {
		a := new(AssetResponse)
		err := rows.Scan(&a.ID, &a.Label, &a.Circulation, &last)
		if err != nil {
			return nil, "", errors.Wrap(err, "row scan")
		}
		assets = append(assets, a)
	}

	if err := rows.Close(); err != nil {
		return nil, "", errors.Wrap(err, "end row scan loop")
	}

	return assets, last, nil
}

// GetAsset returns an AssetResponse for the given asset id.
func GetAsset(ctx context.Context, assetID string) (*AssetResponse, error) {
	const q = `SELECT id, label, issued FROM assets WHERE id=$1`
	a := new(AssetResponse)

	err := pg.FromContext(ctx).QueryRow(q, assetID).Scan(&a.ID, &a.Label, &a.Circulation)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	return a, errors.WithDetailf(err, "asset id: %s", assetID)
}

// AddIssuance increases the issued column on an asset
// by the amount provided.
func AddIssuance(ctx context.Context, id string, amount int64) error {
	const q = `UPDATE assets SET issued=issued+$1 WHERE id=$2`
	_, err := pg.FromContext(ctx).Exec(q, amount, id)
	return errors.Wrap(err)
}
