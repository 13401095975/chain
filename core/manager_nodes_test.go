package core

import (
	"testing"

	"chain/core/asset"
	"chain/core/asset/assettest"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/net/http/authn"
)

func TestCreateManagerNodeBadXPub(t *testing.T) {
	ctx := pgtest.NewContext(t)

	uid := assettest.CreateUserFixture(ctx, t, "foo@bar.com", "abracadabra")
	proj0 := assettest.CreateProjectFixture(ctx, t, uid, "x")

	ctx = authn.NewContext(ctx, uid)

	req := map[string]interface{}{
		"label":               "node",
		"signatures_required": 1,
		"keys":                []*asset.CreateNodeKeySpec{{Type: "node", XPub: "badxpub"}},
	}

	_, err := createManagerNode(ctx, proj0, req)
	if got := errors.Root(err); got != asset.ErrBadKeySpec {
		t.Fatalf("err = %v want %v", got, asset.ErrBadKeySpec)
	}
}

func TestCreateManagerNode(t *testing.T) {
	ctx := pgtest.NewContext(t)

	uid := assettest.CreateUserFixture(ctx, t, "foo@bar.com", "abracadabra")
	proj0 := assettest.CreateProjectFixture(ctx, t, uid, "x")

	ctx = authn.NewContext(ctx, uid)

	req := map[string]interface{}{
		"label":               "node",
		"keys":                []*asset.CreateNodeKeySpec{{Type: "node", Generate: true}},
		"signatures_required": 1,
	}

	_, err := createManagerNode(ctx, proj0, req)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	var count int
	var checkQ = `SELECT COUNT(*) FROM manager_nodes`
	err = pg.QueryRow(ctx, checkQ).Scan(&count)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if count != 1 {
		t.Fatalf("checking results, want=1, got=%d", count)
	}
}

func TestCreateManagerNodeDeprecated(t *testing.T) {
	ctx := pgtest.NewContext(t)

	uid := assettest.CreateUserFixture(ctx, t, "foo@bar.com", "abracadabra")
	proj0 := assettest.CreateProjectFixture(ctx, t, uid, "x")

	ctx = authn.NewContext(ctx, uid)

	req := map[string]interface{}{
		"label":        "deprecated node",
		"generate_key": true,
	}

	_, err := createManagerNode(ctx, proj0, req)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	var count int
	var checkQ = `SELECT COUNT(*) FROM manager_nodes`
	err = pg.QueryRow(ctx, checkQ).Scan(&count)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if count != 1 {
		t.Fatalf("checking results, want=1, got=%d", count)
	}
}
