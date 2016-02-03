// bootdb bootstraps the database to a minimal functional state
//
//   user
//   auth token
//   project (with membership)
//   admin node
//   manager node (with keys)
//   issuer node (with keys)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"chain/api/appdb"
	"chain/database/pg"
	"chain/database/sql"
	"chain/env"
	"chain/errors"
	"chain/fedchain-sandbox/hdkey"
	"chain/log"

	"github.com/btcsuite/btcutil/hdkeychain"
	"golang.org/x/net/context"
)

// config vars
var dbURL = env.String("DB_URL", "postgres:///api?sslmode=disable")

var (
	db     *sql.DB
	logbuf bytes.Buffer
)

func main() {
	env.Parse()
	log.SetOutput(&logbuf)

	if len(os.Args) != 3 {
		fatal("usage: bootdb email password")
	}

	sql.Register("schemadb", pg.SchemaDriver("bootdb"))
	db, err := sql.Open("schemadb", *dbURL)
	if err != nil {
		fatal(err)
	}

	ctx := pg.NewContext(context.Background(), db)
	dbtx, ctx, err := pg.Begin(ctx)
	if err != nil {
		fatal("begin")
	}
	defer dbtx.Rollback(ctx)

	u, err := appdb.CreateUser(ctx, os.Args[1], os.Args[2])
	if err != nil {
		fatal(err)
	}

	tok, err := appdb.CreateAuthToken(ctx, u.ID, "api", nil)
	if err != nil {
		fatal(err)
	}

	proj, err := appdb.CreateProject(ctx, "proj", u.ID)
	if err != nil {
		fatal(err)
	}

	mpub, mpriv := genKey()
	mn, err := appdb.InsertManagerNode(ctx, proj.ID, "manager", mpub, mpriv, 0, 1)
	if err != nil {
		fatal(err)
	}

	ipub, ipriv := genKey()
	in, err := appdb.InsertIssuerNode(ctx, proj.ID, "issuer", ipub, ipriv, 1)
	if err != nil {
		fatal(err)
	}

	err = dbtx.Commit(ctx)
	if err != nil {
		fatal(err)
	}

	result, _ := json.MarshalIndent(map[string]string{
		"userID":        u.ID,
		"tokenID":       tok.ID,
		"tokenSecret":   tok.Secret,
		"projectID":     proj.ID,
		"managerXPRV":   mpriv[0].String(),
		"managerNodeID": mn.ID,
		"issuerXPRV":    ipriv[0].String(),
		"issuerNodeID":  in.ID,
	}, "", "  ")
	fmt.Printf("%s\n", result)
}

func genKey() (pub, priv []*hdkey.XKey) {
	pk, sk, err := newKey()
	if err != nil {
		fatal(err)
	}
	pub = append(pub, pk)
	priv = append(priv, sk)
	return
}

func newKey() (pub, priv *hdkey.XKey, err error) {
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return nil, nil, errors.Wrap(err, "generating key seed")
	}
	xprv, err := hdkeychain.NewMaster(seed)
	if err != nil {
		return nil, nil, errors.Wrap(err, "creating root xprv")
	}
	xpub, err := xprv.Neuter()
	if err != nil {
		return nil, nil, errors.Wrap(err, "getting root xpub")
	}
	return &hdkey.XKey{ExtendedKey: *xpub}, &hdkey.XKey{ExtendedKey: *xprv}, nil
}

func fatal(v interface{}) {
	io.Copy(os.Stderr, &logbuf)
	panic(v)
}
