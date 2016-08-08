package bc

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"

	"golang.org/x/crypto/sha3"

	"chain/encoding/blockchain"
	"chain/errors"
)

const (
	// CurrentTransactionVersion is the current latest
	// supported transaction version.
	CurrentTransactionVersion = 1

	VMVersion = 1
)

const (
	metadataMaxByteLength = 500000 // 500 kb
	witnessMaxByteLength  = 500000 // 500 kb
)

// Tx holds a transaction along with its hash.
type Tx struct {
	TxData
	Hash Hash
}

func (tx *Tx) UnmarshalText(p []byte) error {
	if err := tx.TxData.UnmarshalText(p); err != nil {
		return err
	}

	tx.Hash = tx.TxData.Hash()
	return nil
}

// NewTx returns a new Tx containing data and its hash.
// If you have already computed the hash, use struct literal
// notation to make a Tx object directly.
func NewTx(data TxData) *Tx {
	return &Tx{
		TxData: data,
		Hash:   data.Hash(),
	}
}

// These flags are part of the wire protocol;
// they must not change.
const (
	SerWitness uint8 = 1 << iota
	SerPrevout
	SerMetadata

	// Bit mask for accepted serialization flags.
	// All other flag bits must be 0.
	SerValid    = 0x7
	serRequired = 0x7 // we support only this combination of flags
)

// TxData encodes a transaction in the blockchain.
// Most users will want to use Tx instead;
// it includes the hash.
type TxData struct {
	Version  uint32
	Inputs   []*TxInput
	Outputs  []*TxOutput
	MinTime  uint64
	MaxTime  uint64
	Metadata []byte
}

// Outpoint defines a bitcoin data type that is used to track previous
// transaction outputs.
type Outpoint struct {
	Hash  Hash   `json:"hash"`
	Index uint32 `json:"index"`
}

func NewOutpoint(b []byte, index uint32) *Outpoint {
	result := &Outpoint{Index: index}
	copy(result.Hash[:], b)
	return result
}

// HasIssuance returns true if this transaction has an issuance input.
func (tx *TxData) HasIssuance() bool {
	for _, in := range tx.Inputs {
		if in.IsIssuance() {
			return true
		}
	}
	return false
}

func (tx *TxData) UnmarshalText(p []byte) error {
	b := make([]byte, hex.DecodedLen(len(p)))
	_, err := hex.Decode(b, p)
	if err != nil {
		return err
	}
	return tx.readFrom(bytes.NewReader(b))
}

func (tx *TxData) Scan(val interface{}) error {
	b, ok := val.([]byte)
	if !ok {
		return errors.New("Scan must receive a byte slice")
	}
	return tx.readFrom(bytes.NewReader(b))
}

