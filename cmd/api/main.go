package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/kr/env"
	"github.com/kr/secureheader"
	"golang.org/x/net/context"

	"chain/api"
	"chain/api/appdb"
	"chain/database/pg"
	"chain/metrics"
	chainhttp "chain/net/http"
	"chain/net/http/gzip"
)

var (
	// config vars
	listenAddr = env.String("LISTEN", ":8080")
	dbURL      = env.String("DB_URL", "postgres:///api?sslmode=disable")

	db       *sql.DB
	buildTag = "dev"
)

func main() {
	sql.Register("schemadb", pg.SchemaDriver(buildTag))
	log.SetPrefix("api-" + buildTag + ": ")
	log.SetFlags(log.Lshortfile)
	env.Parse()

	var err error
	db, err = sql.Open("schemadb", *dbURL)
	if err != nil {
		log.Fatal(err)
	}
	appdb.Init(db)

	var h chainhttp.Handler
	h = api.Handler() // TODO(kr): authentication
	h = metrics.Handler{Handler: h}
	h = gzip.Handler{Handler: h}

	bg := context.Background()
	bg = pg.NewContext(bg, db)
	http.Handle("/", chainhttp.ContextHandler{Context: bg, Handler: h})
	http.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {})

	secureheader.DefaultConfig.PermitClearLoopback = true
	http.ListenAndServe(*listenAddr, secureheader.DefaultConfig)
}
