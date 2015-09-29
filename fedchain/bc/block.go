package bc

import (
	"encoding/binary"
	"io"
	"time"

	"chain/crypto/hash256"
	"chain/errors"
	"chain/fedchain/script"
)

// Block describes a complete block, including its header
// and the transactions it contains.
type Block struct {
	BlockHeader
	Transactions []*Tx
}

// WriteTo satisfies interface io.WriterTo.
func (b *Block) WriteTo(w io.Writer) (int64, error) {
	return b.writeTo(w, false)
}

func (b *Block) writeTo(w io.Writer, forSigning bool) (int64, error) {
	ew := errors.NewWriter(w)
	b.BlockHeader.writeTo(ew, forSigning)
	if !forSigning {
		writeUvarint(ew, uint64(len(b.Transactions)))
		for _, tx := range b.Transactions {
			tx.WriteTo(ew)
		}
	}
	return ew.Written(), ew.Err()
}

// Block version to use when creating new blocks.
const NewBlockVersion = 1

// BlockHeader describes necessary metadata of the block.
type BlockHeader struct {
	// Version of the block.
	Version uint32

	// Height of the block in the block chain.
	// Genesis block has height 0.
	Height uint64

	// Hash of the previous block in the block chain.
	PreviousBlockHash [32]byte

	// Root of the block's transactions merkle tree.
	TxRoot [32]byte

	// Root of the state merkle tree after applying
	// transactions in the block.
	StateRoot [32]byte

	// Time of the block in seconds.
	// Must grow monotonically and can be equal
	// to the time in the previous block.
	Timestamp uint64

	// Signature script authenticates the block against
	// the output script from the previous block.
	SignatureScript script.Script

	// Output script specifies a predicate for signing the next block.
	OutputScript script.Script
}

// Time returns the time represented by the Timestamp in bh.
func (bh *BlockHeader) Time() time.Time {
	return time.Unix(int64(bh.Timestamp), 0).UTC()
}

// Hash returns complete hash of the block header.
func (bh *BlockHeader) Hash() Hash {
	h := hash256.New()
	bh.WriteTo(h) // error is impossible
	var v [32]byte
	h.Sum(v[:0])
	return v
}

// HashForSig returns a hash of the block header with signature script blanked out.
// This hash is used for signing the block and verifying the signature.
func (bh *BlockHeader) HashForSig() Hash {
	h := hash256.New()
	bh.WriteForSigTo(h) // error is impossible
	var v [32]byte
	h.Sum(v[:0])
	return v
}

// WriteTo satisfies interface io.WriterTo.
func (bh *BlockHeader) WriteTo(w io.Writer) (int64, error) {
	ew := errors.NewWriter(w)
	bh.writeTo(ew, false)
	return ew.Written(), ew.Err()
}

// WriteForSigTo writes bh to w in a format suitable for signing.
func (bh *BlockHeader) WriteForSigTo(w io.Writer) (int64, error) {
	ew := errors.NewWriter(w)
	bh.writeTo(ew, true)
	return ew.Written(), ew.Err()
}

// writeTo writes bh to w.
// If forSigning is true, it writes an empty string instead of the signature script.
func (bh *BlockHeader) writeTo(w *errors.Writer, forSigning bool) {
	binary.Write(w, endianness, bh.Version)
	binary.Write(w, endianness, bh.Height)
	w.Write(bh.PreviousBlockHash[:])
	w.Write(bh.TxRoot[:])
	w.Write(bh.StateRoot[:])
	binary.Write(w, endianness, bh.Timestamp)
	if forSigning {
		writeBytes(w, nil)
	} else {
		writeBytes(w, bh.SignatureScript)
	}
	writeBytes(w, bh.OutputScript)
}
