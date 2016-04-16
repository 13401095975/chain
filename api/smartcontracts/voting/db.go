package voting

import (
	"fmt"

	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/database/pg"
	"chain/errors"
)

// RightWithUTXO encapsulates a claim to a right token and the current UTXO
// of the right token.
//
// UTXO      — The current utxo for this voting right. Any transactions
//             involving this voting right token need to consume this utxo.
// Outpoint  - The outpoint recording the account's claim to the voting right
//             token. If the Outpoint equals the UTXO, then this voting right
//             claim is the current holder. Otherwise, this claim doesn't
//             currently hold the voting right but may recall the claim by
//             spending the UTXO and invoking the recall clause in the
//             sigscript.
// AssetID   - The asset ID of the voting right token.
// AccountID - The account id that has a claim to the voting right token. This
//             may be nil if it's an account on another node.
//
type RightWithUTXO struct {
	UTXO         bc.Outpoint
	Outpoint     bc.Outpoint
	BlockHeight  uint64
	BlockTxIndex int
	AssetID      bc.AssetID
	AccountID    *string
	rightScriptData
}

type cursor struct {
	prevBlockHeight uint64
	prevBlockPos    int
}

func (c cursor) String() string {
	if c.prevBlockHeight == 0 && c.prevBlockPos == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%d", c.prevBlockHeight, c.prevBlockPos)
}

func insertVotingRight(ctx context.Context, assetID bc.AssetID, blockHeight uint64, blockTxIndex int, outpoint bc.Outpoint, data rightScriptData) error {
	const q = `
		INSERT INTO voting_right_txs
			(asset_id, account_id, tx_hash, index, block_height, block_tx_index, holder, deadline, delegatable, ownership_chain, admin_script)
			VALUES($1, (SELECT account_id FROM addresses WHERE pk_script=$6), $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tx_hash, index) DO NOTHING
	`
	_, err := pg.FromContext(ctx).Exec(ctx, q, assetID, outpoint.Hash, outpoint.Index, blockHeight, blockTxIndex,
		data.HolderScript, data.Deadline, data.Delegatable, data.OwnershipChain[:], data.AdminScript)
	return errors.Wrap(err, "inserting into voting_right_txs")
}

// voidRecalledVotingRights takes the outpoint of the contract being executed
// and an ownership hash in the active chain of ownership. It then voids
// all voting right claims back to—and including—the voting right with
// the provided ownership hash.
func voidRecalledVotingRights(ctx context.Context, out bc.Outpoint, ownershipHash bc.Hash) error {
	const q = `
		WITH right_token AS (
			SELECT asset_id
			FROM voting_right_txs
			WHERE tx_hash = $1 AND index = $2
			LIMIT 1
		),
		recall_point AS (
			SELECT block_height, block_tx_index, asset_id
			FROM voting_right_txs
			WHERE asset_id = (SELECT asset_id FROM right_token) AND ownership_chain = $3 AND NOT void
			LIMIT 1
		)
		UPDATE voting_right_txs SET void = 't'
		FROM recall_point rp
		WHERE voting_right_txs.asset_id = rp.asset_id
		AND (voting_right_txs.block_height, voting_right_txs.block_tx_index) >= (rp.block_height, rp.block_tx_index)
	`
	res, err := pg.FromContext(ctx).Exec(ctx, q, out.Hash, out.Index, ownershipHash[:])
	if err != nil {
		return errors.Wrap(err, "voiding voting_right_txs")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "rows affected while voiding")
	}
	if affected < 1 {
		return fmt.Errorf("at least one voting right should be voided: %s, %d : %x", out.Hash, out.Index, ownershipHash[:])
	}
	return nil
}

// voidVotingRight takes the outpoint of a voting right claim
// and marks it as void.
func voidVotingRight(ctx context.Context, prev bc.Outpoint) error {
	const q = `
		UPDATE voting_right_txs SET void = 't'
		WHERE tx_hash = $1 AND index = $2
	`
	_, err := pg.FromContext(ctx).Exec(ctx, q, prev.Hash, prev.Index)
	return errors.Wrap(err, "voiding voting_right_txs")
}

type votingRightsQuery struct {
	accountID string
	outpoint  *bc.Outpoint
	assetID   *bc.AssetID

	cursor *cursor
	limit  int
}

func (q votingRightsQuery) Limit() string {
	if q.limit == 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d", q.limit)
}

