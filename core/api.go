// Package core provides http handlers for all Chain operations.
package core

import (
	"context"
	"net/http"
	"sync"
	"time"

	"chain/core/account"
	"chain/core/asset"
	"chain/core/leader"
	"chain/core/mockhsm"
	"chain/core/query"
	"chain/core/txbuilder"
	"chain/core/txdb"
	"chain/database/pg"
	"chain/encoding/json"
	"chain/errors"
	"chain/generated/dashboard"
	"chain/generated/doc"
	"chain/net/http/authn"
	"chain/net/http/gzip"
	"chain/net/http/limit"
	"chain/net/http/reqid"
	"chain/net/http/static"
	"chain/net/rpc"
	"chain/protocol"
	"chain/protocol/bc"
)

const (
	defGenericPageSize = 100
)

// TODO(kr): change this to "network" or something.
const networkRPCPrefix = "/rpc/"

var (
	errNotFound       = errors.New("not found")
	errRateLimited    = errors.New("request limit exceeded")
	errLeaderElection = errors.New("no leader; pending election")
)

// Handler serves the Chain HTTP API
type Handler struct {
	Chain        *protocol.Chain
	Store        *txdb.Store
	Assets       *asset.Registry
	Accounts     *account.Manager
	HSM          *mockhsm.HSM
	Indexer      *query.Indexer
	Config       *Config
	DB           pg.DB
	Addr         string
	AltAuth      func(*http.Request) bool
	Signer       func(context.Context, *bc.Block) ([]byte, error)
	RequestLimit int

	once           sync.Once
	handler        http.Handler
	actionDecoders map[string]func(data []byte) (txbuilder.Action, error)

	healthMu     sync.Mutex
	healthErrors map[string]interface{}
}

func maxBytes(h http.Handler) http.Handler {
	const maxReqSize = 1e5 // 100kB
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// A block can easily be bigger than maxReqSize, but everything
		// else should be pretty small.
		if req.URL.Path != networkRPCPrefix+"signer/sign-block" {
			req.Body = http.MaxBytesReader(w, req.Body, maxReqSize)
		}
		h.ServeHTTP(w, req)
	})
}

func (h *Handler) init() {
	// Setup the available transact actions.
	h.actionDecoders = map[string]func(data []byte) (txbuilder.Action, error){
		"control_account":                h.Accounts.DecodeControlAction,
		"control_program":                txbuilder.DecodeControlProgramAction,
		"issue":                          h.Assets.DecodeIssueAction,
		"spend_account":                  h.Accounts.DecodeSpendAction,
		"spend_account_unspent_output":   h.Accounts.DecodeSpendUTXOAction,
		"set_transaction_reference_data": txbuilder.DecodeSetTxRefDataAction,
	}

	// Setup the muxer.
	needConfig := jsonHandler
	if h.Config == nil {
		needConfig = func(f interface{}) http.Handler {
			return alwaysError(errUnconfigured)
		}
	}

	m := http.NewServeMux()
	m.Handle("/", alwaysError(errNotFound))
	m.Handle("/health", jsonHandler(h.health))

	m.Handle("/create-account", needConfig(h.createAccount))
	m.Handle("/create-asset", needConfig(h.createAsset))
	m.Handle("/build-transaction", needConfig(h.build))
	m.Handle("/submit-transaction", needConfig(h.submit))
	m.Handle("/create-control-program", needConfig(h.createControlProgram))
	m.Handle("/create-transaction-feed", needConfig(h.createTxFeed))
	m.Handle("/get-transaction-feed", needConfig(h.getTxFeed))
	m.Handle("/update-transaction-feed", needConfig(h.updateTxFeed))
	m.Handle("/delete-transaction-feed", needConfig(h.deleteTxFeed))
	m.Handle("/mockhsm/create-key", needConfig(h.mockhsmCreateKey))
	m.Handle("/mockhsm/list-keys", needConfig(h.mockhsmListKeys))
	m.Handle("/mockhsm/delkey", needConfig(h.mockhsmDelKey))
	m.Handle("/mockhsm/sign-transaction", needConfig(h.mockhsmSignTemplates))
	m.Handle("/list-accounts", needConfig(h.listAccounts))
	m.Handle("/list-assets", needConfig(h.listAssets))
	m.Handle("/list-transaction-feeds", needConfig(h.listTxFeeds))
	m.Handle("/list-transactions", needConfig(h.listTransactions))
	m.Handle("/list-balances", needConfig(h.listBalances))
	m.Handle("/list-unspent-outputs", needConfig(h.listUnspentOutputs))
	m.Handle("/reset", needConfig(h.reset))

	m.Handle(networkRPCPrefix+"submit", needConfig(h.Chain.AddTx))
	m.Handle(networkRPCPrefix+"get-blocks", needConfig(h.getBlocksRPC))
	m.Handle(networkRPCPrefix+"get-snapshot", needConfig(h.getSnapshotRPC))
	m.Handle(networkRPCPrefix+"signer/sign-block", needConfig(h.leaderSignHandler(h.Signer)))
	m.Handle(networkRPCPrefix+"block-height", needConfig(func(ctx context.Context) map[string]uint64 {
		h := h.Chain.Height()
		return map[string]uint64{
			"block_height": h,
		}
	}))

	m.Handle("/create-access-token", jsonHandler(h.createAccessToken))
	m.Handle("/list-access-tokens", jsonHandler(h.listAccessTokens))
	m.Handle("/delete-access-token", jsonHandler(h.deleteAccessToken))
	m.Handle("/configure", jsonHandler(h.configure))
	m.Handle("/info", jsonHandler(h.info))

	latencyHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if l := latency(m, req); l != nil {
			defer l.RecordSince(time.Now())
		}
		m.ServeHTTP(w, req)
	})

	var handler http.Handler = authn.BasicHandler{
		Auth: (&apiAuthn{
			tokenMap: make(map[string]tokenResult),
			alt:      h.AltAuth,
		}).auth,
		Next: latencyHandler,
	}
	handler = maxBytes(handler)
	handler = webAssetsHandler(handler)
	if h.RequestLimit > 0 {
		handler = limit.Handler(handler, alwaysError(errRateLimited), h.RequestLimit, 100, limit.AuthUserID)
	}
	handler = gzip.Handler{Handler: handler}
	handler = dbContextHandler(handler, h.DB)
	handler = reqid.Handler(handler)
	handler = timeoutContextHandler(handler)
	h.handler = handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.once.Do(h.init)

	h.handler.ServeHTTP(w, r)
}

