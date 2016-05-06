package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/lib/pq"
	"golang.org/x/net/context"

	"chain/database/pg"
	"chain/database/sql"
)

const (
	testDir = "testfiles"
	schema  = "migratetest"
)

var (
	testDB string
	db     *sql.DB
)

func init() {
	sql.Register("schemadb", pg.SchemaDriver(schema))
	testDB = "postgres:///api-test?sslmode=disable"
	if s := os.Getenv("DB_URL_TEST"); s != "" {
		testDB = s
	}

	var err error
	db, err = sql.Open("schemadb", testDB)
	if err != nil {
		log.Fatal(err)
	}
}

func testPath(filename string) string {
	return filepath.Join(testDir, filename)
}

// resetSchema will reset the database schema to the one in the file
// at the provided path. We cannot use pgtest here because we're
// not using the same schema file consistently throughout this package's
// tests.
func resetSchema(db *sql.DB, schemaSQLPath string) {
	ctx := context.Background()
	const reset = `
		DROP SCHEMA IF EXISTS %s CASCADE;
		CREATE SCHEMA %s;
	`

	quotedSchema := pq.QuoteIdentifier(schema)
	_, err := db.Exec(ctx, fmt.Sprintf(reset, quotedSchema, quotedSchema))
	if err != nil {
		panic(err)
	}
	b, err := ioutil.ReadFile(schemaSQLPath)
	if err != nil {
		panic(err)
	}
	_, err = db.Exec(ctx, string(b))
	if err != nil {
		panic(err)
	}
}

func TestLoadMigrations(t *testing.T) {
	testCases := []struct {
		schemaFile   string
		migrationDir string
		want         []migration
	}{
		{
			schemaFile:   "empty.sql",
			migrationDir: "empty",
			want:         []migration{},
		},
		{
			schemaFile:   "migration-table.sql",
			migrationDir: "empty",
			want:         []migration{},
		},
		{
			schemaFile:   "empty.sql",
			migrationDir: "one-migration",
			want: []migration{
				{
					Filename: "select.sql",
					Hash:     "b4e0497804e46e0a0b0b8c31975b062152d551bac49c3c2e80932567b4085dcd",
				},
			},
		},
		{
			schemaFile:   "one-migration-applied.sql",
			migrationDir: "one-migration",
			want: []migration{
				{
					Filename: "select.sql",
					Hash:     "b4e0497804e46e0a0b0b8c31975b062152d551bac49c3c2e80932567b4085dcd",
					Applied:  true,
				},
			},
		},
		{
			schemaFile:   "empty.sql",
			migrationDir: "multiple",
			want: []migration{
				{
					Filename: "2015-11-03.0.api.example.sql",
					Hash:     "b4e0497804e46e0a0b0b8c31975b062152d551bac49c3c2e80932567b4085dcd",
				},
				{
					Filename: "2015-12-15.0.api.another-example.sql",
					Hash:     "a41109d24069b4822ddc5f367b25d484dc7e839bff338ce7a3e5da641caacda0",
				},
			},
		},
	}

	for testNum, tc := range testCases {
		resetSchema(db, testPath(tc.schemaFile))
		migrations, err := loadMigrations(db, testPath(tc.migrationDir))
		if err != nil {
			t.Error(err)
			continue
		}

		// Comparing structs with time.Time using reflect.DeepEqual is a pain
		// because of the *time.Location, so we manually iterate and compare.
		if len(migrations) != len(tc.want) {
			t.Errorf("%d: got=%#v want=%#v", testNum, migrations, tc.want)
			continue
		}
		for i, got := range migrations {
			want := tc.want[i]
			if got.Filename != want.Filename || got.Hash != want.Hash || got.Applied != want.Applied {
				t.Errorf("%d: migration %v, got=%#v want=%#v", testNum, i, got, want)
			}
		}
	}
}

func TestApplyMigrationSQL(t *testing.T) {
	resetSchema(db, testPath("empty.sql"))
	migrations, err := loadMigrations(db, testPath("one-migration"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Errorf("len(migrations) = %d; want=1", len(migrations))
	}
	err = runMigration(db, testDB, testPath("one-migration"), migrations[0])
	if err != nil {
		t.Error(err)
	}
}

func TestApplyMigrationGo(t *testing.T) {
	resetSchema(db, testPath("empty.sql"))
	migrations, err := loadMigrations(db, testPath("go-migration"))
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Errorf("len(migrations) = %d; want=1", len(migrations))
	}
	err = runMigration(db, testDB, testPath("go-migration"), migrations[0])
	if err != nil {
		t.Error(err)
	}
}
