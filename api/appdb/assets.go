package appdb

import (
	"database/sql"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain/wire"
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
	Keys            []*Key
	AGIndex, AIndex []uint32
	RedeemScript    []byte
}

// AssetByID loads an asset from the database using its ID.
func AssetByID(ctx context.Context, id string) (*Asset, error) {
	const q = `
		SELECT assets.keyset, redeem_script, asset_group_id,
			key_index(asset_groups.key_index), key_index(assets.key_index)
		FROM assets
		INNER JOIN asset_groups ON asset_groups.id=assets.asset_group_id
		WHERE assets.id=$1
	`
	var (
		keyIDs []string
		a      = new(Asset)
	)
	var err error
	a.Hash, err = wire.NewHash20FromStr(id)
	if err != nil {
		return nil, errors.WithDetailf(ErrBadAsset, "asset id=%v", id)
	}
	err = pg.FromContext(ctx).QueryRow(q, id).Scan(
		(*pg.Strings)(&keyIDs),
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

	a.Keys, err = GetKeys(ctx, keyIDs)
	if err != nil {
		return nil, err
	}

	return a, nil
}

// InsertAsset adds the asset to the database
func InsertAsset(ctx context.Context, asset *Asset) error {
	const q = `
		INSERT INTO assets (id, asset_group_id, key_index, keyset, redeem_script, label)
		VALUES($1, $2, to_key_index($3), $4, $5, $6)
	`
	var keyIDs []string
	for _, key := range asset.Keys {
		keyIDs = append(keyIDs, key.ID)
	}

	_, err := pg.FromContext(ctx).Exec(q,
		asset.Hash.String(),
		asset.GroupID,
		pg.Uint32s(asset.AIndex),
		pg.Strings(keyIDs),
		asset.RedeemScript,
		asset.Label,
	)
	return err
}
