package txvm2

import (
	"bytes"
	"encoding/binary"
	"io"

	"chain/crypto/sha3pool"
)

type item interface {
	typ() int
	encode(io.Writer)
}

type (
	vint64 int64
	vbytes []byte
	tuple  []item
)

const (
	int64type = 33
	bytestype = 34
	tupletype = 35
)

func (i vint64) typ() int { return int64type }
func (b vbytes) typ() int { return bytestype }
func (t tuple) typ() int  { return tupletype }

func (i vint64) encode(w io.Writer) {
	if i >= 0 && i <= vint64(MaxSmallInt) {
		w.Write([]byte{Op0 + byte(i)})
	} else {
		var buf [10]byte
		n := binary.PutVarint(buf[:], int64(i)) // xxx right?
		w.Write(pushdata(buf[:n]))
		w.Write([]byte{OpInt64})
	}
}

func (b vbytes) encode(w io.Writer) {
	w.Write(pushdata(b))
}

func (t tuple) encode(w io.Writer) {
	for i := len(t) - 1; i >= 0; i-- {
		t[i].encode(w)
	}
	vint64(len(t)).encode(w)
	w.Write([]byte{OpTuple})
}

func getTxID(v item) (txid [32]byte, ok bool) {
	if !isNamed(v, transactionTuple) {
		return txid, false
	}
	t := v.(tuple)
	// xxx check that len(t[1]) == len(txid)?
	b := t[1].(vbytes)
	copy(txid[:], b)
	return txid, true
}

func getID(v item) vbytes {
	hasher := sha3pool.Get256()
	defer sha3pool.Put256(hasher)

	hasher.Write([]byte("txvm"))
	v.encode(hasher)

	var hash [32]byte
	hasher.Read(hash[:])

	return vbytes(hash[:])
}

func encode(v item) []byte {
	b := new(bytes.Buffer)
	v.encode(b)
	return b.Bytes()
}

func pushdata(b []byte) []byte {
	// xxx
	return nil
}