func (q votingRightsQuery) Where() (string, []interface{}) {
	var (
		whereClause string
		values      []interface{}
		param       int = 1
	)

	if q.accountID != "" {
		whereClause = fmt.Sprintf("%s AND vr.account_id = $%d\n", whereClause, param)
		values = append(values, q.accountID)
		param++
	}
	if q.outpoint != nil {
		whereClause = fmt.Sprintf("%s AND vr.tx_hash = $%d AND vr.index = $%d\n", whereClause, param, param+1)
		values = append(values, q.outpoint.Hash, q.outpoint.Index)
		param += 2
	}
	if q.assetID != nil {
		whereClause = fmt.Sprintf("%s AND vr.asset_id = $%d\n", whereClause, param)
		values = append(values, *q.assetID)
		param++
	}
	if q.cursor != nil {
		whereClause = fmt.Sprintf("%s AND (vr.block_height, vr.block_tx_index) > ($%d, $%d)\n", whereClause, param, param+1)
		values = append(values, q.cursor.prevBlockHeight, q.cursor.prevBlockPos)
		param += 2
	}
	whereClause = fmt.Sprintf("%s AND vr.void = 'f'\n", whereClause)
	return whereClause, values
}

// FindRightsForAccount returns all voting rights belonging to the provided account.
func FindRightsForAccount(ctx context.Context, accountID string, prev string, limit int) ([]*RightWithUTXO, string, error) {
	// Since the sort criteria is composite, the cursor is composite.
	var (
		prevBlockHeight uint64
		prevBlockPos    int
		cur             *cursor
	)
	_, err := fmt.Sscanf(prev, "%d-%d", &prevBlockHeight, &prevBlockPos)

	// ignore malformed cursors
	if err == nil {
		cur = &cursor{
			prevBlockHeight: prevBlockHeight,
			prevBlockPos:    prevBlockPos,
		}
	}

	return findVotingRights(ctx, votingRightsQuery{
		accountID: accountID,
		cursor:    cur,
		limit:     limit,
	})
}

// FindRightForOutpoint returns the voting right with the provided tx outpoint.
func FindRightForOutpoint(ctx context.Context, out bc.Outpoint) (*RightWithUTXO, error) {
	rights, _, err := findVotingRights(ctx, votingRightsQuery{outpoint: &out})
	if err != nil {
		return nil, err
	}
	if len(rights) != 1 {
		return nil, fmt.Errorf("expected 1 right, found %d", len(rights))
	}
	return rights[0], nil
}

// FindRightsForAsset return all non-void claims to the voting right
// token with the provided asset ID. The resulting voting rights will
// be sorted chronologically (by block_height, block_tx_index). Effectively,
// this function returns the entire active chain of ownership for the
// voting right token.
func FindRightsForAsset(ctx context.Context, assetID bc.AssetID) ([]*RightWithUTXO, error) {
	rights, _, err := findVotingRights(ctx, votingRightsQuery{assetID: &assetID})
	if err != nil {
		return nil, err
	}
	return rights, nil
}

func findVotingRights(ctx context.Context, q votingRightsQuery) ([]*RightWithUTXO, string, error) {
	var (
		cur     cursor
		results []*RightWithUTXO
	)

	const sqlQ = `
		SELECT
			u.tx_hash AS utxo_hash,
			u.index   AS utxo_index,
			vr.tx_hash,
			vr.index,
			vr.block_height,
			vr.block_tx_index,
			vr.asset_id,
			vr.account_id,
			vr.holder,
			vr.deadline,
			vr.delegatable,
			vr.ownership_chain,
			vr.admin_script
		FROM voting_right_txs vr
		INNER JOIN utxos u ON vr.asset_id = u.asset_id
		WHERE
			u.asset_id = vr.asset_id AND
			NOT EXISTS (SELECT 1 FROM pool_inputs pi WHERE pi.tx_hash = u.tx_hash AND pi.index = u.index)
	`
	whereSQL, values := q.Where()
	queryStr := fmt.Sprintf("%s%s ORDER BY vr.block_height ASC, vr.block_tx_index ASC%s", sqlQ, whereSQL, q.Limit())
	rows, err := pg.Query(ctx, queryStr, values...)
	if err != nil {
		return nil, "", errors.Wrap(err, "query")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			right          RightWithUTXO
			ownershipChain []byte
		)

		err = rows.Scan(
			&right.UTXO.Hash, &right.UTXO.Index,
			&right.Outpoint.Hash, &right.Outpoint.Index,
			&right.BlockHeight, &right.BlockTxIndex,
			&right.AssetID, &right.AccountID,
			&right.HolderScript, &right.Deadline, &right.Delegatable, &ownershipChain,
			&right.AdminScript)
		if err != nil {
			return nil, "", errors.Wrap(err, "scanning RightWithUTXO")
		}
		copy(right.OwnershipChain[:], ownershipChain)
		results = append(results, &right)
		cur = cursor{
			prevBlockHeight: right.BlockHeight,
			prevBlockPos:    right.BlockTxIndex,
		}
	}

	if err = rows.Err(); err != nil {
		return nil, "", errors.Wrap(err, "end scan")
	}
	return results, cur.String(), nil
}
