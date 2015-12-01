package appdb

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/log"
	"chain/metrics"
)

// Account represents an indexed namespace inside of a manager node
type Account struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Index []uint32 `json:"account_index"`
}

// CreateAccount inserts an account database record
// for the given manager node,
// and returns the new Account.
func CreateAccount(ctx context.Context, managerNodeID, label string) (*Account, error) {
	defer metrics.RecordElapsed(time.Now())
	if label == "" {
		return nil, ErrBadLabel
	}

	account := &Account{Label: label}

	const attempts = 3
	for i := 0; i < attempts; i++ {
		const q = `
			WITH incr AS (
				UPDATE manager_nodes
				SET
					accounts_count=accounts_count+1,
					next_account_index=next_account_index+1
				WHERE id=$1
				RETURNING (next_account_index - 1)
			)
			INSERT INTO accounts (manager_node_id, key_index, label)
			VALUES ($1, (TABLE incr), $2)
			RETURNING id, key_index(key_index)
		`
		err := pg.FromContext(ctx).QueryRow(q, managerNodeID, label).Scan(
			&account.ID,
			(*pg.Uint32s)(&account.Index),
		)
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code.Name() == "unique_violation" {
			// There was an (expected) unique index conflict.
			// It is safe to try again.
			// This happens when there is contention incrementing
			// the account index.
			log.Write(ctx, "attempt", i, "error", err)
			if i == attempts-1 {
				return nil, err
			}
			continue
		} else if err != nil {
			return nil, err
		}
		break
	}

	return account, nil
}

// ListAccounts returns a list of accounts contained in the given manager node.
func ListAccounts(ctx context.Context, managerNodeID string, prev string, limit int) ([]*Account, string, error) {
	q := `
		SELECT id, label, key_index(key_index)
		FROM accounts
		WHERE manager_node_id = $1 AND ($2='' OR id<$2)
		ORDER BY id DESC LIMIT $3
	`
	rows, err := pg.FromContext(ctx).Query(q, managerNodeID, prev, limit)
	if err != nil {
		return nil, "", errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var (
		accounts []*Account
		last     string
	)
	for rows.Next() {
		a := new(Account)
		err = rows.Scan(
			&a.ID,
			&a.Label,
			(*pg.Uint32s)(&a.Index),
		)
		if err != nil {
			return nil, "", errors.Wrap(err, "row scan")
		}
		accounts = append(accounts, a)
		last = a.ID
	}

	if err = rows.Err(); err != nil {
		return nil, "", errors.Wrap(err, "end row scan loop")
	}

	return accounts, last, err
}

// GetAccount returns a single account.
func GetAccount(ctx context.Context, accountID string) (*Account, error) {
	q := `
		SELECT label, key_index(key_index)
		FROM accounts
		WHERE id = $1
	`
	a := &Account{ID: accountID}
	err := pg.FromContext(ctx).QueryRow(q, accountID).Scan(&a.Label, (*pg.Uint32s)(&a.Index))
	if err == sql.ErrNoRows {
		return nil, pg.ErrUserInputNotFound
	}
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}

	return a, nil
}

// UpdateAccount updates the label of an account.
func UpdateAccount(ctx context.Context, accountID string, label *string) error {
	if label == nil {
		return nil
	}
	const q = `UPDATE accounts SET label = $2 WHERE id = $1`
	db := pg.FromContext(ctx)
	_, err := db.Exec(q, accountID, *label)
	return errors.Wrap(err, "update query")
}

// DeleteAccount deletes the account but only if there is no activity
// and there are no addresses associated with it (enforced by ON
// DELETE NO ACTION).
func DeleteAccount(ctx context.Context, accountID string) error {
	const q = `DELETE FROM accounts WHERE id = $1`
	db := pg.FromContext(ctx)
	result, err := db.Exec(q, accountID)
	if err != nil {
		if pg.IsForeignKeyViolation(err) {
			return errors.WithDetailf(ErrCannotDelete, "account ID %v", accountID)
		}
		return errors.Wrap(err, "delete query")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "delete query")
	}
	if rowsAffected == 0 {
		return errors.WithDetailf(pg.ErrUserInputNotFound, "account ID %v", accountID)
	}
	return nil
}
