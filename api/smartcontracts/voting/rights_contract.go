package voting

import (
	"crypto/sha256"

	"chain/crypto/hash256"
	"chain/fedchain/bc"
	"chain/fedchain/txscript"
)

type rightsContractClause int64

const (
	clauseAuthenticate rightsContractClause = 1
	clauseTransfer                          = 2
	clauseDelegate                          = 3
	clauseRecall                            = 4
	clauseOverride                          = 5
	clauseCancel                            = 6
)

// rightScriptData encapsulates all the data stored within the p2c script
// for the voting rights holding contract.
type rightScriptData struct {
	HolderScript   []byte
	OwnershipChain bc.Hash
	Deadline       int64
	Delegatable    bool
}

// PKScript constructs a script address to pay into the holding
// contract for this voting right. It implements the txbuilder.Receiver
// interface.
func (r rightScriptData) PKScript() []byte {
	var (
		params [][]byte
	)

	params = append(params, txscript.BoolToScriptBytes(r.Delegatable))
	params = append(params, txscript.Int64ToScriptBytes(r.Deadline))
	params = append(params, r.OwnershipChain[:])
	params = append(params, r.HolderScript)

	addr := txscript.NewAddressContractHash(rightsHoldingContractHash[:], params)
	return addr.ScriptAddress()
}

// testRightsContract tests whether the given pkscript is a voting
// rights holding contract.
func testRightsContract(pkscript []byte) (*rightScriptData, error) {
	match, contractHash, params := txscript.TestPayToContract(pkscript)
	if !match {
		return nil, nil
	}
	if *contractHash != rightsHoldingContractHash {
		return nil, nil
	}
	if len(params) != 4 {
		return nil, nil
	}

	var (
		err   error
		right rightScriptData
	)

	// delegatable bool
	right.Delegatable = txscript.AsBool(params[0])

	// deadline in unix secs
	right.Deadline, err = txscript.AsInt64(params[1])
	if err != nil {
		return nil, err
	}

	// chain of ownership hash
	if cap(right.OwnershipChain) != len(params[2]) {
		return nil, nil
	}
	copy(right.OwnershipChain[:], params[2])

	// script identifying holder of the right
	right.HolderScript = make([]byte, len(params[3]))
	copy(right.HolderScript, params[3])

	return &right, nil
}

const (
	// rightsHoldingContractString contains the entire rights holding
	// contract script. For now, it's structured as a series of IF...ENDIF
	// clauses. In the future, we will use merkleized scripts, as documented in
	// the fedchain p2c documentation.
	//
	// This script with documentation and comments is available here:
	// https://gist.github.com/jbowens/ae16b535c856c137830e
	//
	// TODO(jackson): Include and eval admin script too.
	//
	// 1 - Authenticate (Unimplemented)
	// 2 - Transfer
	// 3 - Delegate     (Unimplemented)
	// 4 - Recall       (Unimplemented)
	// 5 - Override     (Unimplemented)
	// 6 - Cancel       (Unimplemented)
	rightsHoldingContractString = `
		2 DROP
		4 ROLL
		DUP 2 EQUAL IF
			DROP
			1 PICK
			TIME
			GREATERTHAN VERIFY
			DATA_2 0x5175
			5 ROLL CATPUSHDATA
			3 ROLL CATPUSHDATA
			2 ROLL CATPUSHDATA
			SWAP CATPUSHDATA
			OUTPUTSCRIPT
			DATA_1 0x27 RIGHT
			CAT
			AMOUNT ASSET 2 ROLL
			REQUIREOUTPUT VERIFY
			EVAL
		ENDIF
		DEPTH 1 EQUALVERIFY
	`
)

var (
	rightsHoldingContract     []byte
	rightsHoldingContractHash [sha256.Size]byte
)

func init() {
	var err error
	rightsHoldingContract, err = txscript.ParseScriptString(rightsHoldingContractString)
	if err != nil {
		panic("failed parsing voting rights holding script: " + err.Error())
	}
	// TODO(jackson): Before going to production, we'll probably want to hard-code the
	// contract hash and panic if the contract changes.
	rightsHoldingContractHash = hash256.Sum(rightsHoldingContract)
}
