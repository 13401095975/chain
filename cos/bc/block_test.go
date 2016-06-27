package bc

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestEmptyBlock(t *testing.T) {
	block := Block{
		BlockHeader: BlockHeader{
			Version: NewBlockVersion,
		},
	}

	got := serialize(t, &block)
	want, _ := hex.DecodeString("0100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if !bytes.Equal(got, want) {
		t.Errorf("empty block bytes = %x want %x", got, want)
	}

	got = serialize(t, &block.BlockHeader)
	want, _ = hex.DecodeString("01000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if !bytes.Equal(got, want) {
		t.Errorf("empty block header bytes = %x want %x", got, want)
	}

	wantHash := mustDecodeHash("55a2e2cff0fa4b44bca37badd5c2ed7d1414bc1c9532a20d9dcd6b4abf101345")
	if h := block.Hash(); h != wantHash {
		t.Errorf("empty block has incorrect hash %s", h)
	}
	if h := block.HashForSig(); h != wantHash {
		t.Errorf("empty block has incorrect sig hash %s", h)
	}

	wTime := time.Unix(0, 0).UTC()
	if got := block.Time(); got != wTime {
		t.Errorf("empty block time = %v want %v", got, wTime)
	}
}

func TestSmallBlock(t *testing.T) {
	block := Block{
		BlockHeader: BlockHeader{
			Version: NewBlockVersion,
		},
		Transactions: []*Tx{NewTx(TxData{SerFlags: 0x7, Version: CurrentTransactionVersion})},
	}

	got := serialize(t, &block)
	want, _ := hex.DecodeString("010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000107010000000000000000000000000000")
	if !bytes.Equal(got, want) {
		t.Errorf("small block bytes = %x want %x", got, want)
	}
}
