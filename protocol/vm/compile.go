package vm

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"chain/errors"
)

// Compile converts a string like "2 3 ADD 5 NUMEQUAL" into 0x525393559c.
// The input should not include PUSHDATA (or OP_<num>) ops; those will
// be inferred.
func Compile(s string) ([]byte, error) {
	var res []byte
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Split(split)
	for scanner.Scan() {
		token := scanner.Text()
		if info, ok := opsByName[token]; ok {
			if strings.HasPrefix(token, "PUSHDATA") || strings.HasPrefix(token, "JUMP") {
				return nil, errors.Wrap(ErrToken, token)
			}
			res = append(res, byte(info.op))
		} else if strings.HasPrefix(token, "0x") {
			bytes, err := hex.DecodeString(strings.TrimPrefix(token, "0x"))
			if err != nil {
				return nil, err
			}
			res = append(res, PushdataBytes(bytes)...)
		} else if strings.HasPrefix(token, "JUMP:") {
			// TODO (Dan): refactor these into function, add labels, add IF/ELSE/ENDIF and BEGIN/WHILE/REPEAT
			address, err := strconv.ParseUint(strings.TrimPrefix(token, "JUMP:"), 10, 32)
			if err != nil {
				return nil, err
			}
			res = append(res, byte(OP_JUMP))
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, uint32(address))
			res = append(res, b...)
		} else if strings.HasPrefix(token, "JUMPIF:") {
			address, err := strconv.ParseUint(strings.TrimPrefix(token, "JUMPIF:"), 10, 32)
			if err != nil {
				return nil, err
			}
			res = append(res, byte(OP_JUMPIF))
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, uint32(address))
			res = append(res, b...)
		} else if len(token) >= 2 && token[0] == '\'' && token[len(token)-1] == '\'' {
			bytes := make([]byte, 0, len(token)-2)
			var b int
			for i := 1; i < len(token)-1; i++ {
				if token[i] == '\\' {
					i++
				}
				bytes = append(bytes, token[i])
				b++
			}
			res = append(res, PushdataBytes(bytes)...)
		} else if num, err := strconv.ParseInt(token, 10, 64); err == nil {
			res = append(res, PushdataInt64(num)...)
		} else {
			return nil, errors.Wrap(ErrToken, token)
		}
	}
	err := scanner.Err()
	if err != nil {
		return nil, err
	}
	return res, nil
}

func Decompile(prog []byte) (string, error) {
	var strs []string
	for i := uint32(0); i < uint32(len(prog)); { // update i inside the loop
		inst, err := ParseOp(prog, i)
		if err != nil {
			return "", err
		}
		var str string
		if len(inst.Data) > 0 {
			str = fmt.Sprintf("0x%x", inst.Data)
		} else {
			str = inst.Op.String()
		}
		strs = append(strs, str)
		i += inst.Len
	}
	return strings.Join(strs, " "), nil
}

// split is a bufio.SplitFunc for scanning the input to Compile.
// It starts like bufio.ScanWords but adjusts the return value to
// account for quoted strings.
func split(inp []byte, atEOF bool) (advance int, token []byte, err error) {
	advance, token, err = bufio.ScanWords(inp, atEOF)
	if err != nil {
		return
	}
	if len(token) > 1 && token[0] != '\'' {
		return
	}
	var start int
	for ; start < len(inp); start++ {
		if !unicode.IsSpace(rune(inp[start])) {
			break
		}
	}
	if start == len(inp) || inp[start] != '\'' {
		return
	}
	var escape bool
	for i := start + 1; i < len(inp); i++ {
		if escape {
			escape = false
		} else {
			switch inp[i] {
			case '\'':
				advance = i + 1
				token = inp[start:advance]
				return
			case '\\':
				escape = true
			}
		}
	}
	// Reached the end of the input with no closing quote.
	if atEOF {
		return 0, nil, ErrToken
	}
	return 0, nil, nil
}
