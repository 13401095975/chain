package cursor

import (
	"context"
	"database/sql"

	"chain/database/pg"
	"chain/errors"
	"chain/net/http/httpjson"
)

type Cursor struct {
	ID     string  `json:"id,omitempty"`
	Alias  *string `json:"alias"`
	Filter string  `json:"filter,omitempty"`
	After  string  `json:"after,omitempty"`
	Order  string  `json:"order,omitempty"`
}

func Create(ctx context.Context, alias, filter, after string, clientToken *string) (*Cursor, error) {
	var ptrAlias *string
	if alias != "" {
		ptrAlias = &alias
	}

	cur := &Cursor{
		Alias:  ptrAlias,
		Filter: filter,
		After:  after,
	}

	return insertCursor(ctx, cur, clientToken)
}

// insertCursor adds the cursor to the database. If the cursor has a client token,
// and there already exists a cursor with that client token, insertCursor will
// lookup and return the existing cursor instead.
func insertCursor(ctx context.Context, cur *Cursor, clientToken *string) (*Cursor, error) {
	const q = `
		INSERT INTO txconsumers (alias, filter, after, is_ascending, client_token)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (client_token) DO NOTHING
		RETURNING id
	`

	var alias sql.NullString
	if cur.Alias != nil {
		alias = sql.NullString{Valid: true, String: *cur.Alias}
	}

	isAscending := cur.Order == "asc"

	err := pg.QueryRow(
		ctx, q, alias, cur.Filter, cur.After,
		isAscending, clientToken).Scan(&cur.ID)

	if pg.IsUniqueViolation(err) {
		return nil, errors.WithDetail(httpjson.ErrBadRequest, "non-unique alias")
	} else if err == sql.ErrNoRows && clientToken != nil {
		// There is already a cursor with the provided client
		// token. We should return the existing cursor
		cur, err = cursorByClientToken(ctx, *clientToken)
		if err != nil {
			return nil, errors.Wrap(err, "retrieving existing cursor")
		}
	} else if err != nil {
		return nil, err
	}

	return cur, nil
}

func cursorByClientToken(ctx context.Context, clientToken string) (*Cursor, error) {
	const q = `
		SELECT id, alias, filter, after, is_ascending
		FROM txconsumers
		WHERE client_token=$1
	`

	var (
		cur         Cursor
		alias       sql.NullString
		isAscending bool
	)
	err := pg.QueryRow(ctx, q, clientToken).Scan(&cur.ID, &alias, &cur.Filter, &cur.After, &isAscending)
	if err != nil {
		return nil, err
	}

	if isAscending {
		cur.Order = "asc"
	} else {
		cur.Order = "desc"
	}

	if alias.Valid {
		cur.Alias = &alias.String
	}

	return &cur, nil
}

func Find(ctx context.Context, id, alias string) (*Cursor, error) {
	where := ` WHERE `
	if id != "" {
		where += `id=$1`
	} else {
		where += `alias=$1`
		id = alias
	}

	q := `
		SELECT id, alias, filter, after, is_ascending
		FROM txconsumers
	` + where

	var (
		cur         Cursor
		sqlAlias    sql.NullString
		isAscending bool
	)

	err := pg.QueryRow(ctx, q, id).Scan(&cur.ID, &sqlAlias, &cur.Filter, &cur.After, &isAscending)
	if err != nil {
		return nil, err
	}

	if isAscending {
		cur.Order = "asc"
	} else {
		cur.Order = "desc"
	}

	if sqlAlias.Valid {
		cur.Alias = &sqlAlias.String
	}

	return &cur, nil
}

func Delete(ctx context.Context, id, alias string) error {
	where := ` WHERE `
	if id != "" {
		where += `id=$1`
	} else {
		where += `alias=$1`
		id = alias
	}

	q := `DELETE FROM txconsumers` + where

	res, err := pg.Exec(ctx, q, id)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return errors.WithDetailf(pg.ErrUserInputNotFound, "could not find and delete cursor with id/alias=%s", id)
	}

	return nil
}

func Update(ctx context.Context, id, alias, after, prev string) (*Cursor, error) {
	where := ` WHERE `
	if id != "" {
		where += `id=$2`
	} else {
		where += `alias=$2`
		id = alias
	}

	q := `
		UPDATE cursors SET after=$1
	` + where + ` AND after=$3`

	res, err := pg.Exec(ctx, q, after, id, prev)
	if err != nil {
		return nil, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}

	if affected == 0 {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "could not find cursor with id/alias=%s and prev=%s", id, prev)
	}

	return &Cursor{
		ID:    id,
		Alias: &alias,
		After: after,
	}, nil
}
