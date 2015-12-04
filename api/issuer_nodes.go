package api

import (
	"time"

	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/api/asset"
	"chain/database/pg"
	"chain/metrics"
	"chain/net/http/httpjson"
)

// POST /v3/projects/:projID/issuer-nodes
func createIssuerNode(ctx context.Context, projID string, req *asset.CreateNodeReq) (interface{}, error) {
	if err := projectAuthz(ctx, projID); err != nil {
		return nil, err
	}

	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback()

	issuerNode, err := asset.CreateNode(ctx, asset.IssuerNode, projID, req)
	if err != nil {
		return nil, err
	}

	err = dbtx.Commit()
	if err != nil {
		return nil, err
	}

	return issuerNode, nil
}

// GET /v3/projects/:projID/issuer-nodes
func listIssuerNodes(ctx context.Context, projID string) (interface{}, error) {
	if err := projectAuthz(ctx, projID); err != nil {
		return nil, err
	}
	return appdb.ListIssuerNodes(ctx, projID)
}

// GET /v3/issuer-nodes/:inodeID
func getIssuerNode(ctx context.Context, inodeID string) (interface{}, error) {
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return nil, err
	}
	return appdb.GetIssuerNode(ctx, inodeID)
}

// PUT /v3/issuer-nodes/:inodeID
func updateIssuerNode(ctx context.Context, inodeID string, in struct{ Label *string }) error {
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return err
	}
	return appdb.UpdateIssuerNode(ctx, inodeID, in.Label)
}

// DELETE /v3/issuer-nodes/:inodeID
func deleteIssuerNode(ctx context.Context, inodeID string) error {
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return err
	}
	return appdb.DeleteIssuerNode(ctx, inodeID)
}

// GET /v3/issuer-nodes/:inodeID/assets
func listAssets(ctx context.Context, inodeID string) (interface{}, error) {
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defAssetPageSize)
	if err != nil {
		return nil, err
	}

	assets, last, err := appdb.ListAssets(ctx, inodeID, prev, limit)
	if err != nil {
		return nil, err
	}

	// !!!HACK(jeffomatic) - do not split confirmed/total issuances until we enable
	// automatic block generation
	var res []map[string]interface{}
	for _, a := range assets {
		res = append(res, map[string]interface{}{
			"id":          a.ID,
			"label":       a.Label,
			"circulation": a.Circulation.Total,
		})
	}

	ret := map[string]interface{}{
		"last":   last,
		"assets": httpjson.Array(res),
	}
	return ret, nil
}

// POST /v3/issuer-nodes/:inodeID/assets
func createAsset(ctx context.Context, inodeID string, in struct {
	Label      string
	Definition map[string]interface{}
}) (interface{}, error) {
	defer metrics.RecordElapsed(time.Now())
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return nil, err
	}

	if in.Definition == nil {
		in.Definition = make(map[string]interface{})
	}

	ast, err := asset.Create(ctx, inodeID, in.Label, in.Definition)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"id":             ast.Hash.String(),
		"issuer_node_id": ast.IssuerNodeID,
		"label":          ast.Label,
	}
	return ret, nil
}

// GET /v3/assets/:assetID
func getAsset(ctx context.Context, assetID string) (interface{}, error) {
	if err := assetAuthz(ctx, assetID); err != nil {
		return nil, err
	}

	// !!!HACK(jeffomatic) - do not split confirmed/total issuances until we enable
	// automatic block generation
	asset, err := appdb.GetAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":          asset.ID,
		"label":       asset.Label,
		"circulation": asset.Circulation.Total,
	}, nil
}

// PUT /v3/assets/:assetID
func updateAsset(ctx context.Context, assetID string, in struct{ Label *string }) error {
	if err := assetAuthz(ctx, assetID); err != nil {
		return err
	}
	return appdb.UpdateAsset(ctx, assetID, in.Label)
}

// DELETE /v3/assets/:assetID
func deleteAsset(ctx context.Context, assetID string) error {
	if err := assetAuthz(ctx, assetID); err != nil {
		return err
	}
	return appdb.DeleteAsset(ctx, assetID)
}

// GET /v3/issuer-nodes/:inodeID/activity
func getIssuerNodeActivity(ctx context.Context, inodeID string) (interface{}, error) {
	if err := issuerAuthz(ctx, inodeID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defActivityPageSize)
	if err != nil {
		return nil, err
	}

	activity, last, err := appdb.IssuerNodeActivity(ctx, inodeID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":       last,
		"activities": httpjson.Array(activity),
	}
	return ret, nil
}

// GET /v3/assets/:assetID/activity
func getAssetActivity(ctx context.Context, assetID string) (interface{}, error) {
	if err := assetAuthz(ctx, assetID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defActivityPageSize)
	if err != nil {
		return nil, err
	}

	activity, last, err := appdb.AssetActivity(ctx, assetID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":       last,
		"activities": httpjson.Array(activity),
	}
	return ret, nil
}
