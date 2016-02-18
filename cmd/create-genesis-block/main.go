package main

import (
	"encoding/hex"
	"log"

	"golang.org/x/net/context"

	"github.com/btcsuite/btcd/btcec"

	"chain/api/asset"
	"chain/api/txdb"
	"chain/database/pg"
	"chain/database/sql"
	"chain/env"
	"chain/fedchain"
)

var (
	dbURL    = env.String("DB_URL", "postgres:///api?sslmode=disable")
	blockKey = env.String("BLOCK_KEY", "2c1f68880327212b6aa71d7c8e0a9375451143352d5c760dc38559f1159c84ce")
	db       *sql.DB
)

func main() {
	log.SetFlags(0)
	env.Parse()

	keyBytes, err := hex.DecodeString(*blockKey)
	if err != nil {
		log.Fatalln("error:", err)
	}

	privKey, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), keyBytes)
	asset.BlockKey = privKey

	sql.Register("schemadb", pg.SchemaDriver("create-genesis-block"))
	db, err := sql.Open("schemadb", *dbURL)
	if err != nil {
		log.Fatalln("error:", err)
	}
	ctx := pg.NewContext(context.Background(), db)

	store := txdb.NewStore()
	fc := fedchain.New(store, nil)
	b, err := fc.UpsertGenesisBlock(ctx, []*btcec.PublicKey{pubKey}, 1)
	if err != nil {
		log.Fatalln("error:", err)
	}
	log.Printf("block created: %+v", b)
}
