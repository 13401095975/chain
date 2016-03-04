package txscript

import (
	"chain/crypto/hash160"
	"chain/errors"
)

// RedeemToPkScript takes a redeem script
// and calculates its corresponding pk script
func RedeemToPkScript(redeem []byte) []byte {
	hash := hash160.Sum(redeem)
	builder := NewScriptBuilder()
	builder.AddOp(OP_DUP)
	builder.AddOp(OP_HASH160)
	builder.AddData(hash[:])
	builder.AddOp(OP_EQUALVERIFY)
	builder.AddOp(OP_EVAL)
	script, _ := builder.Script()
	return script
}

// RedeemScriptFromP2SHSigScript parses the signature script and returns the
// redeem script.
func RedeemScriptFromP2SHSigScript(sigScript []byte) ([]byte, error) {
	opCodes, err := parseScript(sigScript)
	if err != nil {
		return nil, errors.Wrap(err, "decoding redeem script from sig script")
	}

	return opCodes[len(opCodes)-1].data, nil
}

// TODO(tessr): Write BuildP2SHSigScript, which will correlate to
// RedeemScriptFromP2SHSigScript and will do something similar to
// asset.assembleSignatures.
