package txbuilder

import (
	"context"
	"encoding/json"

	chainjson "chain/encoding/json"
	"chain/errors"
	"chain/protocol/bc"
)

// Template represents a partially- or fully-signed transaction.
type Template struct {
	Unsigned *bc.TxData `json:"unsigned_hex"`
	Inputs   []*Input   `json:"inputs"`

	// Local indicates that all inputs to the transaction are signed
	// exclusively by keys managed by this Core. Whenever accepting
	// a template from an external Core, `Local` should be set to
	// false.
	Local bool `json:"local"`

	sigHasher *bc.SigHasher
}

func (t *Template) Hash(idx int, hashType bc.SigHashType) bc.Hash {
	if t.sigHasher == nil {
		t.sigHasher = bc.NewSigHasher(t.Unsigned)
	}
	return t.sigHasher.Hash(idx, hashType)
}

// Input is an input for a TxTemplate.
type Input struct {
	bc.AssetAmount
	Position          uint32             `json:"position"`
	WitnessComponents []WitnessComponent `json:"witness_components,omitempty"`
}

func (inp *Input) UnmarshalJSON(b []byte) error {
	var pre struct {
		bc.AssetAmount
		Position          uint32            `json:"position"`
		WitnessComponents []json.RawMessage `json:"witness_components"`
	}
	err := json.Unmarshal(b, &pre)
	if err != nil {
		return err
	}
	inp.AssetAmount = pre.AssetAmount
	inp.Position = pre.Position

	for i, w := range pre.WitnessComponents {
		var t struct {
			Type string `json:"type"`
		}
		err = json.Unmarshal(w, &t)
		if err != nil {
			return err
		}
		switch t.Type {
		case "data":
			var d struct {
				Data chainjson.HexBytes `json:"data"`
			}
			err = json.Unmarshal(w, &d)
			if err != nil {
				return err
			}
			inp.WitnessComponents = append(inp.WitnessComponents, DataWitness(d.Data))
		case "signature":
			var s SignatureWitness
			err = json.Unmarshal(w, &s)
			if err != nil {
				return err
			}
			inp.WitnessComponents = append(inp.WitnessComponents, &s)
		default:
			return errors.WithDetailf(ErrBadWitnessComponent, "witness component %d has unknown type '%s'", i, t.Type)
		}
	}
	return nil
}

type Action interface {
	Build(context.Context) ([]*bc.TxInput, []*bc.TxOutput, []*Input, error)
}
