package admin

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"testing"

	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/fedchain/bc"
)

func init() {
	u := "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		u = s
	}

	db := pgtest.Open(u, "admintest", "../appdb/schema.sql")
	err := appdb.Init(db)
	if err != nil {
		log.Fatal(err)
	}
}

// Establish a context object with a new db transaction in which to
// run the given callback function.
func withContext(tb testing.TB, sql string, fn func(context.Context)) {
	var dbtx pg.Tx
	if sql == "" {
		dbtx = pgtest.TxWithSQL(tb)
	} else {
		dbtx = pgtest.TxWithSQL(tb, sql)
	}
	defer dbtx.Rollback()
	ctx := pg.NewContext(context.Background(), dbtx)
	fn(ctx)
}

func hashForFixture(h bc.Hash) string {
	return fmt.Sprintf("decode('%s', 'hex')", hex.EncodeToString(h[:]))
}

func blockForFixture(b *bc.Block) string {
	buf := new(bytes.Buffer)
	_, err := b.WriteTo(buf)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("decode('%s', 'hex')", hex.EncodeToString(buf.Bytes()))
}

func txForFixture(tx *bc.TxData) string {
	data, err := tx.MarshalText()
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("decode('%s', 'hex')", hex.EncodeToString(data))
}

func TestGetSummary(t *testing.T) {
	b0 := bc.Block{BlockHeader: bc.BlockHeader{Height: 0}}
	b1 := bc.Block{BlockHeader: bc.BlockHeader{Height: 1}}

	t0 := bc.TxData{Metadata: []byte{0}}
	t1 := bc.TxData{Metadata: []byte{1}}
	t2 := bc.TxData{Metadata: []byte{2}}
	t3 := bc.TxData{Metadata: []byte{3}}
	t4 := bc.TxData{Metadata: []byte{4}}

	var fix = `
		INSERT INTO projects
			(id, name)
		VALUES
			('proj-id-0', 'proj-name-0'),
			('proj-id-other', 'proj-name-other');

		INSERT INTO manager_nodes
			(id, project_id, key_index, label)
		VALUES
			('mn-id-0', 'proj-id-0', 0, 'mn-label-0'),
			('mn-id-1', 'proj-id-0', 1, 'mn-label-1'),
			('mn-id-other', 'proj-id-other', 2, 'mn-label-other');

		INSERT INTO issuer_nodes
			(id, project_id, key_index, label, keyset)
		VALUES
			('in-id-0', 'proj-id-0', 3, 'in-label-0', '{}'),
			('in-id-1', 'proj-id-0', 4, 'in-label-1', '{}'),
			('mn-id-other', 'proj-id-other', 5, 'in-label-other', '{}');

		INSERT INTO blocks
			(block_hash, height, data)
		VALUES
			(` + hashForFixture(b0.Hash()) + `, ` + strconv.Itoa(int(b0.Height)) + `, ` + blockForFixture(&b0) + `),
			(` + hashForFixture(b1.Hash()) + `, ` + strconv.Itoa(int(b1.Height)) + `, ` + blockForFixture(&b1) + `);

		INSERT INTO blocks_txs
			(block_hash, tx_hash)
		VALUES
			(` + hashForFixture(b0.Hash()) + `, ` + hashForFixture(t0.Hash()) + `),
			(` + hashForFixture(b1.Hash()) + `, ` + hashForFixture(t1.Hash()) + `),
			(` + hashForFixture(b1.Hash()) + `, ` + hashForFixture(t2.Hash()) + `);

		INSERT INTO pool_txs
			(tx_hash, data)
		VALUES
			(` + hashForFixture(t3.Hash()) + `, ` + txForFixture(&t3) + `),
			(` + hashForFixture(t4.Hash()) + `, ` + txForFixture(&t4) + `);
	`

	withContext(t, fix, func(ctx context.Context) {
		want := &Summary{
			BlockFreqMs: 0,
			BlockCount:  2,
			TransactionCount: TxCount{
				Confirmed:   3,
				Unconfirmed: 2,
			},
			Permissions: NodePerms{
				ManagerNodes: []NodePermStatus{
					{"mn-id-0", "mn-label-0", true},
					{"mn-id-1", "mn-label-1", true},
				},
				IssuerNodes: []NodePermStatus{
					{"in-id-0", "in-label-0", true},
					{"in-id-1", "in-label-1", true},
				},
				AuditorNodes: []NodePermStatus{
					{"audnode-proj-id-0", "Auditor Node for proj-id-0", true},
				},
			},
		}

		got, err := GetSummary(ctx, "proj-id-0")
		if err != nil {
			t.Fatal("unexpected error: ", err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("summary:\ngot:  %v\nwant: %v", *got, *want)
		}
	})
}
