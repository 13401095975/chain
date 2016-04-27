package voting

import (
	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/api/txbuilder"
	"chain/cos/bc"
	"chain/cos/txscript"
	"chain/errors"
)

// RightIssuance builds a txbuilder Receiver issuance for an asset that
// is being issued into a voting right contract.
func RightIssuance(ctx context.Context, adminScript, holderScript []byte) txbuilder.Receiver {
	return rightScriptData{
		AdminScript:    adminScript,
		HolderScript:   holderScript,
		Delegatable:    true,
		Deadline:       infiniteDeadline,
		OwnershipChain: bc.Hash{},
	}
}

// RightAuthentication builds txbuilder Reserver and Receiver implementations
// for passing a voting right token through a transaction unchanged. The
// output voting right is identical to the input voting right. Its
// presence in the transaction proves voting right ownership during
// voting.
func RightAuthentication(ctx context.Context, src *RightWithUTXO) (txbuilder.Reserver, txbuilder.Receiver, error) {
	originalHolderAddr, err := appdb.GetAddress(ctx, src.HolderScript)
	if err != nil {
		holderScriptStr, _ := txscript.DisasmString(src.HolderScript)
		return nil, nil, errors.Wrapf(err, "could not get address for holder script [%s]", holderScriptStr)
	}

	reserver := rightsReserver{
		outpoint:   src.Outpoint,
		clause:     clauseAuthenticate,
		output:     src.rightScriptData, // unchanged
		prevScript: src.PKScript(),
		holderAddr: originalHolderAddr,
	}
	return reserver, reserver.output, nil
}

// RightTransfer builds txbuilder Reserver and Receiver implementations for
// a voting right transfer.
func RightTransfer(ctx context.Context, src *RightWithUTXO, newHolderScript []byte) (txbuilder.Reserver, txbuilder.Receiver, error) {
	currentHolderAddr, err := appdb.GetAddress(ctx, src.HolderScript)
	if err != nil {
		holderScriptStr, _ := txscript.DisasmString(src.HolderScript)
		return nil, nil, errors.Wrapf(err, "could not get address for holder script [%s]", holderScriptStr)
	}
	adminAddr, err := appdb.GetAddress(ctx, src.AdminScript)
	if err != nil {
		adminScriptStr, _ := txscript.DisasmString(src.AdminScript)
		return nil, nil, errors.Wrapf(err, "could not get address for admin script [%s]", adminScriptStr)
	}

	reserver := rightsReserver{
		outpoint: src.Outpoint,
		clause:   clauseTransfer,
		output: rightScriptData{
			AdminScript:    src.AdminScript, // unchanged
			HolderScript:   newHolderScript,
			Delegatable:    src.Delegatable,    // unchanged
			Deadline:       src.Deadline,       // unchanged
			OwnershipChain: src.OwnershipChain, // unchanged
		},
		prevScript: src.PKScript(),
		holderAddr: currentHolderAddr,
		adminAddr:  adminAddr,
	}
	return reserver, reserver.output, nil
}

// RightDelegation builds txbuilder Reserver and Receiver implementations for
// delegating a voting right to another party.
func RightDelegation(ctx context.Context, src *RightWithUTXO, newHolderScript []byte, newDeadline int64, delegatable bool) (txbuilder.Reserver, txbuilder.Receiver, error) {
	currentHolderAddr, err := appdb.GetAddress(ctx, src.HolderScript)
	if err != nil {
		holderScriptStr, _ := txscript.DisasmString(src.HolderScript)
		return nil, nil, errors.Wrapf(err, "could not get address for holder script [%s]", holderScriptStr)
	}
	adminAddr, err := appdb.GetAddress(ctx, src.AdminScript)
	if err != nil {
		adminScriptStr, _ := txscript.DisasmString(src.AdminScript)
		return nil, nil, errors.Wrapf(err, "could not get address for admin script [%s]", adminScriptStr)
	}

	reserver := rightsReserver{
		outpoint: src.Outpoint,
		clause:   clauseDelegate,
		output: rightScriptData{
			AdminScript:  src.AdminScript,
			HolderScript: newHolderScript,
			Delegatable:  delegatable,
			Deadline:     newDeadline,
			OwnershipChain: calculateOwnershipChain(
				src.OwnershipChain,
				src.HolderScript,
				src.Deadline,
			),
		},
		prevScript: src.PKScript(),
		holderAddr: currentHolderAddr,
		adminAddr:  adminAddr,
	}
	return reserver, reserver.output, nil
}

// RightRecall builds txbuilder Reserver and Receiver implementations for
// a voting right recall.
func RightRecall(ctx context.Context, src, recallPoint *RightWithUTXO, intermediaryRights []*RightWithUTXO) (txbuilder.Reserver, txbuilder.Receiver, error) {
	originalHolderAddr, err := appdb.GetAddress(ctx, recallPoint.HolderScript)
	if err != nil {
		holderScriptStr, _ := txscript.DisasmString(recallPoint.HolderScript)
		return nil, nil, errors.Wrapf(err, "could not get address for holder script [%s]", holderScriptStr)
	}

	intermediaries := make([]intermediateHolder, 0, len(intermediaryRights))
	for _, r := range intermediaryRights {
		intermediaries = append(intermediaries, intermediateHolder{
			script:   r.HolderScript,
			deadline: r.Deadline,
		})
	}

	reserver := rightsReserver{
		outpoint:       src.Outpoint,
		clause:         clauseRecall,
		output:         recallPoint.rightScriptData,
		intermediaries: intermediaries,
		prevScript:     src.PKScript(),
		holderAddr:     originalHolderAddr,
	}
	return reserver, reserver.output, nil
}

// TokenIssuance builds a txbuilder Receiver implementation
// for a voting token issuance.
func TokenIssuance(ctx context.Context, rightAssetID bc.AssetID, admin []byte, optionCount int64, secretHash bc.Hash) txbuilder.Receiver {
	scriptData := tokenScriptData{
		Right:       rightAssetID,
		AdminScript: admin,
		OptionCount: optionCount,
		State:       stateDistributed,
		SecretHash:  secretHash,
		Vote:        0,
	}
	return scriptData
}

// TokenIntent builds txbuilder Reserver and Receiver implementations
// for a voting token intent-to-vote transition.
func TokenIntent(ctx context.Context, token *Token, right txbuilder.Receiver) (txbuilder.Reserver, txbuilder.Receiver, error) {
	prevScript := token.tokenScriptData.PKScript()
	intended := token.tokenScriptData
	intended.State = stateIntended

	reserver := tokenReserver{
		outpoint:    token.Outpoint,
		clause:      clauseIntendToVote,
		output:      intended,
		prevScript:  prevScript,
		rightScript: right.PKScript(),
	}
	return reserver, intended, nil
}
