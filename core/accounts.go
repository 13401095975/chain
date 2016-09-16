package core

import (
	"context"
	"sync"
	"time"

	"chain/core/account"
	"chain/errors"
	"chain/metrics"
	"chain/net/http/httpjson"
)

// This type enforces JSON field ordering in API output.
type accountResponse struct {
	ID     interface{} `json:"id"`
	Alias  interface{} `json:"alias"`
	XPubs  interface{} `json:"xpubs"`
	Quorum interface{} `json:"quorum"`
	Tags   interface{} `json:"tags"`
}

// POST /create-account
func createAccount(ctx context.Context, ins []struct {
	XPubs  []string
	Quorum int
	Alias  string
	Tags   map[string]interface{}

	// ClientToken is the application's unique token for the account. Every account
	// should have a unique client token. The client token is used to ensure
	// idempotency of create account requests. Duplicate create account requests
	// with the same client_token will only create one account.
	ClientToken *string `json:"client_token"`
}) interface{} {
	defer metrics.RecordElapsed(time.Now())

	responses := make([]interface{}, len(ins))
	var wg sync.WaitGroup
	wg.Add(len(responses))

	for i := 0; i < len(responses); i++ {
		go func(i int) {
			defer wg.Done()
			acc, err := account.Create(ctx, ins[i].XPubs, ins[i].Quorum, ins[i].Alias, ins[i].Tags, ins[i].ClientToken)
			if err != nil {
				logHTTPError(ctx, err)
				responses[i], _ = errInfo(err)
			} else {
				r := &accountResponse{
					ID:     acc.ID,
					Alias:  acc.Alias,
					XPubs:  acc.XPubs,
					Quorum: acc.Quorum,
					Tags:   acc.Tags,
				}
				responses[i] = r
			}
		}(i)
	}

	wg.Wait()
	return responses
}

// POST /archive-account
func archiveAccount(ctx context.Context, in struct {
	AccountID string `json:"account_id"`
	Alias     string `json:"alias"`
}) error {
	if in.AccountID != "" && in.Alias != "" {
		return errors.Wrap(httpjson.ErrBadRequest, "cannot supply both account_id and alias")
	}

	if in.AccountID == "" && in.Alias == "" {
		return errors.Wrap(httpjson.ErrBadRequest, "must supply either account_id or alias")
	}

	return account.Archive(ctx, in.AccountID, in.Alias)
}
