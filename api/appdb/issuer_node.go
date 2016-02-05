package appdb

import (
	"database/sql"
	"time"

	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
	"chain/metrics"
)

// IssuerNode represents a single issuer ndoe. It is intended to be used wth API
// responses.
type IssuerNode struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Keys     []*NodeKey `json:"keys,omitempty"`
	SigsReqd int        `json:"signatures_required"`
}

// InsertIssuerNode adds the issuer node to the database
func InsertIssuerNode(ctx context.Context, projID, label string, xpubs, gennedKeys []*hdkey.XKey, sigsRequired int) (*IssuerNode, error) {
	_ = pg.FromContext(ctx).(pg.Tx) // panic if not in a db transaction

	const q = `
		INSERT INTO issuer_nodes (label, project_id, keyset, generated_keys, sigs_required)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	var id string
	err := pg.FromContext(ctx).QueryRow(ctx, q,
		label,
		projID,
		pg.Strings(keysToStrings(xpubs)),
		pg.Strings(keysToStrings(gennedKeys)),
		sigsRequired,
	).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "insert issuer node")
	}

	keys, err := buildNodeKeys(xpubs, gennedKeys)
	if err != nil {
		return nil, errors.Wrap(err, "generating node key list")
	}

	return &IssuerNode{
		ID:       id,
		Label:    label,
		Keys:     keys,
		SigsReqd: sigsRequired,
	}, nil
}

// NextAsset returns all data needed
// for creating a new asset. This includes
// all keys, the issuer node index, a
// new index for the asset being created,
// and the number of signatures required.
func NextAsset(ctx context.Context, inodeID string) (asset *Asset, sigsRequired int, err error) {
	defer metrics.RecordElapsed(time.Now())
	const q = `
		SELECT keyset,
		key_index(key_index),
		key_index(nextval('assets_key_index_seq'::regclass)-1),
		sigs_required FROM issuer_nodes
		WHERE id=$1
	`
	asset = &Asset{IssuerNodeID: inodeID}
	var (
		xpubs   []string
		sigsReq int
	)
	err = pg.FromContext(ctx).QueryRow(ctx, q, inodeID).Scan(
		(*pg.Strings)(&xpubs),
		(*pg.Uint32s)(&asset.INIndex),
		(*pg.Uint32s)(&asset.AIndex),
		&sigsReq,
	)
	if err == sql.ErrNoRows {
		err = pg.ErrUserInputNotFound
	}
	if err != nil {
		return nil, 0, errors.WithDetailf(err, "issuer node %v: get key info", inodeID)
	}

	asset.Keys, err = stringsToKeys(xpubs)
	if err != nil {
		return nil, 0, errors.Wrap(err, "parsing keys")
	}

	return asset, sigsReq, nil
}

// ListIssuerNodes returns a list of issuer nodes belonging to the given
// project.
func ListIssuerNodes(ctx context.Context, projID string) ([]*IssuerNode, error) {
	q := `
		SELECT id, label
		FROM issuer_nodes
		WHERE project_id = $1 AND NOT archived
		ORDER BY id
	`
	rows, err := pg.FromContext(ctx).Query(ctx, q, projID)
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var inodes []*IssuerNode
	for rows.Next() {
		inode := new(IssuerNode)
		err := rows.Scan(&inode.ID, &inode.Label)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		inodes = append(inodes, inode)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	return inodes, nil
}

// GetIssuerNode returns basic information about a single issuer node.
func GetIssuerNode(ctx context.Context, groupID string) (*IssuerNode, error) {
	var (
		q           = `SELECT label, keyset, generated_keys, sigs_required FROM issuer_nodes WHERE id = $1`
		label       string
		pubKeyStrs  []string
		privKeyStrs []string
		sigsReqd    int
	)
	err := pg.FromContext(ctx).QueryRow(ctx, q, groupID).Scan(
		&label,
		(*pg.Strings)(&pubKeyStrs),
		(*pg.Strings)(&privKeyStrs),
		&sigsReqd,
	)
	if err == sql.ErrNoRows {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "issuer node ID: %v", groupID)
	}
	if err != nil {
		return nil, err
	}

	xpubs, err := stringsToKeys(pubKeyStrs)
	if err != nil {
		return nil, errors.Wrap(err, "parsing pub keys")
	}

	xprvs, err := stringsToKeys(privKeyStrs)
	if err != nil {
		return nil, errors.Wrap(err, "parsing private keys")
	}

	keys, err := buildNodeKeys(xpubs, xprvs)
	if err != nil {
		return nil, errors.Wrap(err, "generating node key list")
	}

	return &IssuerNode{
		ID:       groupID,
		Label:    label,
		Keys:     keys,
		SigsReqd: sigsReqd,
	}, nil
}

// UpdateIssuerNode updates the label of an issuer node.
func UpdateIssuerNode(ctx context.Context, inodeID string, label *string) error {
	if label == nil {
		return nil
	}
	const q = `UPDATE issuer_nodes SET label = $2 WHERE id = $1`
	db := pg.FromContext(ctx)
	_, err := db.Exec(ctx, q, inodeID, *label)
	return errors.Wrap(err, "update query")
}

// ArchiveIssuerNode marks an issuer node as archived.
// Archived issuer nodes do not appear for their parent projects,
// in the dashboard or for listIssuerNodes. They cannot create new
// assets, and their preexisting assets become archived.
//
// Must be called inside a database transaction.
func ArchiveIssuerNode(ctx context.Context, inodeID string) error {
	_ = pg.FromContext(ctx).(pg.Tx) // panics if not in a db transaction

	const assetQ = `UPDATE assets SET archived = true WHERE issuer_node_id = $1`
	_, err := pg.FromContext(ctx).Exec(ctx, assetQ, inodeID)
	if err != nil {
		return errors.Wrap(err, "archiving assets")
	}

	const q = `UPDATE issuer_nodes SET archived = true WHERE id = $1`
	_, err = pg.FromContext(ctx).Exec(ctx, q, inodeID)
	if err != nil {
		return errors.Wrap(err, "archive query")
	}

	return nil
}
