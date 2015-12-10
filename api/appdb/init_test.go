package appdb

import (
	"database/sql"
	"log"
	"os"

	"chain/database/pg/pgtest"
)

var db *sql.DB

func init() {
	u := "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		u = s
	}

	db = pgtest.Open(u, "appdbtest", "schema.sql")
	err := Init(db)
	if err != nil {
		log.Fatal(err)
	}
}
