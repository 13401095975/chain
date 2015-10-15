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

// POST /v3/projects/:projID/manager-nodes
func createWallet(ctx context.Context, appID string, wReq *asset.CreateNodeReq) (interface{}, error) {
	if err := projectAuthz(ctx, appID); err != nil {
		return nil, err
	}

	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer dbtx.Rollback()

	wallet, err := asset.CreateNode(ctx, asset.ManagerNode, appID, wReq)
	if err != nil {
		return nil, err
	}

	err = dbtx.Commit()
	if err != nil {
		return nil, err
	}

	return wallet, nil
}

// PUT /v3/manager-nodes/:mnodeID
func updateManagerNode(ctx context.Context, mnodeID string, in struct{ Label *string }) error {
	if err := managerAuthz(ctx, mnodeID); err != nil {
		return err
	}
	return appdb.UpdateManagerNode(ctx, mnodeID, in.Label)
}

// GET /v3/projects/:projID/manager-nodes
func listWallets(ctx context.Context, projID string) (interface{}, error) {
	if err := projectAuthz(ctx, projID); err != nil {
		return nil, err
	}
	return appdb.ListWallets(ctx, projID)
}

// GET /v3/manager-nodes/:mnodeID
func getWallet(ctx context.Context, mnodeID string) (interface{}, error) {
	if err := managerAuthz(ctx, mnodeID); err != nil {
		return nil, err
	}
	return appdb.GetWallet(ctx, mnodeID)
}

// GET /v3/manager-nodes/:mnodeID/activity
func getWalletActivity(ctx context.Context, wID string) (interface{}, error) {
	if err := managerAuthz(ctx, wID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defActivityPageSize)
	if err != nil {
		return nil, err
	}

	activity, last, err := appdb.WalletActivity(ctx, wID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":       last,
		"activities": httpjson.Array(activity),
	}
	return ret, nil
}

// GET /v3/manager-nodes/:mnodeID/transactions/:txID
func walletTxActivity(ctx context.Context, mnodeID, txID string) (interface{}, error) {
	if err := managerAuthz(ctx, mnodeID); err != nil {
		return nil, err
	}
	return appdb.WalletTxActivity(ctx, mnodeID, txID)
}

// GET /v3/manager-nodes/:mnodeID/balance
func walletBalance(ctx context.Context, walletID string) (interface{}, error) {
	if err := managerAuthz(ctx, walletID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defBalancePageSize)
	if err != nil {
		return nil, err
	}

	balances, last, err := appdb.WalletBalance(ctx, walletID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":     last,
		"balances": httpjson.Array(balances),
	}
	return ret, nil
}

// GET /v3/manager-nodes/:mnodeID/accounts
func listBuckets(ctx context.Context, walletID string) (interface{}, error) {
	if err := managerAuthz(ctx, walletID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defBucketPageSize)
	if err != nil {
		return nil, err
	}

	buckets, last, err := appdb.ListBuckets(ctx, walletID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":     last,
		"accounts": httpjson.Array(buckets),
	}
	return ret, nil
}

// POST /v3/manager-nodes/:mnodeID/accounts
func createBucket(ctx context.Context, walletID string, in struct{ Label string }) (*appdb.Bucket, error) {
	defer metrics.RecordElapsed(time.Now())
	if err := managerAuthz(ctx, walletID); err != nil {
		return nil, err
	}
	return appdb.CreateBucket(ctx, walletID, in.Label)
}

// GET /v3/accounts/:accountID
func getBucket(ctx context.Context, bucketID string) (interface{}, error) {
	if err := accountAuthz(ctx, bucketID); err != nil {
		return nil, err
	}
	return appdb.GetBucket(ctx, bucketID)
}

// GET /v3/accounts/:accountID/activity
func getBucketActivity(ctx context.Context, bid string) (interface{}, error) {
	if err := accountAuthz(ctx, bid); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defActivityPageSize)
	if err != nil {
		return nil, err
	}

	activity, last, err := appdb.BucketActivity(ctx, bid, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":       last,
		"activities": httpjson.Array(activity),
	}
	return ret, nil
}

// GET /v3/accounts/:accountID/balance
func bucketBalance(ctx context.Context, bucketID string) (interface{}, error) {
	if err := accountAuthz(ctx, bucketID); err != nil {
		return nil, err
	}
	prev, limit, err := getPageData(ctx, defBalancePageSize)
	if err != nil {
		return nil, err
	}

	balances, last, err := appdb.BucketBalance(ctx, bucketID, prev, limit)
	if err != nil {
		return nil, err
	}

	ret := map[string]interface{}{
		"last":     last,
		"balances": httpjson.Array(balances),
	}
	return ret, nil
}

// /v3/accounts/:accountID/addresses
func createAddr(ctx context.Context, bucketID string, in struct {
	Amount  uint64
	Expires time.Time
}) (interface{}, error) {
	if err := accountAuthz(ctx, bucketID); err != nil {
		return nil, err
	}
	addr := &appdb.Address{
		BucketID: bucketID,
		Amount:   in.Amount,
		Expires:  in.Expires,
		IsChange: false,
	}
	err := asset.CreateAddress(ctx, addr)
	if err != nil {
		return nil, err
	}

	signers := asset.Signers(addr.Keys, asset.ReceiverPath(addr))
	ret := map[string]interface{}{
		"address":             addr.Address,
		"signatures_required": addr.SigsRequired,
		"signers":             addrSigners(signers),
		"block_chain":         "sandbox",
		"created":             addr.Created.UTC(),
		"expires":             optionalTime(addr.Expires),
		"id":                  addr.ID,
		"index":               addr.Index[:],
	}
	return ret, nil
}

// PUT /v3/accounts/:accountID
func updateAccount(ctx context.Context, accountID string, in struct{ Label *string }) error {
	if err := accountAuthz(ctx, accountID); err != nil {
		return err
	}
	return appdb.UpdateAccount(ctx, accountID, in.Label)
}

func addrSigners(signers []*asset.DerivedKey) (v []interface{}) {
	for _, s := range signers {
		v = append(v, map[string]interface{}{
			"pubkey":          s.Address.String(),
			"derivation_path": s.Path,
			"xpub":            s.Root.String(),
		})
	}
	return v
}

// optionalTime returns a pointer to t or nil, if t is zero.
// It is helpful for JSON structs with omitempty.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
