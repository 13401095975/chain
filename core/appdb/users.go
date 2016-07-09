package appdb

import (
	"database/sql"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/errors"
	"chain/net/http/authn"
)

const pwResetLiftime = time.Hour * 24

var passwordBcryptCost = 10

// Errors returned by CreateUser.
// May be wrapped using package chain/errors.
var (
	ErrBadEmail          = errors.New("bad email")
	ErrBadPassword       = errors.New("bad password")
	ErrPasswordCheck     = errors.New("password does not match")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrNoUserForEmail    = errors.New("no user for that email")
)

// User represents a single user. Instances should be safe to deliver in API
// responses.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// CreateUser creates a new row in the users table corresponding to the provided
// credentials.
func CreateUser(ctx context.Context, email, password, role string) (*User, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	if err := validatePassword(password); err != nil {
		return nil, err
	}

	if err := validateRole(role); err != nil {
		return nil, err
	}

	phash, err := bcrypt.GenerateFromPassword([]byte(password), passwordBcryptCost)
	if err != nil {
		return nil, errors.Wrap(err, "password hash")
	}

	q := `
		INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3)
		RETURNING id
	`
	var id string
	err = pg.QueryRow(ctx, q, email, phash, role).Scan(&id)
	if pg.IsUniqueViolation(err) {
		return nil, ErrUserAlreadyExists
	}
	if err != nil {
		return nil, errors.Wrap(err, "insert query")
	}

	return &User{id, email, role}, nil
}

// AuthenticateUserCreds takes an email and password and returns a user ID
// corresponding to those credentials. If the credentials are invalid,
// authn.ErrNotAuthenticated is returned.
func AuthenticateUserCreds(ctx context.Context, email, password string) (userID string, err error) {
	email = strings.TrimSpace(email)

	var (
		id    string
		phash []byte

		q = `SELECT id, password_hash FROM users WHERE lower(email) = lower($1)`
	)
	err = pg.QueryRow(ctx, q, email).Scan(&id, &phash)
	if err == sql.ErrNoRows {
		return "", authn.ErrNotAuthenticated
	}
	if err != nil {
		return "", errors.Wrap(err, "select user")
	}

	if bcrypt.CompareHashAndPassword(phash, []byte(password)) != nil {
		return "", authn.ErrNotAuthenticated
	}

	return id, nil
}

// GetUser returns a User corresponding to the given ID. If no user is found,
// it will return an error with pg.ErrUserInputNotFound as its root.
func GetUser(ctx context.Context, id string) (*User, error) {
	var (
		q = `SELECT email, role FROM users WHERE id = $1`
		u = &User{ID: id}
	)

	err := pg.QueryRow(ctx, q, id).Scan(&u.Email, &u.Role)
	if err == sql.ErrNoRows {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "ID: %v", id)
	}
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}

	return u, nil
}

// GetUserByEmail returns a User corresponding to the given email. It is not
// sensitive to the case of the provided email address. If no user is found,
// it will return an error with pg.ErrUserInputNotFound as its root.
func GetUserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.TrimSpace(email)

	var (
		q = `SELECT id, email, role FROM users WHERE lower(email) = lower($1)`
		u = new(User)
	)

	err := pg.QueryRow(ctx, q, email).Scan(&u.ID, &u.Email, &u.Role)
	if err == sql.ErrNoRows {
		return nil, errors.WithDetailf(pg.ErrUserInputNotFound, "email: %v", email)
	}
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}

	return u, nil
}

// UpdateUserEmail modifies a user's email address. If the provided email
// address is not a valid email address, or the email address is in use by
// another user, ErrBadEmail is returned. As an extra layer of security, it will
// check the provided password; if the password is incorrect,
// ErrPasswordCheck is returned.
func UpdateUserEmail(ctx context.Context, id, password, email string) error {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return err
	}

	if err := checkPassword(ctx, id, password); err != nil {
		return err
	}

	q := `UPDATE users SET email = $1 WHERE id = $2`
	_, err := pg.Exec(ctx, q, email, id)
	if pg.IsUniqueViolation(err) {
		return errors.Wrap(ErrBadEmail, "email address already in use")
	}
	if err != nil {
		return errors.Wrap(err, "update query")
	}

	return nil
}

// UpdateUserPassword modifies a user's password. If the new password is not
// valid, ErrBadPassword is returned. As an extra layer of security, it will
// verify the current password; if the password is incorrect, ErrPasswordCheck
// is returned.
func UpdateUserPassword(ctx context.Context, id, password, newpass string) error {
	if err := validatePassword(newpass); err != nil {
		return err
	}

	if err := checkPassword(ctx, id, password); err != nil {
		return err
	}

	phash, err := bcrypt.GenerateFromPassword([]byte(newpass), passwordBcryptCost)
	if err != nil {
		return errors.Wrap(err, "password hash")
	}

	q := `UPDATE users SET password_hash = $1 WHERE id = $2`
	_, err = pg.Exec(ctx, q, phash, id)
	if err != nil {
		return errors.Wrap(err, "update query")
	}

	return nil
}

// UpdateUserRole changes the role of a user. If the role is not valid,
// ErrBadRole will be returned. If the user is not a part of the core
// an error with pg.ErrUserInputNotFound as its root will be returned.
func UpdateUserRole(ctx context.Context, userID, role string) error {
	if err := validateRole(role); err != nil {
		return err
	}

	q := `
		UPDATE users SET role = $1
		WHERE id = $2
		RETURNING 1
	`
	err := pg.QueryRow(ctx, q, role, userID).Scan(new(int))
	if err == sql.ErrNoRows {
		return errors.WithDetailf(
			pg.ErrUserInputNotFound,
			"user id: %v", userID,
		)
	}
	if err != nil {
		return errors.Wrap(err, "update query")
	}
	return nil
}

