package txbuilder

import (
	"context"
	"encoding/json"

	"golang.org/x/crypto/sha3"

	"chain/crypto/ed25519"
	chainjson "chain/encoding/json"
	"chain/errors"
	"chain/protocol/bc"
	"chain/protocol/vm"
	"chain/protocol/vmutil"
)

// WitnessComponent encodes instructions for finalizing a transaction
// by populating its InputWitness fields. Each WitnessComponent object
// produces zero or more items for the InputWitness of the txinput it
// corresponds to.
type WitnessComponent interface {
	// Sign is called to add signatures. Actual signing is delegated to
	// a callback function.
	Sign(context.Context, *Template, int, func(context.Context, string, []uint32, [32]byte) ([]byte, error)) error

	// Materialize is called to turn the component into a vector of
	// arguments for the input witness.
	Materialize(*Template, int) ([][]byte, error)
}

// materializeWitnesses takes a filled in Template and "materializes"
// each witness component, turning it into a vector of arguments for
// the tx's input witness, creating a fully-signed transaction.
func materializeWitnesses(txTemplate *Template) error {
	msg := txTemplate.Transaction

	if msg == nil {
		return errors.Wrap(ErrMissingRawTx)
	}

	if len(txTemplate.Inputs) > len(msg.Inputs) {
		return errors.Wrap(ErrBadInputCount)
	}

	for i, input := range txTemplate.Inputs {
		if msg.Inputs[input.Position] == nil {
			return errors.WithDetailf(ErrBadTxInputIdx, "input %d references missing tx input %d", i, input.Position)
		}

		var witness [][]byte
		for j, c := range input.WitnessComponents {
			items, err := c.Materialize(txTemplate, i)
			if err != nil {
				return errors.WithDetailf(err, "error in witness component %d of input %d", j, i)
			}
			witness = append(witness, items...)
		}

		msg.Inputs[input.Position].InputWitness = witness
	}

	return nil
}

type DataWitness []byte

func (_ DataWitness) Sign(_ context.Context, _ *Template, _ int, _ func(context.Context, string, []uint32, [32]byte) ([]byte, error)) error {
	return nil
}

func (d DataWitness) Materialize(_ *Template, _ int) ([][]byte, error) {
	return [][]byte{d}, nil
}

func (d DataWitness) MarshalJSON() ([]byte, error) {
	obj := struct {
		Type string             `json:"type"`
		Data chainjson.HexBytes `json:"data"`
	}{
		Type: "data",
		Data: chainjson.HexBytes(d),
	}
	return json.Marshal(obj)
}

type (
	SignatureWitness struct {
		// Quorum is the number of signatures required.
		Quorum int `json:"quorum"`

		// Keys are the identities of the keys to sign with.
		Keys []KeyID `json:"keys"`

		// Program is the deferred predicate, whose hash is what gets
		// signed. If empty, it is computed during Sign from the outputs
		// and the current input of the transaction.
		Program chainjson.HexBytes `json:"program"`

		// Sigs are signatures of Program made from each of the Keys
		// during Sign.
		Sigs []chainjson.HexBytes `json:"signatures"`
	}

	KeyID struct {
		XPub           string   `json:"xpub"`
		DerivationPath []uint32 `json:"derivation_path"`
	}
)

var ErrEmptyProgram = errors.New("empty signature program")

// Sign populates sw.Sigs with as many signatures of the deferred predicate in
// sw.Program as it can from the set of keys in sw.Keys.
//
// If sw.Program is empty, it is populated with an _inferred_ deferred
// predicate: a program committing to aspects of the current
// transaction. Specifically, the program commits to:
//  - the maxtime of the transaction
//  - the outpoint and reference data of the current input
//  - the assetID, amount, reference data, and control program of each output.
func (sw *SignatureWitness) Sign(ctx context.Context, tpl *Template, index int, signFn func(context.Context, string, []uint32, [32]byte) ([]byte, error)) error {
	// Compute the deferred predicate to sign. This is either a
	// txsighash program if tpl.Final is true (i.e., the tx is complete
	// and no further changes are allowed) or a program enforcing
	// constraints derived from the existing outputs and current input.
	if len(sw.Program) == 0 {
		sw.Program = buildSigProgram(tpl, index)
		if len(sw.Program) == 0 {
			return ErrEmptyProgram
		}
	}
	if len(sw.Sigs) < len(sw.Keys) {
		// Each key in sw.Keys will produce a signature in sw.Sigs. Make
		// sure there are enough slots in sw.Sigs and that we preserve any
		// sigs already present.
		newSigs := make([]chainjson.HexBytes, len(sw.Keys))
		copy(newSigs, sw.Sigs)
		sw.Sigs = newSigs
	}
	h := sha3.Sum256(sw.Program)
	for i, keyID := range sw.Keys {
		if len(sw.Sigs[i]) > 0 {
			// Already have a signature for this key
			continue
		}
		sigBytes, err := signFn(ctx, keyID.XPub, keyID.DerivationPath, h)
		if err != nil {
			return errors.WithDetailf(err, "computing signature %d", i)
		}
		sw.Sigs[i] = sigBytes
	}
	return nil
}

