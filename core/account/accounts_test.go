package account

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"chain/core/signers"
	"chain/database/pg"
	"chain/database/pg/pgtest"
	"chain/errors"
	"chain/net/http/httpjson"
	"chain/protocol/vm"
	"chain/testutil"
)

var dummyXPub = testutil.TestXPub.String()

func TestCreateAccount(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)

	account, err := Create(ctx, []string{dummyXPub}, 1, "", nil, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	// Verify that the account was defined.
	var id string
	var checkQ = `SELECT id FROM signers`
	err = pg.QueryRow(ctx, checkQ).Scan(&id)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if id != account.ID {
		t.Errorf("expected account %s to be recorded as %s", account.ID, id)
	}
}

func TestCreateAccountReusedAlias(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	createTestAccount(ctx, t, "some-account", nil)

	_, err := Create(ctx, []string{dummyXPub}, 1, "some-account", nil, nil)
	if errors.Root(err) != httpjson.ErrBadRequest {
		t.Errorf("Expected %s when reusing an alias, got %v", httpjson.ErrBadRequest, err)
	}
}

func resetSeqs(ctx context.Context, t testing.TB) {
	acpIndexNext, acpIndexCap = 1, 100
	pgtest.Exec(ctx, t, `ALTER SEQUENCE account_control_program_seq RESTART`)
	pgtest.Exec(ctx, t, `ALTER SEQUENCE signers_key_index_seq RESTART`)
}

func TestCreateControlProgram(t *testing.T) {
	ctx := pg.NewContext(context.Background(), pgtest.NewTx(t))
	resetSeqs(ctx, t)

	account, err := Create(ctx, []string{dummyXPub}, 1, "", nil, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	got, err := CreateControlProgram(ctx, account.ID, false)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	want, err := vm.Compile("DUP TOALTSTACK SHA3 0xa8e89cdeac83b17d2da60de5cf1499979376fb421d1bbc2109cdeebea41807c1 1 1 CHECKMULTISIG VERIFY FROMALTSTACK 0 CHECKPREDICATE")
	if err != nil {
		testutil.FatalErr(t, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("got control program = %x want %x", got, want)
	}
}

func createTestAccount(ctx context.Context, t testing.TB, alias string, tags map[string]interface{}) *Account {
	account, err := Create(ctx, []string{dummyXPub}, 1, alias, tags, nil)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	return account
}

func createTestControlProgram(ctx context.Context, t testing.TB, accountID string) []byte {
	if accountID == "" {
		account := createTestAccount(ctx, t, "", nil)
		accountID = account.ID
	}

	acp, err := CreateControlProgram(ctx, accountID, false)
	if err != nil {
		testutil.FatalErr(t, err)
	}
	return acp
}

func TestFindByID(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	tags := map[string]interface{}{"someTag": "taggityTag"}
	account := createTestAccount(ctx, t, "", tags)

	found, err := FindByID(ctx, account.ID)
	if err != nil {
		testutil.FatalErr(t, err)
	}

	if !reflect.DeepEqual(account, found) {
		t.Errorf("expected found account to be %v, instead found %v", account, found)
	}
}

func TestFindByAlias(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	tags := map[string]interface{}{"someTag": "taggityTag"}
	account := createTestAccount(ctx, t, "some-alias", tags)

	found, err := FindByAlias(ctx, "some-alias")
	if err != nil {
		testutil.FatalErr(t, err)
	}

	if !reflect.DeepEqual(account, found) {
		t.Errorf("expected found account to be %v, instead found %v", account, found)
	}
}

func TestArchiveByID(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	account := createTestAccount(ctx, t, "", nil)

	err := Archive(ctx, account.ID, "")
	if err != nil {
		testutil.FatalErr(t, err)
	}

	_, err = FindByID(ctx, account.ID)
	if errors.Root(err) != signers.ErrArchived {
		t.Errorf("expected %s when Finding an archived account, instead got %s", signers.ErrArchived, err)
	}
}

func TestArchiveByAlias(t *testing.T) {
	dbtx := pgtest.NewTx(t)
	ctx := pg.NewContext(context.Background(), dbtx)
	createTestAccount(ctx, t, "some-alias", nil)

	err := Archive(ctx, "", "some-alias")
	if err != nil {
		testutil.FatalErr(t, err)
	}

	_, err = FindByAlias(ctx, "some-alias")
	if errors.Root(err) != signers.ErrArchived {
		t.Errorf("expected %s when Finding an archived account, instead got %s", signers.ErrArchived, err)
	}
}
