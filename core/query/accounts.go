package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"chain/core/query/chql"
	"chain/errors"
)

// SaveAnnotatedAccount saves an annotated account to the query indexes.
func (i *Indexer) SaveAnnotatedAccount(ctx context.Context, accountID string, account map[string]interface{}) error {
	b, err := json.Marshal(account)
	if err != nil {
		return errors.Wrap(err)
	}

	const q = `
		INSERT INTO annotated_accounts (id, data) VALUES($1, $2)
		ON CONFLICT (id) DO UPDATE SET data = $2
	`
	_, err = i.db.Exec(ctx, q, accountID, b)
	return errors.Wrap(err, "saving annotated account")
}

// Accounts queries the blockchain for accounts matching the query `q`.
func (i *Indexer) Accounts(ctx context.Context, q chql.Query, vals []interface{}, cur string, limit int) ([]map[string]interface{}, string, error) {
	if len(vals) != q.Parameters {
		return nil, "", ErrParameterCountMismatch
	}
	expr, err := chql.AsSQL(q, "data", vals)
	if err != nil {
		return nil, "", errors.Wrap(err, "converting to SQL")
	}

	queryStr, queryArgs := constructAccountsQuery(expr, cur, limit)
	rows, err := i.db.Query(ctx, queryStr, queryArgs...)
	if err != nil {
		return nil, "", errors.Wrap(err, "executing acc query")
	}
	defer rows.Close()

	accounts := make([]map[string]interface{}, 0, limit)
	for rows.Next() {
		var accID string
		var rawAccount []byte
		err := rows.Scan(&accID, &rawAccount)
		if err != nil {
			return nil, "", errors.Wrap(err, "scanning account row")
		}

		var account map[string]interface{}
		if len(rawAccount) > 0 {
			err = json.Unmarshal(rawAccount, &account)
			if err != nil {
				return nil, "", err
			}
		}

		cur = accID
		accounts = append(accounts, account)
	}
	return accounts, cur, errors.Wrap(rows.Err())
}

func constructAccountsQuery(expr chql.SQLExpr, cur string, limit int) (string, []interface{}) {
	var buf bytes.Buffer
	var vals []interface{}

	buf.WriteString("SELECT id, data FROM annotated_accounts")
	buf.WriteString(" WHERE ")

	// add filter conditions
	if len(expr.SQL) > 0 {
		vals = append(vals, expr.Values...)
		buf.WriteString("(")
		buf.WriteString(expr.SQL)
		buf.WriteString(") AND ")
	}

	// add cursor conditions
	buf.WriteString(fmt.Sprintf("id > $%d ", len(vals)+1))
	vals = append(vals, string(cur))

	buf.WriteString("ORDER BY id ASC ")
	buf.WriteString("LIMIT " + strconv.Itoa(limit))
	return buf.String(), vals
}