// Config encapsulates Core-level, persistent configuration options.
type Config struct {
	IsSigner             bool    `json:"is_signer"`
	IsGenerator          bool    `json:"is_generator"`
	BlockchainID         bc.Hash `json:"blockchain_id"`
	GeneratorURL         string  `json:"generator_url"`
	GeneratorAccessToken string  `json:"generator_access_token"`
	ConfiguredAt         time.Time
	BlockPub             string         `json:"block_pub"`
	Signers              []ConfigSigner `json:"block_signer_urls"`
	Quorum               int
	MaxIssuanceWindow    time.Duration
}

type ConfigSigner struct {
	AccessToken string        `json:"access_token"`
	Pubkey      json.HexBytes `json:"pubkey"`
	URL         string        `json:"url"`
}

// Used as a request object for api queries
type requestQuery struct {
	Filter       string        `json:"filter,omitempty"`
	FilterParams []interface{} `json:"filter_params,omitempty"`
	SumBy        []string      `json:"sum_by,omitempty"`
	PageSize     int           `json:"page_size"`

	// AscLongPoll and Timeout are used by /list-transactions
	// to facilitate notifications.
	AscLongPoll bool          `json:"ascending_with_long_poll,omitempty"`
	Timeout     json.Duration `json:"timeout"`

	After string `json:"after"`

	// These two are used for time-range queries like /list-transactions
	StartTimeMS uint64 `json:"start_time,omitempty"`
	EndTimeMS   uint64 `json:"end_time,omitempty"`

	// This is used for point-in-time queries like /list-balances
	// TODO(bobg): Different request structs for endpoints with different needs
	TimestampMS uint64 `json:"timestamp,omitempty"`

	// This is used for filtering results from /list-access-tokens
	// Value must be "client" or "network"
	Type string `json:"type"`
}

// Used as a response object for api queries
type page struct {
	Items    interface{}  `json:"items"`
	Next     requestQuery `json:"next"`
	LastPage bool         `json:"last_page"`
}

// timeoutContextHandler propagates the timeout, if any, provided as a header
// in the http request.
func timeoutContextHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		timeout, err := time.ParseDuration(req.Header.Get(rpc.HeaderTimeout))
		if err != nil {
			handler.ServeHTTP(w, req) // unmodified
			return
		}

		ctx := req.Context()
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		handler.ServeHTTP(w, req.WithContext(ctx))
	})
}

func dbContextHandler(handler http.Handler, db pg.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		ctx = pg.NewContext(ctx, db)
		handler.ServeHTTP(w, req.WithContext(ctx))
	})
}

func webAssetsHandler(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/dashboard/", http.StripPrefix("/dashboard/", static.Handler{
		Assets:  dashboard.Files,
		Default: "index.html",
	}))
	mux.Handle("/doc/", http.StripPrefix("/doc/", static.Handler{
		Assets: doc.Files,
		Index:  "index",
	}))
	mux.Handle("/", next)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			http.Redirect(w, req, "/dashboard/", http.StatusFound)
			return
		}

		mux.ServeHTTP(w, req)
	})
}

func (h *Handler) leaderSignHandler(f func(context.Context, *bc.Block) ([]byte, error)) func(context.Context, *bc.Block) ([]byte, error) {
	return func(ctx context.Context, b *bc.Block) ([]byte, error) {
		if f == nil {
			return nil, errNotFound // TODO(kr): is this really the right error here?
		}
		if leader.IsLeading() {
			return f(ctx, b)
		}
		var resp []byte
		err := h.callLeader(ctx, "/rpc/signer/sign-block", b, &resp)
		return resp, err
	}
}

func (h *Handler) callLeader(ctx context.Context, path string, body interface{}, resp interface{}) error {
	addr, err := leader.Address(ctx)
	if err != nil {
		return errors.Wrap(err)
	}

	// Don't infinite loop if the leader's address is our own address.
	// This is possible if we just became the leader. The client should
	// just retry.
	if addr == h.Addr {
		return errLeaderElection
	}

	// TODO(jackson): If using TLS, use https:// here.
	l := &rpc.Client{
		BaseURL: "http://" + addr,
	}

	return l.Call(ctx, path, body, &resp)
}
