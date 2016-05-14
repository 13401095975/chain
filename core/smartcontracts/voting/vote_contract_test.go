package voting

import (
	"bytes"
	"reflect"
	"testing"

	"chain/core/asset/assettest"
	"chain/cos/bc"
	"chain/cos/txscript"
	"chain/cos/txscript/txscripttest"
	"chain/crypto/hash256"
	"chain/database/pg/pgtest"
)

func TestRegisterToVoteClause(t *testing.T) {
	ctx := pgtest.NewContext(t)

	var (
		rightAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		otherRightAssetID = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokenAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokensAssetAmount = bc.AssetAmount{
			AssetID: tokenAssetID,
			Amount:  200,
		}
		rightAssetAmount = bc.AssetAmount{
			AssetID: rightAssetID,
			Amount:  1,
		}
		right = rightScriptData{
			Deadline:       infiniteDeadline,
			Delegatable:    true,
			OwnershipChain: bc.Hash{}, // 0x000...000
			HolderScript:   []byte{txscript.OP_1},
		}
	)

	testCases := []struct {
		err   error
		right *rightScriptData
		prev  tokenScriptData
		out   tokenScriptData
	}{
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
			},
		},
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: exampleHash[:],
				OptionCount: 10,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: exampleHash[:],
				OptionCount: 10,
				State:       stateRegistered,
				SecretHash:  exampleHash,
			},
		},
		{
			// Output has wrong voting right asset id.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Cannot move from FINISHED even if distributed.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// State changed to VOTED, not REGISTERED.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Tx has a voting right, but it's the wrong voting
			// right.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Voting right output script doesn't match the token holding
			// contract sigscript param.
			err: txscript.ErrStackVerifyFailed,
			right: &rightScriptData{
				HolderScript: []byte{txscript.OP_2},
			},
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
	}

	for i, tc := range testCases {
		sb := txscript.NewScriptBuilder()
		sb = sb.
			AddData(right.PKScript()).
			AddInt64(int64(clauseRegister)).
			AddData(tokenHoldingContract)
		sigscript, err := sb.Script()
		if err != nil {
			t.Fatal(err)
		}
		r := right
		if tc.right != nil {
			r = *tc.right
		}
		err = txscripttest.NewTestTx(mockTimeFunc).
			AddInput(rightAssetAmount, r.PKScript(), nil).
			AddInput(tokensAssetAmount, tc.prev.PKScript(), sigscript).
			AddOutput(rightAssetAmount, r.PKScript()).
			AddOutput(tokensAssetAmount, tc.out.PKScript()).
			Execute(ctx, 1)
		if !reflect.DeepEqual(err, tc.err) {
			t.Errorf("%d: got=%s want=%s", i, err, tc.err)
		}
	}
}

