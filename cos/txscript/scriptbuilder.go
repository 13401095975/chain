// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txscript

import (
	"chain/cos/bc"
	"encoding/binary"
	"fmt"
)

const (
	// defaultScriptAlloc is the default size used for the backing array
	// for a script being built by the ScriptBuilder.  The array will
	// dynamically grow as needed, but this figure is intended to provide
	// enough space for vast majority of scripts without needing to grow the
	// backing array multiple times.
	defaultScriptAlloc = 500
)

// ErrScriptNotCanonical identifies a non-canonical script.  The caller can use
// a type assertion to detect this error type.
type ErrScriptNotCanonical string

// Error implements the error interface.
func (e ErrScriptNotCanonical) Error() string {
	return string(e)
}

// ScriptBuilder provides a facility for building custom scripts.  It allows
// you to push opcodes, ints, and data while respecting canonical encoding.  In
// general it does not ensure the script will execute correctly, however any
// data pushes which would exceed the maximum allowed script engine limits and
// are therefore guaranteed not to execute will not be pushed and will result in
// the Script function returning an error.
//
// For example, the following would build a 2-of-3 multisig script for usage in
// a pay-to-script-hash (although in this situation MultiSigScript() would be a
// better choice to generate the script):
// 	builder := txscript.NewScriptBuilder()
// 	builder.AddOp(txscript.OP_2).AddData(pubKey1).AddData(pubKey2)
// 	builder.AddData(pubKey3).AddOp(txscript.OP_3)
// 	builder.AddOp(txscript.OP_CHECKMULTISIG)
// 	script, err := builder.Script()
// 	if err != nil {
// 		// Handle the error.
// 		return
// 	}
// 	fmt.Printf("Final multi-sig script: %x\n", script)
type ScriptBuilder struct {
	script []byte
	err    error
}

// AddOp pushes the passed opcode to the end of the script.  The script will not
// be modified if pushing the opcode would cause the script to exceed the
// maximum allowed script engine size.
func (b *ScriptBuilder) AddOp(opcode byte) *ScriptBuilder {
	if b.err != nil {
		return b
	}
	newScript := append(b.script, opcode)
	if b.checkLen(len(newScript)) {
		b.script = newScript
	}
	return b
}

// canonicalDataSize returns the number of bytes the canonical encoding of the
// data will take.
func canonicalDataSize(data []byte) int {
	dataLen := len(data)

	// When the data consists of a single number that can be represented
	// by one of the "small integer" opcodes, that opcode will be instead
	// of a data push opcode followed by the number.
	if dataLen == 0 {
		return 1
	} else if dataLen == 1 && data[0] <= 16 {
		return 1
	} else if dataLen == 1 && data[0] == 0x81 {
		return 1
	}

	if dataLen < OP_PUSHDATA1 {
		return 1 + dataLen
	} else if dataLen <= 0xff {
		return 2 + dataLen
	} else if dataLen <= 0xffff {
		return 3 + dataLen
	}

	return 5 + dataLen
}

// AddDataToScript adds data to script using canonical opcodes.  A zero
// length buffer will lead to a push of empty data onto the stack (OP_0).
// No data limits are enforced with this function.
func AddDataToScript(script, data []byte) []byte {
	dataLen := len(data)

	// When the data consists of a single number that can be represented
	// by one of the "small integer" opcodes, use that opcode instead of
	// a data push opcode followed by the number.
	if dataLen == 0 || dataLen == 1 && data[0] == 0 {
		return append(script, OP_0)
	}
	if dataLen == 1 && data[0] <= 16 {
		return append(script, byte((OP_1-1)+data[0]))
	}
	if dataLen == 1 && data[0] == 0x81 {
		return append(script, byte(OP_1NEGATE))
	}

	// Use one of the OP_DATA_# opcodes if the length of the data is small
	// enough so the data push instruction is only a single byte.
	// Otherwise, choose the smallest possible OP_PUSHDATA# opcode that
	// can represent the length of the data.
	if dataLen < OP_PUSHDATA1 {
		script = append(script, byte((OP_DATA_1-1)+dataLen))
	} else if dataLen <= 0xff {
		script = append(script, OP_PUSHDATA1, byte(dataLen))
	} else if dataLen <= 0xffff {
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(dataLen))
		script = append(script, OP_PUSHDATA2)
		script = append(script, buf...)
	} else {
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(dataLen))
		script = append(script, OP_PUSHDATA4)
		script = append(script, buf...)
	}

	// Append the actual data.
	return append(script, data...)
}

// AddInt64ToScript canonically adds an integer to the script.  No length
// checks are performed.
func AddInt64ToScript(script []byte, val int64) []byte {
	// Fast path for small integers and OP_1NEGATE.
	if val == 0 {
		return append(script, OP_0)
	}
	if val == -1 || (val >= 1 && val <= 16) {
		return append(script, byte((OP_1-1)+val))
	}
	return AddDataToScript(script, scriptNum(val).Bytes())
}

