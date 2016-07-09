package core

import (
	"time"

	"golang.org/x/net/context"

	"chain/core/appdb"
	"chain/net/http/authn"
)

// POST /v3/login
func login(ctx context.Context) (*appdb.AuthToken, error) {
	uid := authn.GetAuthID(ctx)
	expiresAt := time.Now().UTC().Add(sessionTokenLifetime)
	return appdb.CreateAuthToken(ctx, uid, "session", &expiresAt)
}

// GET /v3/user
func getAuthdUser(ctx context.Context) (*appdb.User, error) {
	uid := authn.GetAuthID(ctx)
	return appdb.GetUser(ctx, uid)
}

// POST /v3/user/email
func updateUserEmail(ctx context.Context, in struct{ Email, Password string }) error {
	uid := authn.GetAuthID(ctx)
	return appdb.UpdateUserEmail(ctx, uid, in.Password, in.Email)
}

// POST /v3/user/password
func updateUserPassword(ctx context.Context, in struct{ Current, New string }) error {
	uid := authn.GetAuthID(ctx)
	return appdb.UpdateUserPassword(ctx, uid, in.Current, in.New)
}

// PUT /v3/user/role
func updateUserRole(ctx context.Context, userID string, in struct{ Role string }) error {
	if err := adminAuthz(ctx); err != nil {
		return err
	}
	return appdb.UpdateUserRole(ctx, userID, in.Role)
}

// GET /v3/users
func listUsers(ctx context.Context) (interface{}, error) {
	return appdb.ListUsers(ctx)
}

// POST /nouser/password-reset/start
func startPasswordReset(ctx context.Context, in struct{ Email string }) (interface{}, error) {
	secret, err := appdb.StartPasswordReset(ctx, in.Email, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"secret": secret}, nil
}

// POST /nouser/password-reset/check
func checkPasswordReset(ctx context.Context, in struct{ Email, Secret string }) error {
	return appdb.CheckPasswordReset(ctx, in.Email, in.Secret)
}

// POST /nouser/password-reset/finish
func finishPasswordReset(ctx context.Context, in struct{ Email, Secret, Password string }) error {
	return appdb.FinishPasswordReset(ctx, in.Email, in.Secret, in.Password)
}
