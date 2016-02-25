package api

import (
	"golang.org/x/net/context"

	"chain/api/explorer"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/net/http/httpjson"
)

func listBlocks(ctx context.Context) (interface{}, error) {
	prev, limit, err := getPageData(ctx, 50)
	if err != nil {
		return nil, err
	}

	list, last, err := explorer.ListBlocks(ctx, prev, limit)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"blocks": httpjson.Array(list),
		"last":   last,
	}, nil
}

// EXPERIMENTAL(jeffomatic), implemented for R3 demo. Before baking it into the
// public API, we should decide whether this style of API querying is desirable.
func getExplorerAssets(ctx context.Context, req struct {
	AssetIDs []string `json:"asset_ids"`
}) (interface{}, error) {
	assets, err := explorer.GetAssets(ctx, req.AssetIDs)
	if err != nil {
		return nil, err
	}

	var res []*explorer.Asset
	for _, a := range assets {
		res = append(res, a)
	}

	return res, nil
}

func listExplorerUTXOsByAsset(ctx context.Context, assetID string) (interface{}, error) {
	prev, limit, err := getPageData(ctx, 50)
	if err != nil {
		return nil, err
	}

	h, err := bc.ParseHash(assetID)
	if err != nil {
		return nil, errors.WithDetailf(httpjson.ErrBadRequest, "invalid asset ID: %q", assetID)
	}

	list, last, err := explorer.ListUTXOsByAsset(ctx, bc.AssetID(h), prev, limit)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"utxos": httpjson.Array(list),
		"last":  last,
	}, nil
}