// addData is the internal function that actually pushes the passed data to the
// end of the script.  It automatically chooses canonical opcodes depending on
// the length of the data.  A zero length buffer will lead to a push of empty
// data onto the stack (OP_0).  No data limits are enforced with this function.
func (b *ScriptBuilder) addData(data []byte) *ScriptBuilder {
	b.script = AddDataToScript(b.script, data)
	return b
}

// AddFullData should not typically be used by ordinary users as it does not
// include the checks which prevent data pushes larger than the maximum allowed
// sizes which leads to scripts that can't be executed.  This is provided for
// testing purposes such as regression tests where sizes are intentionally made
// larger than allowed.
//
// Use AddData instead.
func (b *ScriptBuilder) AddFullData(data []byte) *ScriptBuilder {
	if b.err != nil {
		return b
	}

	return b.addData(data)
}

// AddData pushes the passed data to the end of the script.  It automatically
// chooses canonical opcodes depending on the length of the data.  A zero length
// buffer will lead to a push of empty data onto the stack (OP_0) and any push
// of data greater than MaxScriptElementSize will not modify the script since
// that is not allowed by the script engine.  Also, the script will not be
// modified if pushing the data would cause the script to exceed the maximum
// allowed script engine size.
func (b *ScriptBuilder) AddData(data []byte) *ScriptBuilder {
	if b.err != nil {
		return b
	}

	// Pushes that would cause the script to exceed the largest allowed
	// script size would result in a non-canonical script.
	dataSize := canonicalDataSize(data)
	if !b.checkLen(len(b.script) + dataSize) {
		return b
	}

	// Pushes larger than the max script element size would result in a
	// script that is not canonical.
	dataLen := len(data)
	if dataLen > MaxScriptElementSize {
		str := fmt.Sprintf("adding a data element of %d bytes would "+
			"exceed the maximum allowed script element size of %d",
			dataLen, MaxScriptElementSize)
		b.err = ErrScriptNotCanonical(str)
		return b
	}

	return b.addData(data)
}

// AddInt64 pushes the passed integer to the end of the script.  The script will
// not be modified if pushing the data would cause the script to exceed the
// maximum allowed script engine size.
func (b *ScriptBuilder) AddInt64(val int64) *ScriptBuilder {
	if b.err != nil {
		return b
	}
	newScript := AddInt64ToScript(b.script, val)
	if b.checkLen(len(newScript)) {
		b.script = newScript
	}
	return b
}

// AddBool pushes the passed boolean to the end of the script.  If the boolean
// is true, a OP_1 will be pushed onto the stack.  If the boolean is false, a
// OP_0 will be pushed onto the stack.  The script will not be modified if
// pushing the data would cause the script to exceed the maximum allowed script
// engine size.
func (b *ScriptBuilder) AddBool(val bool) *ScriptBuilder {
	var v int64
	if val {
		v = 1
	}
	return b.AddInt64(v)
}

// ConcatRawScript makes the ability to add the pass script directly to
// an existing script to the test package.  This differs from AddData since it
// doesn't add a push data opcode.
func (b *ScriptBuilder) ConcatRawScript(data []byte) *ScriptBuilder {
	if b.err != nil {
		return b
	}

	b.script = append(b.script, data...)
	return b
}

type Item interface {
	AddTo(*ScriptBuilder) *ScriptBuilder
}

type (
	NumItem  int64
	DataItem []byte
	BoolItem bool
)

func (n NumItem) AddTo(sb *ScriptBuilder) *ScriptBuilder {
	return sb.AddInt64(int64(n))
}

func (d DataItem) AddTo(sb *ScriptBuilder) *ScriptBuilder {
	return sb.AddData([]byte(d))
}

func (b BoolItem) AddTo(sb *ScriptBuilder) *ScriptBuilder {
	return sb.AddBool(bool(b))
}

// Reset resets the script so it has no content.
func (b *ScriptBuilder) Reset() *ScriptBuilder {
	b.script = b.script[0:0]
	b.err = nil
	return b
}

// Script returns the currently built script.  When any errors occured while
// building the script, the script will be returned up the point of the first
// error along with the error.
func (b *ScriptBuilder) Script() ([]byte, error) {
	return b.script, b.err
}

func (b *ScriptBuilder) checkLen(l int) bool {
	if l > bc.MaxProgramByteLength {
		str := fmt.Sprintf("exceeded the maximum allowed canonical script length of %d", bc.MaxProgramByteLength)
		b.err = ErrScriptNotCanonical(str)
		return false
	}
	return true
}

// NewScriptBuilder returns a new instance of a script builder.  See
// ScriptBuilder for details.
func NewScriptBuilder() *ScriptBuilder {
	return &ScriptBuilder{
		script: make([]byte, 0, defaultScriptAlloc),
	}
}