// ListUsers returns a list of users on the core.
func ListUsers(ctx context.Context) ([]*User, error) {
	q := `
		SELECT id, email, role
		FROM users
		ORDER BY email
	`
	rows, err := pg.Query(ctx, q)
	if err != nil {
		return nil, errors.Wrap(err, "select query")
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := new(User)
		err := rows.Scan(&u.ID, &u.Email, &u.Role)
		if err != nil {
			return nil, errors.Wrap(err, "row scan")
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "end row scan loop")
	}

	return users, nil
}

// StartPasswordReset generates an expiring secret token that can be used to
// set a user's password with no other credentials. For any given user, only
// the most recent password reset token will be effective.
//
// The password reset secret must be handled carefully, since it is equivalent
// to an auth token for the corresponding user. It should only be sent to
// trusted clients, such as internal services or the user themselves.
//
// If the provided email does not correspond to an actual email account,
// ErrNoUserForEmail is returned. Since this may leak information about
// the existence of registered accounts, this error should be visible to trusted
// clients. It should not be propagated all the way to the end user.
func StartPasswordReset(ctx context.Context, email string, now time.Time) (string, error) {
	email = strings.TrimSpace(email)

	secret, hash, err := generateSecret()
	if err != nil {
		return "", errors.Wrap(err, "generate password reset secret")
	}

	exp := now.Add(pwResetLiftime)
	q := `
		UPDATE users
		SET pwreset_secret_hash = $1, pwreset_expires_at = $2
		WHERE lower(email) = lower($3)
		RETURNING 1
	`
	err = pg.QueryRow(ctx, q, hash, exp, email).Scan(new(int))
	if err == sql.ErrNoRows {
		return "", ErrNoUserForEmail
	}
	if err != nil {
		return "", errors.Wrap(err, "update query")
	}

	return secret, nil
}

// CheckPasswordReset returns pg.ErrUserInputNotFound if there is no unexpired
// password reset corresponding to the given credentials.
func CheckPasswordReset(ctx context.Context, email, secret string) error {
	email = strings.TrimSpace(email)

	selectq := `
		SELECT pwreset_secret_hash
		FROM users
		WHERE lower(email) = lower($1)
			AND pwreset_secret_hash IS NOT NULL
			AND pwreset_expires_at IS NOT NULL
			AND pwreset_expires_at > now()
	`
	var secHash []byte
	err := pg.QueryRow(ctx, selectq, email).Scan(&secHash)
	if err == sql.ErrNoRows {
		return pg.ErrUserInputNotFound
	}
	if err != nil {
		return errors.Wrap(err, "select query")
	}

	if bcrypt.CompareHashAndPassword(secHash, []byte(secret)) != nil {
		// Treat a mismatching secret as if StartPasswordReset was never called.
		return pg.ErrUserInputNotFound
	}

	return nil
}

// FinishPasswordReset updates a user password using a password reset secret as
// a credential.
//
// If the new password is not valid, ErrBadPassword will be returned. If the
// the password reset has expired, or either the email or secret are not found,
// pg.ErrUserInputNotFound will be returned.
func FinishPasswordReset(ctx context.Context, email, secret, newpass string) error {
	email = strings.TrimSpace(email)

	if err := CheckPasswordReset(ctx, email, secret); err != nil {
		return err
	}

	if err := validatePassword(newpass); err != nil {
		return err
	}

	pwHash, err := bcrypt.GenerateFromPassword([]byte(newpass), passwordBcryptCost)
	if err != nil {
		return errors.Wrap(err, "hash password")
	}

	updateq := `
		UPDATE users
		SET
			password_hash = $1,
			pwreset_secret_hash = NULL,
			pwreset_expires_at = NULL
		WHERE lower(email) = lower($2)
	`
	_, err = pg.Exec(ctx, updateq, pwHash, email)
	if err != nil {
		return errors.Wrap(err, "update query")
	}

	return nil
}

func validateEmail(email string) error {
	switch {
	case len(email) > 255:
		return errors.WithDetail(ErrBadEmail, "too long")
	case !strings.Contains(email, "@"):
		return errors.WithDetail(ErrBadEmail, "no '@' symbol")
	case email != strings.TrimSpace(email):
		return errors.WithDetail(ErrBadEmail, "contains extra whitespace")
	}
	return nil
}

func validatePassword(password string) error {
	switch {
	case len(password) < 6:
		return errors.WithDetail(ErrBadPassword, "too short")
	case 255 < len(password):
		return errors.WithDetail(ErrBadPassword, "too long")
	}
	return nil
}

// validateRole checks whether the provided role is one of the valid roles,
// either "admin" or "developer". If the role is invalid, an error with
// ErrBadRole as its root is returned.
func validateRole(role string) error {
	if role != "admin" && role != "developer" {
		return errors.WithDetailf(ErrBadRole, "role: %v", role)
	}
	return nil
}

func checkPassword(ctx context.Context, id, password string) error {
	var (
		q     = `SELECT password_hash FROM users WHERE id = $1`
		phash []byte
	)
	err := pg.QueryRow(ctx, q, id).Scan(&phash)
	if err != nil {
		return errors.Wrap(err, "select query")
	}

	if bcrypt.CompareHashAndPassword(phash, []byte(password)) != nil {
		return ErrPasswordCheck
	}

	return nil
}
