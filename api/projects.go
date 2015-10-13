package api

import (
	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/net/http/authn"
)

// GET /v3/projects/:projID
func getApplication(ctx context.Context, projID string) (*appdb.Application, error) {
	if err := projectAuthz(ctx, projID); err != nil {
		return nil, err
	}
	return appdb.GetApplication(ctx, projID)
}

// GET /v3/projects
func listApplications(ctx context.Context) ([]*appdb.Application, error) {
	uid := authn.GetAuthID(ctx)
	return appdb.ListApplications(ctx, uid)
}

// POST /v3/projects
func createApplication(ctx context.Context, in struct{ Name string }) (*appdb.Application, error) {
	uid := authn.GetAuthID(ctx)
	return appdb.CreateApplication(ctx, in.Name, uid)
}

// PUT /v3/projects/:projID
func updateApplication(ctx context.Context, aid string, in struct{ Name string }) error {
	if err := projectAdminAuthz(ctx, aid); err != nil {
		return err
	}
	return appdb.UpdateApplication(ctx, aid, in.Name)
}

// GET /v3/projects/:projID/members
func listMembers(ctx context.Context, projID string) (interface{}, error) {
	if err := projectAuthz(ctx, projID); err != nil {
		return nil, err
	}
	return appdb.ListMembers(ctx, projID)
}

// POST /v3/projects/:projID/members
func addMember(ctx context.Context, aid string, in struct{ Email, Role string }) error {
	if err := projectAdminAuthz(ctx, aid); err != nil {
		return err
	}
	user, err := appdb.GetUserByEmail(ctx, in.Email)
	if err != nil {
		return err
	}

	return appdb.AddMember(ctx, aid, user.ID, in.Role)
}

// PUT /v3/projects/:projID/members/:userID
func updateMember(ctx context.Context, aid, memberID string, in struct{ Role string }) error {
	if err := projectAdminAuthz(ctx, aid); err != nil {
		return err
	}
	return appdb.UpdateMember(ctx, aid, memberID, in.Role)
}

// DELETE /v3/projects/:projID/members/:userID
func removeMember(ctx context.Context, projID, userID string) error {
	if err := projectAdminAuthz(ctx, projID); err != nil {
		return err
	}
	return appdb.RemoveMember(ctx, projID, userID)
}