func (tx *TxData) Value() (driver.Value, error) {
	b := new(bytes.Buffer)
	_, err := tx.WriteTo(b)
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// assumes r has sticky errors
func (tx *TxData) readFrom(r io.Reader) error {
	var serflags [1]byte
	_, err := io.ReadFull(r, serflags[:])
	if err == nil && serflags[0] != serRequired {
		return fmt.Errorf("unsupported serflags %#x", serflags[0])
	}

	v, _ := blockchain.ReadUvarint(r)
	tx.Version = uint32(v)

	for n, _ := blockchain.ReadUvarint(r); n > 0; n-- {
		ti := new(TxInput)
		err = ti.readFrom(r)
		if err != nil {
			return err
		}
		tx.Inputs = append(tx.Inputs, ti)
	}

	for n, _ := blockchain.ReadUvarint(r); n > 0; n-- {
		to := new(TxOutput)
		err = to.readFrom(r)
		if err != nil {
			return err
		}
		tx.Outputs = append(tx.Outputs, to)
	}

	tx.MinTime, _ = blockchain.ReadUvarint(r)
	tx.MaxTime, _ = blockchain.ReadUvarint(r)
	tx.Metadata, err = blockchain.ReadBytes(r, metadataMaxByteLength)
	return err
}

// assumes r has sticky errors
func (p *Outpoint) readFrom(r io.Reader) {
	io.ReadFull(r, p.Hash[:])
	index, _ := blockchain.ReadUvarint(r)
	p.Index = uint32(index)
}

// Hash computes the hash of the transaction with metadata fields
// replaced by their hashes,
// and stores the result in Hash.
func (tx *TxData) Hash() Hash {
	h := sha3.New256()
	tx.writeTo(h, 0) // error is impossible
	var v Hash
	h.Sum(v[:0])
	return v
}

// WitnessHash is the combined hash of the
// transactions hash and signature data hash.
// It is used to compute the TxRoot of a block.
func (tx *TxData) WitnessHash() Hash {
	var b bytes.Buffer

	txhash := tx.Hash()
	b.Write(txhash[:])

	blockchain.WriteUvarint(&b, uint64(len(tx.Inputs)))
	for _, txin := range tx.Inputs {
		h := txin.WitnessHash()
		b.Write(h[:])
	}

	blockchain.WriteUvarint(&b, uint64(len(tx.Outputs)))
	for _, txout := range tx.Outputs {
		h := txout.WitnessHash()
		b.Write(h[:])
	}

	return sha3.Sum256(b.Bytes())
}

// HashForSig generates the hash required for the specified input's
// signature.
func (tx *TxData) HashForSig(idx int, hashType SigHashType) Hash {
	return NewSigHasher(tx).Hash(idx, hashType)
}

type SigHasher struct {
	tx             *TxData
	inputsHash     *Hash
	allOutputsHash *Hash
}

func NewSigHasher(tx *TxData) *SigHasher {
	return &SigHasher{tx: tx}
}

func (s *SigHasher) writeInput(w io.Writer, idx int) {
	s.tx.Inputs[idx].writeTo(w, 0)
}

func (s *SigHasher) writeOutput(w io.Writer, idx int) {
	s.tx.Outputs[idx].writeTo(w, 0)
}

// Use only when hashtype is not "anyone can pay"
func (s *SigHasher) getInputsHash() *Hash {
	if s.inputsHash == nil {
		var hash Hash
		h := sha3.New256()
		w := errors.NewWriter(h)

		blockchain.WriteUvarint(w, uint64(len(s.tx.Inputs)))
		for i := 0; i < len(s.tx.Inputs); i++ {
			s.writeInput(w, i)
		}
		h.Sum(hash[:0])
		s.inputsHash = &hash
	}
	return s.inputsHash
}

func (s *SigHasher) getAllOutputsHash() *Hash {
	if s.allOutputsHash == nil {
		var hash Hash
		h := sha3.New256()
		w := errors.NewWriter(h)
		blockchain.WriteUvarint(w, uint64(len(s.tx.Outputs)))
		for i := 0; i < len(s.tx.Outputs); i++ {
			s.writeOutput(w, i)
		}
		h.Sum(hash[:0])
		s.allOutputsHash = &hash
	}
	return s.allOutputsHash
}

func (s *SigHasher) Hash(idx int, hashType SigHashType) (hash Hash) {
	var inputsHash *Hash
	if hashType&SigHashAnyOneCanPay == 0 {
		inputsHash = s.getInputsHash()
	} else {
		inputsHash = &Hash{}
	}

	var outputCommitment []byte
	if !s.tx.Inputs[idx].IsIssuance() {
		var buf bytes.Buffer
		assetAmount := s.tx.Inputs[idx].AssetAmount()
		buf.Write(assetAmount.AssetID[:])
		blockchain.WriteUvarint(&buf, assetAmount.Amount)
		blockchain.WriteUvarint(&buf, VMVersion)
		blockchain.WriteBytes(&buf, s.tx.Inputs[idx].ControlProgram())
		outputCommitment = buf.Bytes()
	}

	var outputsHash *Hash
	switch hashType & sigHashMask {
	case SigHashAll:
		outputsHash = s.getAllOutputsHash()
	case SigHashNone:
		outputsHash = &Hash{}
	case SigHashSingle:
		if idx >= len(s.tx.Outputs) {
			outputsHash = &Hash{}
		} else {
			h := sha3.New256()
			w := errors.NewWriter(h)
			blockchain.WriteUvarint(w, 1)
			s.writeOutput(w, idx)
			var hash Hash
			h.Sum(hash[:0])
			outputsHash = &hash
		}
	default:
		return Hash{}
	}

	h := sha3.New256()
	w := errors.NewWriter(h)
	blockchain.WriteUvarint(w, uint64(s.tx.Version))
	w.Write(inputsHash[:])
	s.writeInput(w, idx)
	blockchain.WriteBytes(w, outputCommitment)
	w.Write(outputsHash[:])
	blockchain.WriteUvarint(w, s.tx.MinTime)
	blockchain.WriteUvarint(w, s.tx.MaxTime)
	writeMetadata(w, s.tx.Metadata, 0)
	w.Write([]byte{byte(hashType)})

	h.Sum(hash[:0])
	return hash
}

// MarshalText satisfies blockchain.TextMarshaller interface
func (tx *TxData) MarshalText() ([]byte, error) {
	var buf bytes.Buffer
	tx.WriteTo(&buf) // error is impossible
	b := make([]byte, hex.EncodedLen(buf.Len()))
	hex.Encode(b, buf.Bytes())
	return b, nil
}

// WriteTo writes tx to w.
func (tx *TxData) WriteTo(w io.Writer) (int64, error) {
	ew := errors.NewWriter(w)
	tx.writeTo(ew, serRequired)
	return ew.Written(), ew.Err()
}

// assumes w has sticky errors
func (tx *TxData) writeTo(w io.Writer, serflags byte) {
	w.Write([]byte{serflags})
	blockchain.WriteUvarint(w, uint64(tx.Version))

	blockchain.WriteUvarint(w, uint64(len(tx.Inputs)))
	for _, ti := range tx.Inputs {
		ti.writeTo(w, serflags)
	}

	blockchain.WriteUvarint(w, uint64(len(tx.Outputs)))
	for _, to := range tx.Outputs {
		to.writeTo(w, serflags)
	}

	blockchain.WriteUvarint(w, tx.MinTime)
	blockchain.WriteUvarint(w, tx.MaxTime)
	writeMetadata(w, tx.Metadata, serflags)
}

// String returns the Outpoint in the human-readable form "hash:index".
func (p Outpoint) String() string {
	return p.Hash.String() + ":" + strconv.FormatUint(uint64(p.Index), 10)
}

// WriteTo writes p to w.
// It assumes w has sticky errors.
func (p Outpoint) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(p.Hash[:])
	if err != nil {
		return int64(n), err
	}
	u, err := blockchain.WriteUvarint(w, uint64(p.Index))
	return int64(n + u), err
}

type AssetAmount struct {
	AssetID AssetID `json:"asset_id"`
	Amount  uint64  `json:"amount"`
}

// assumes r has sticky errors
func (a *AssetAmount) readFrom(r io.Reader) {
	io.ReadFull(r, a.AssetID[:])
	a.Amount, _ = blockchain.ReadUvarint(r)
}

// assumes w has sticky errors
func (a AssetAmount) writeTo(w io.Writer) {
	w.Write(a.AssetID[:])
	blockchain.WriteUvarint(w, a.Amount)
}

// assumes w has sticky errors
func writeMetadata(w io.Writer, data []byte, serflags byte) {
	if serflags&SerMetadata != 0 {
		blockchain.WriteBytes(w, data)
	} else {
		h := fastHash(data)
		blockchain.WriteBytes(w, h)
	}
}
