package txdb

import (
	"golang.org/x/net/context"

	"chain/cos/bc"
	"chain/cos/patricia"
	"chain/database/pg"
	"chain/database/sql"
	"chain/errors"
)

func stateTree(ctx context.Context, db pg.DB) (*patricia.Tree, error) {
	const q = `
		SELECT key, hash, leaf, value FROM state_trees ORDER BY LENGTH(key) ASC
	`
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	defer rows.Close()

	var nodes []*patricia.Node
	for rows.Next() {
		var (
			keyStr string
			hash   bc.Hash
			isLeaf bool
			value  []byte
		)
		err := rows.Scan(&keyStr, &hash, &isLeaf, &value)
		if err != nil {
			return nil, errors.Wrap(err)
		}

		var v patricia.Valuer
		if len(value) == 0 {
			v = patricia.HashValuer(hash)
		} else {
			v = patricia.BytesValuer(value)
		}
		nodes = append(nodes, patricia.NewNode(strTreeKey(keyStr), v, isLeaf))
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err)
	}

	return patricia.NewTree(nodes), nil
}

func writeStateTree(ctx context.Context, dbtx *sql.Tx, tree *patricia.Tree) error {
	deletes, inserts, updates := tree.Delta()

	var keys []string
	for _, d := range deletes {
		keys = append(keys, treeKeyStr(d))
	}
	const deleteQ = `
		WITH dels AS (
			SELECT unnest($1::text[]) AS key
		)
		DELETE FROM state_trees
		WHERE key IN (TABLE dels)
	`
	_, err := dbtx.Exec(ctx, deleteQ, pg.Strings(keys))
	if err != nil {
		return errors.Wrap(err)
	}

	const insertQ = `
		WITH nodes AS (
			SELECT * FROM unnest($1::text[], $2::text[], $3::bool[], $4::bytea[])
				AS t(key, hash, leaf, value)
		)
		INSERT INTO state_trees (key, hash, leaf, value)
		SELECT * FROM Nodes
	`
	var (
		hashes []string
		leafs  []bool
		values [][]byte
	)
	keys = nil
	for _, n := range inserts {
		keys = append(keys, treeKeyStr(n.Key()))
		hashes = append(hashes, n.Hash().String())
		leafs = append(leafs, n.IsLeaf())

		// Values are omitted if the node is only a hash.
		if n.Value().IsHash {
			values = append(values, nil)
		} else {
			values = append(values, n.Value().Bytes)
		}
	}
	_, err = dbtx.Exec(
		ctx,
		insertQ,
		pg.Strings(keys),
		pg.Strings(hashes),
		pg.Bools(leafs),
		pg.Byteas(values),
	)
	if err != nil {
		return errors.Wrap(err)
	}

	const updateQ = `
		WITH nodes AS (
			SELECT * FROM unnest($1::text[], $2::text[], $3::bool[], $4::bytea[])
				AS t(key, hash, leaf, value)
		)
		UPDATE state_trees s
		SET hash=n.hash, leaf=n.leaf, value=n.value
		FROM nodes n
		WHERE s.key = n.key
	`
	keys, hashes, leafs, values = nil, nil, nil, nil
	for _, n := range updates {
		keys = append(keys, treeKeyStr(n.Key()))
		hashes = append(hashes, n.Hash().String())
		leafs = append(leafs, n.IsLeaf())

		// Values are omitted if the node is only a hash.
		if n.Value().IsHash {
			values = append(values, nil)
		} else {
			values = append(values, n.Value().Bytes)
		}
	}
	_, err = dbtx.Exec(
		ctx,
		updateQ,
		pg.Strings(keys),
		pg.Strings(hashes),
		pg.Bools(leafs),
		pg.Byteas(values),
	)
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func treeKeyStr(key []uint8) string {
	str := ""
	for _, p := range key {
		if p == 0 {
			str += "0"
		} else {
			str += "1"
		}
	}
	return str
}

func strTreeKey(str string) []uint8 {
	key := make([]uint8, len(str))
	for i := 0; i < len(str); i++ {
		if str[i] == '0' {
			key[i] = 0
		} else {
			key[i] = 1
		}
	}
	return key
}