func TestVoteClause(t *testing.T) {
	ctx := pgtest.NewContext(t)

	var (
		rightAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		otherRightAssetID = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokenAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokensAssetAmount = bc.AssetAmount{
			AssetID: tokenAssetID,
			Amount:  200,
		}
		rightAssetAmount = bc.AssetAmount{
			AssetID: rightAssetID,
			Amount:  1,
		}
		right = rightScriptData{
			Deadline:       infiniteDeadline,
			Delegatable:    true,
			OwnershipChain: bc.Hash{}, // 0x000...000
			HolderScript:   []byte{txscript.OP_1},
		}
		votingSecret     = []byte("an example voting secret")
		votingSecretHash = hash256.Sum(votingSecret)
	)

	testCases := []struct {
		err   error
		right *rightScriptData
		prev  tokenScriptData
		out   tokenScriptData
	}{
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  votingSecretHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        2,
			},
		},
		{
			// Already voted, changing vote
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 2,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        0,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 2,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        1,
			},
		},
		{
			// Wrong voting secret
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Output has wrong voting right asset id.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  votingSecretHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        2,
			},
		},
		{
			// Cannot move from FINISHED even if REGISTERED.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered | stateFinished,
				SecretHash:  votingSecretHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  votingSecretHash,
				Vote:        2,
			},
		},
		{
			// Vote is outside the range of options.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  votingSecretHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        99999,
			},
		},
		{
			// Tx has a voting right, but it's the wrong voting
			// right.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  votingSecretHash,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  votingSecretHash,
				Vote:        2,
			},
		},
		{
			// Voting right output script doesn't match the token holding
			// contract sigscript param.
			err: txscript.ErrStackVerifyFailed,
			right: &rightScriptData{
				HolderScript: []byte{txscript.OP_2},
			},
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateRegistered,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
	}

	for i, tc := range testCases {
		r := right
		if tc.right != nil {
			r = *tc.right
		}

		sb := txscript.NewScriptBuilder()
		sb = sb.
			AddInt64(tc.out.Vote).
			AddData(votingSecret).
			AddData(r.PKScript()).
			AddInt64(int64(clauseVote)).
			AddData(tokenHoldingContract)
		sigscript, err := sb.Script()
		if err != nil {
			t.Fatal(err)
		}
		err = txscripttest.NewTestTx(mockTimeFunc).
			AddInput(rightAssetAmount, r.PKScript(), nil).
			AddInput(tokensAssetAmount, tc.prev.PKScript(), sigscript).
			AddOutput(rightAssetAmount, r.PKScript()).
			AddOutput(tokensAssetAmount, tc.out.PKScript()).
			Execute(ctx, 1)
		if !reflect.DeepEqual(err, tc.err) {
			t.Errorf("%d: got=%s want=%s", i, err, tc.err)
		}
	}
}

func TestFinishVoteClause(t *testing.T) {
	ctx := pgtest.NewContext(t)

	var (
		rightAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		otherRightAssetID = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokenAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokensAssetAmount = bc.AssetAmount{
			AssetID: tokenAssetID,
			Amount:  200,
		}
	)

	testCases := []struct {
		err  error
		prev tokenScriptData
		out  tokenScriptData
	}{
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
			},
		},
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 10,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        8,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 10,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        8,
			},
		},
		{
			// Voting token state already finished.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
			},
		},
		{
			// Admin script does not authorize.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1, txscript.OP_DROP, txscript.OP_0},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1, txscript.OP_DROP, txscript.OP_0},
				OptionCount: 3,
				State:       stateDistributed + stateFinished,
				SecretHash:  exampleHash,
			},
		},
		{
			// Output has wrong voting right asset id.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Cannot change the base state at the same time.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateRegistered | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Vote changed during closing
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        3,
			},
		},
	}

	for i, tc := range testCases {
		sb := txscript.NewScriptBuilder()
		sb = sb.
			AddInt64(int64(clauseFinish)).
			AddData(tokenHoldingContract)
		sigscript, err := sb.Script()
		if err != nil {
			t.Fatal(err)
		}
		err = txscripttest.NewTestTx(mockTimeFunc).
			AddInput(tokensAssetAmount, tc.prev.PKScript(), sigscript).
			AddOutput(tokensAssetAmount, tc.out.PKScript()).
			Execute(ctx, 0)
		if !reflect.DeepEqual(err, tc.err) {
			t.Errorf("%d: got=%s want=%s", i, err, tc.err)
		}
	}
}

