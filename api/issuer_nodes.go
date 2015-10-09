package api

import (
	"time"

	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/api/asset"
	"chain/database/pg"
	"chain/fedchain-sandbox/hdkey"
	"chain/metrics"
	"chain/net/http/httpjson"
)

// POST /v3/projects/:projID/issuer-nodes
func createAssetGroup(ctx context.Context, appID string, agReq struct {
	Label string
	XPubs []string
}) (interface{}, error) {
	var keys []*hdkey.XKey
	for _, xpub := range agReq.XPubs {
		key, err := hdkey.NewXKey(xpub)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback()

	agID, err := appdb.CreateAssetGroup(ctx, appID, agReq.Label, keys)
	if err != nil {
		return nil, err
	}

	err = dbtx.Commit()
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"id":                  agID,
		"label":               agReq.Label,
		"block_chain":         "sandbox",
		"keys":                keys,
		"signatures_required": 1,
	}
	return ret, nil
}

// GET /v3/issuer-nodes/:inodeID/assets
func listAssets(ctx context.Context, groupID string) (interface{}, error) {
	prev, limit, err := getPageData(ctx, defAssetPageSize)
	if err != nil {
		return nil, err
	}

	assets, last, err := appdb.ListAssets(ctx, groupID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":   last,
		"assets": httpjson.Array(assets),
	}
	return ret, nil
}

// POST /v3/issuer-nodes/:inodeID/assets
func createAsset(ctx context.Context, groupID string, in struct{ Label string }) (interface{}, error) {
	defer metrics.RecordElapsed(time.Now())
	asset, err := asset.Create(ctx, groupID, in.Label)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"id":             asset.Hash.String(),
		"issuer_node_id": asset.GroupID,
		"label":          asset.Label,
	}
	return ret, nil
}
