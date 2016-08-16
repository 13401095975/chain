package validation

import (
	"testing"

	"chain/cos/bc"
	"chain/cos/txscript"
	"chain/errors"
)

func TestValidateBlockHeader(t *testing.T) {
	prevBlock := &bc.Block{
		BlockHeader: bc.BlockHeader{
			Height:           1,
			TimestampMS:      5,
			ConsensusProgram: []byte{txscript.OP_5, txscript.OP_ADD, txscript.OP_9, txscript.OP_EQUAL},
		},
	}
	prevHash := prevBlock.Hash()
	cases := []struct {
		desc   string
		header bc.BlockHeader
		want   error
	}{{
		desc: "bad prev block hash",
		header: bc.BlockHeader{
			PreviousBlockHash: bc.Hash{},
			Height:            2,
		},
		want: ErrBadPrevHash,
	}, {
		desc: "bad block height",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            3,
		},
		want: ErrBadHeight,
	}, {
		desc: "bad block timestamp",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            2,
			TimestampMS:       3,
		},
		want: ErrBadTimestamp,
	}, {
		desc: "fake genesis block",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            1,
			TimestampMS:       6,
			ConsensusProgram:  []byte{txscript.OP_5, txscript.OP_ADD, txscript.OP_9, txscript.OP_EQUAL},
			Witness:           [][]byte{{0x04}},
		},
		want: ErrBadHeight,
	}, {
		desc: "bad block output script",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            2,
			TimestampMS:       6,
			ConsensusProgram:  []byte{txscript.OP_RETURN},
		},
		want: ErrBadScript,
	}, {
		desc: "bad block signature script",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            2,
			TimestampMS:       6,
			ConsensusProgram:  []byte{txscript.OP_5, txscript.OP_ADD, txscript.OP_9, txscript.OP_EQUAL},
			Witness:           [][]byte{{0x03}},
		},
		want: ErrBadSig,
	}, {
		desc: "valid header",
		header: bc.BlockHeader{
			PreviousBlockHash: prevHash,
			Height:            2,
			TimestampMS:       6,
			ConsensusProgram:  []byte{txscript.OP_5, txscript.OP_ADD, txscript.OP_9, txscript.OP_EQUAL},
			Witness:           [][]byte{{0x04}},
		},
		want: nil,
	}}
	for _, c := range cases {
		block := &bc.Block{BlockHeader: c.header}
		got := ValidateBlockHeader(prevBlock, block)
		if errors.Root(got) != c.want {
			t.Errorf("%s: got %q want %q", c.desc, got, c.want)
		}
	}
}