func TestRetireClause(t *testing.T) {
	ctx := pgtest.NewContext(t)

	var (
		rightAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokenAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokensAssetAmount = bc.AssetAmount{
			AssetID: tokenAssetID,
			Amount:  200,
		}
	)

	testCases := []struct {
		err          error
		prev         tokenScriptData
		outputScript []byte
	}{
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
			},
			outputScript: []byte{txscript.OP_RETURN},
		},
		{
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 10,
				State:       stateVoted | stateFinished,
				Vote:        2,
				SecretHash:  exampleHash,
			},
			outputScript: []byte{txscript.OP_RETURN},
		},
		{
			// Output sends back into the voting token contract.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			outputScript: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
				Vote:        1,
			}.PKScript(),
		},
		{
			// Token must be FINISHED to be retired.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			outputScript: []byte{txscript.OP_RETURN},
		},
		{
			// Admin script must authorize token retirement.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1, txscript.OP_DROP, txscript.OP_0},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			outputScript: []byte{txscript.OP_RETURN},
		},
	}

	for i, tc := range testCases {
		sb := txscript.NewScriptBuilder()
		sb = sb.
			AddInt64(int64(clauseRetire)).
			AddData(tokenHoldingContract)
		sigscript, err := sb.Script()
		if err != nil {
			t.Fatal(err)
		}
		err = txscripttest.NewTestTx(mockTimeFunc).
			AddInput(tokensAssetAmount, tc.prev.PKScript(), sigscript).
			AddOutput(tokensAssetAmount, tc.outputScript).
			Execute(ctx, 0)
		if !reflect.DeepEqual(err, tc.err) {
			t.Errorf("%d: got=%s want=%s", i, err, tc.err)
		}
	}
}

// TestTokenContractValidMatch tests generating a pkscript from a voting token.
// The generated pkscript is then used in the voting token p2c detection
// flow, where it should be found to match the contract. Then the decoded
// voting token and the original voting token are checked for equality.
func TestTokenContractValidMatch(t *testing.T) {
	testCases := []tokenScriptData{
		{
			Right:       bc.AssetID{},
			AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
			OptionCount: 10,
			State:       stateRegistered,
			SecretHash:  exampleHash,
			Vote:        5,
		},
		{
			Right:       bc.AssetID{0x01},
			AdminScript: []byte{0xde, 0xad, 0xbe, 0xef},
			OptionCount: 2,
			State:       stateVoted | stateFinished,
			SecretHash:  bc.Hash{},
			Vote:        1,
		},
	}

	for i, want := range testCases {
		script := want.PKScript()
		got, err := testTokenContract(script)
		if err != nil {
			t.Errorf("%d: testing voting token contract for %x: %s", i, script, err)
			continue
		}

		if got == nil {
			t.Errorf("%d: No match for pkscript %x generated from %#v", i, script, want)
			continue
		}

		if got.Right != want.Right {
			t.Errorf("%d: token.Right, got=%#v want=%#v", i, got.Right, want.Right)
		}
		if !bytes.Equal(got.AdminScript, want.AdminScript) {
			t.Errorf("%d: token.AdminScript, got=%#v want=%#v", i, got.AdminScript, want.AdminScript)
		}
		if got.OptionCount != want.OptionCount {
			t.Errorf("%d: token.OptionCount, got=%#v want=%#v", i, got.OptionCount, want.OptionCount)
		}
		if got.SecretHash != want.SecretHash {
			t.Errorf("%d: token.SecretHash, got=%#v want=%#v", i, got.SecretHash, want.SecretHash)
		}
		if got.Vote != want.Vote {
			t.Errorf("%d: token.Vote, got=%#v want=%#v", i, got.Vote, want.Vote)
		}
	}
}