func buildSigProgram(tpl *Template, index int) []byte {
	if tpl.Final {
		h := tpl.Hash(index, bc.SigHashAll)
		builder := vmutil.NewBuilder()
		builder.AddData(h[:])
		builder.AddInt64(1).AddOp(vm.OP_TXSIGHASH).AddOp(vm.OP_EQUAL)
		return builder.Program
	}
	constraints := make([]constraint, 0, 3+len(tpl.Transaction.Outputs))
	if tpl.Transaction.MaxTime > 0 {
		constraints = append(constraints, ttlConstraint(tpl.Transaction.MaxTime))
	}
	inp := tpl.Transaction.Inputs[index]
	if !inp.IsIssuance() {
		constraints = append(constraints, outpointConstraint(inp.Outpoint()))
	}
	if len(inp.ReferenceData) > 0 {
		constraints = append(constraints, refdataConstraint(inp.ReferenceData))
	}
	for _, out := range tpl.Transaction.Outputs {
		c := &payConstraint{
			AssetAmount: out.AssetAmount,
			Program:     out.ControlProgram,
		}
		if len(out.ReferenceData) > 0 {
			h := sha3.Sum256(out.ReferenceData)
			c.RefDataHash = (*bc.Hash)(&h)
		}
		constraints = append(constraints, c)
	}
	var program []byte
	for i, c := range constraints {
		program = append(program, c.code()...)
		if i < len(constraints)-1 { // leave the final bool on top of the stack
			program = append(program, byte(vm.OP_VERIFY))
		}
	}
	return program
}

func (sw SignatureWitness) Materialize(tpl *Template, index int) ([][]byte, error) {
	input := tpl.Transaction.Inputs[index]
	var multiSig []byte
	if input.IsIssuance() {
		multiSig = input.IssuanceProgram()
	} else {
		multiSig = input.ControlProgram()
	}
	pubkeys, quorum, err := vmutil.ParseP2DPMultiSigProgram(multiSig)
	if err != nil {
		return nil, errors.Wrap(err, "parsing input program script")
	}
	var sigs [][]byte
	h := sha3.Sum256(sw.Program)
	for i := 0; i < len(pubkeys) && len(sigs) < quorum; i++ {
		k := indexSig(pubkeys[i], h[:], sw.Sigs)
		if k >= 0 {
			sigs = append(sigs, sw.Sigs[k])
		}
	}
	return append(sigs, sw.Program), nil
}

func indexSig(key ed25519.PublicKey, msg []byte, sigs []chainjson.HexBytes) int {
	for i, sig := range sigs {
		if ed25519.Verify(key, msg, sig) {
			return i
		}
	}
	return -1
}

func (sw SignatureWitness) MarshalJSON() ([]byte, error) {
	obj := struct {
		Type   string               `json:"type"`
		Quorum int                  `json:"quorum"`
		Keys   []KeyID              `json:"keys"`
		Sigs   []chainjson.HexBytes `json:"signatures"`
	}{
		Type:   "signature",
		Quorum: sw.Quorum,
		Keys:   sw.Keys,
		Sigs:   sw.Sigs,
	}
	return json.Marshal(obj)
}

func (inp *Input) AddWitnessData(data []byte) {
	inp.WitnessComponents = append(inp.WitnessComponents, DataWitness(data))
}

func (inp *Input) AddWitnessKeys(keys []KeyID, quorum int) {
	sw := &SignatureWitness{
		Quorum: quorum,
		Keys:   keys,
	}
	inp.WitnessComponents = append(inp.WitnessComponents, sw)
}