func TestResetClause(t *testing.T) {
	ctx := pgtest.NewContext(t)

	var (
		rightAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		otherRightAssetID = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokenAssetID      = assettest.CreateAssetFixture(ctx, t, "", "", "")
		tokensAssetAmount = bc.AssetAmount{
			AssetID: tokenAssetID,
			Amount:  200,
		}
	)

	testCases := []struct {
		err  error
		prev tokenScriptData
		out  tokenScriptData
	}{
		{
			// Reset secret hash only
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash2,
			},
		},
		{
			// Move from voted | closed, to registered
			err: nil,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 10,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        8,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 10,
				State:       stateRegistered,
				SecretHash:  exampleHash,
			},
		},
		{
			// Cannot reset to voted.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        1,
			},
		},
		{
			// Admin script does not authorize.
			err: txscript.ErrStackScriptFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1, txscript.OP_DROP, txscript.OP_0},
				OptionCount: 3,
				State:       stateDistributed,
				SecretHash:  exampleHash,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1, txscript.OP_DROP, txscript.OP_0},
				OptionCount: 3,
				State:       stateDistributed + stateFinished,
				SecretHash:  exampleHash,
			},
		},
		{
			// Output has wrong voting right asset id.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       otherRightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Cannot change the base state at the same time.
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateDistributed | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateRegistered | stateFinished,
				SecretHash:  exampleHash,
				Vote:        2,
			},
		},
		{
			// Vote changed during closing
			err: txscript.ErrStackVerifyFailed,
			prev: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted,
				SecretHash:  exampleHash,
				Vote:        2,
			},
			out: tokenScriptData{
				Right:       rightAssetID,
				AdminScript: []byte{txscript.OP_1},
				OptionCount: 3,
				State:       stateVoted | stateFinished,
				SecretHash:  exampleHash,
				Vote:        3,
			},
		},
	}

	for i, tc := range testCases {
		sb := txscript.NewScriptBuilder()
		sb = sb.
			AddInt64(int64(tc.out.State)).
			AddData(tc.out.SecretHash[:]).
			AddInt64(int64(clauseReset)).
			AddData(tokenHoldingContract)
		sigscript, err := sb.Script()
		if err != nil {
			t.Fatal(err)
		}
		err = txscripttest.NewTestTx(mockTimeFunc).
			AddInput(tokensAssetAmount, tc.prev.PKScript(), sigscript).
			AddOutput(tokensAssetAmount, tc.out.PKScript()).
			Execute(ctx, 0)
		if !reflect.DeepEqual(err, tc.err) {
			t.Errorf("%d: got=%s want=%s", i, err, tc.err)
		}
	}
}

// TestTokenContractInvalidScript tests that testTokenContract correctly
// fails on pkscripts that are paid to the token contract but are
// improperly formatted.
func TestTokenContractInvalidMatch(t *testing.T) {
	testCaseScriptParams := [][][]byte{
		{ // no parameters
		},
		{ // not enough parameters
			[]byte{}, []byte{}, []byte{},
		},
		{ // enough parameters, but all empty
			[]byte{}, []byte{}, []byte{}, []byte{}, []byte{}, []byte{},
		},
		{ // asset id not long enough
			[]byte{0x01},                   // voting right asset id = 0x01
			[]byte{0xde, 0xad, 0xbe, 0xef}, // admin script = 0xdeadbeef
			[]byte{0x52},                   // option count = 2
			[]byte{byte(clauseRegister)},   // state = REGISTERED
			exampleHash[:],                 // secret hash = example hash
			[]byte{0x51},                   // vote = 1
		},
		{ // too many parameters
			exampleHash[:],                 // voting right asset id = example hash
			[]byte{0xde, 0xad, 0xbe, 0xef}, // admin script = 0xdeadbeef
			[]byte{0x52},                   // option count = 2
			[]byte{byte(clauseRegister)},   // state = REGISTERED
			exampleHash[:],                 // secret hash = example hash
			[]byte{0x51},                   // vote = 1
			[]byte{0x00, 0x01},             // garbage parameter
		},
	}

	for i, params := range testCaseScriptParams {
		addr := txscript.NewAddressContractHash(tokenHoldingContractHash[:], scriptVersion, params)
		script := addr.ScriptAddress()

		data, err := testTokenContract(script)
		if err != nil {
			t.Errorf("%d: testing token contract for %x: %s", i, script, err)
			continue
		}
		if data != nil {
			t.Errorf("%d: Match for pkscript %x generated from params: %#v", i, script, params)
			continue
		}
	}
}
