package ivy

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"

	chainjson "chain/encoding/json"
	"chain/errors"
)

type (
	CompileResult struct {
		Name    string
		Program chainjson.HexBytes
		Value   string
		Params  []ContractParam
		Clauses []ClauseInfo
	}

	ContractParam struct {
		Name string `json:"name"`
		Typ  string `json:"type"`
	}

	ClauseInfo struct {
		Name   string      `json:"name"`
		Args   []ClauseArg `json:"args"`
		Values []ValueInfo `json:"value_info"`

		// Mintimes is the stringified form of "x" for any "verify after(x)" in the clause
		Mintimes []string `json:"mintimes"`

		// Maxtimes is the stringified form of "x" for any "verify before(x)" in the clause
		Maxtimes []string `json:"maxtimes"`

		// Records each call to a hash function and the type of the
		// argument passed in
		HashCalls []HashCall `json:"hash_calls"`
	}

	ClauseArg struct {
		Name string `json:"name"`
		Typ  string `json:"type"`
	}

	ValueInfo struct {
		Name    string `json:"name"`
		Program string `json:"program,omitempty"`
		Asset   string `json:"asset,omitempty"`
		Amount  string `json:"amount,omitempty"`
	}
)

type ContractArg struct {
	B *bool               `json:"boolean,omitempty"`
	I *int64              `json:"integer,omitempty"`
	S *chainjson.HexBytes `json:"string,omitempty"`
}

// Compile parses an Ivy contract from the supplied reader and
// produces the compiled bytecode and other analysis.
func Compile(r io.Reader, args []ContractArg) (CompileResult, error) {
	inp, err := ioutil.ReadAll(r)
	if err != nil {
		return CompileResult{}, errors.Wrap(err, "reading input")
	}
	c, err := parse(inp)
	if err != nil {
		return CompileResult{}, errors.Wrap(err, "parse error")
	}

	globalEnv := newEnviron(nil)
	for _, k := range keywords {
		globalEnv.add(k, nilType, roleKeyword)
	}
	for _, b := range builtins {
		globalEnv.add(b.name, nilType, roleBuiltin)
	}
	err = globalEnv.add(c.Name, contractType, roleContract)
	if err != nil {
		return CompileResult{}, err
	}

	prog, labels, err := compileContract(c, args, globalEnv)
	if err != nil {
		return CompileResult{}, errors.Wrap(err, "compiling contract")
	}
	result := CompileResult{
		Name:    c.Name,
		Program: prog,
		Params:  []ContractParam{},
		Value:   c.Value,
		Labels:  labels,
	}
	for _, param := range c.Params {
		result.Params = append(result.Params, ContractParam{Name: param.Name, Typ: string(param.bestType())})
	}

	for _, clause := range c.Clauses {
		info := ClauseInfo{
			Name:      clause.Name,
			Args:      []ClauseArg{},
			Mintimes:  clause.MinTimes,
			Maxtimes:  clause.MaxTimes,
			HashCalls: clause.HashCalls,
		}
		if info.Mintimes == nil {
			info.Mintimes = []string{}
		}
		if info.Maxtimes == nil {
			info.Maxtimes = []string{}
		}

		for _, p := range clause.Params {
			info.Args = append(info.Args, ClauseArg{Name: p.Name, Typ: string(p.bestType())})
		}
		for _, stmt := range clause.statements {
			switch s := stmt.(type) {
			case *lockStatement:
				valueInfo := ValueInfo{
					Name:    s.locked.String(),
					Program: s.program.String(),
				}
				if s.locked.String() != c.Value {
					for _, r := range clause.Reqs {
						if s.locked.String() == r.name {
							valueInfo.Asset = r.assetExpr.String()
							valueInfo.Amount = r.amountExpr.String()
							break
						}
					}
				}
				info.Values = append(info.Values, valueInfo)
			case *unlockStatement:
				valueInfo := ValueInfo{Name: c.Value}
				info.Values = append(info.Values, valueInfo)
			}
		}
		result.Clauses = append(result.Clauses, info)
	}
	return result, nil
}

func compileContract(contract *Contract, args []ContractArg, globalEnv *environ) ([]byte, error) {
	var err error

	if len(contract.Clauses) == 0 {
		return nil, nil, fmt.Errorf("empty contract")
	}
	env := newEnviron(globalEnv)
	for _, p := range contract.Params {
		err = env.add(p.Name, p.Type, roleContractParam)
		if err != nil {
			return nil, nil, err
		}
	}
	err = env.add(contract.Value, valueType, roleContractValue)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range contract.Clauses {
		err = env.add(c.Name, nilType, roleClause)
		if err != nil {
			return nil, nil, err
		}
	}

	err = prohibitValueParams(contract)
	if err != nil {
		return nil, nil, err
	}
	err = prohibitSigParams(contract)
	if err != nil {
		return nil, nil, err
	}
	err = requireAllParamsUsedInClauses(contract.Params, contract.Clauses)
	if err != nil {
		return nil, nil, err
	}

	var stk stack

	if len(contract.Clauses) > 1 {
		stk = stk.add("<clause selector>")
	}

	for i := len(contract.Params) - 1; i >= 0; i-- {
		p := contract.Params[i]
		stk = stk.add(p.Name)
	}

	b := newBuilder()
	for i := len(args) - 1; i >= 0; i-- {
		a := args[i]
		switch {
		case a.B != nil:
			var n int64
			if *a.B {
				n = 1
			}
			b.addInt64(n)
		case a.I != nil:
			b.addInt64(*a.I)
		case a.S != nil:
			b.addData(*a.S)
		}
	}

	var prog []byte

	if len(contract.Clauses) == 1 {
		err = compileClause(b, stk, contract, env, contract.Clauses[0])
		if err != nil {
			return nil, nil, err
		}
		prog, err = b.build()
	} else {
		if len(contract.Params) > 0 {
			// A clause selector is at the bottom of the stack. Roll it to the
			// top.
			stk = b.addRoll(stk, len(contract.Params)) // stack: [<clause params> <contract params> <clause selector>]
		}

		// clauses 2..N-1
		for i := len(contract.Clauses) - 1; i >= 2; i-- {
			stk = b.addDup(stk)                                                   // stack: [... <clause selector> <clause selector>]
			stk = b.addInt64(stk, int64(i))                                       // stack: [... <clause selector> <clause selector> <i>]
			stk = b.addNumEqual(stk, fmt.Sprintf("(<clause selector> == %d)", i)) // stack: [... <clause selector> <i == clause selector>]
			stk = b.addJumpIf(stk, contract.Clauses[i].Name)                      // stack: [... <clause selector>]
		}

		// clause 1
		stk = b.addJumpIf(stk, contract.Clauses[1].Name) // consumes the clause selector

		// no jump needed for clause 0

		for i, clause := range contract.Clauses {
			b.addJumpTarget(stk, clause.Name)

			if i > 1 {
				// Clauses 0 and 1 have no clause selector on top of the
				// stack. Clauses 2 and later do.
				stk = b.addDrop(stk)
			}

			err = compileClause(b, stk, contract, env, clause)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "compiling clause \"%s\"", clause.Name)
			}
			b.forgetPendingVerify()
			if i < len(contract.Clauses)-1 {
				b.addJump(stk, "_end")
			}
		}
		b.addJumpTarget(stk, "_end")
		prog, err = b.build()
		if err != nil {
			return nil, nil, err
		}
	}

	return prog, labels, nil
}

func compileClause(b *builder, contractStk stack, contract *Contract, env *environ, clause *Clause) error {
	var err error

	// copy env to leave outerEnv unchanged
	env = newEnviron(env)
	for _, p := range clause.Params {
		err = env.add(p.Name, p.Type, roleClauseParam)
		if err != nil {
			return err
		}
	}
	for _, req := range clause.Reqs {
		err = env.add(req.name, valueType, roleClauseValue)
		if err != nil {
			return err
		}
	}

	assignIndexes(clause)

	var stk stack
	for _, p := range clause.params {
		// NOTE: the order of clause params is not reversed, unlike
		// contract params (and also unlike the arguments to Ivy
		// function-calls).
		stk = stk.add(p.Name)
	}
	stk = stk.addFromStack(contractStk)

	// a count of the number of times each variable is referenced
	counts := make(map[string]int)
	for _, req := range clause.Reqs {
		req.assetExpr.countVarRefs(counts)
		req.amountExpr.countVarRefs(counts)
	}
	for _, s := range clause.statements {
		s.countVarRefs(counts)
	}

	for _, s := range clause.statements {
		switch stmt := s.(type) {
		case *verifyStatement:
			stk, err = compileExpr(b, stk, contract, clause, env, counts, stmt.expr)
			if err != nil {
				return errors.Wrapf(err, "in verify statement in clause \"%s\"", clause.Name)
			}
			stk = b.addVerify(stk)

			// special-case reporting of certain function calls
			if c, ok := stmt.expr.(*callExpr); ok && len(c.args) == 1 {
				if b := referencedBuiltin(c.fn); b != nil {
					switch b.name {
					case "before":
						clause.MaxTimes = append(clause.MaxTimes, c.args[0].String())
					case "after":
						clause.MinTimes = append(clause.MinTimes, c.args[0].String())
					}
				}
			}

		case *lockStatement:
			// index
			stk = b.addInt64(stk, stmt.index)

			// refdatahash
			stk = b.addData(stk, nil)

			// TODO: permit more complex expressions for locked,
			// like "lock x+y with foo" (?)

			if stmt.locked.String() == contract.Value {
				stk = b.addAmount(stk)
				stk = b.addAsset(stk)
			} else {
				var req *ClauseRequirement
				for _, r := range clause.Reqs {
					if stmt.locked.String() == r.name {
						req = r
						break
					}
				}
				if req == nil {
					return fmt.Errorf("unknown value \"%s\" in lock statement in clause \"%s\"", stmt.locked, clause.Name)
				}

				// amount
				stk, err = compileExpr(b, stk, contract, clause, env, counts, req.amountExpr)
				if err != nil {
					return errors.Wrapf(err, "in lock statement in clause \"%s\"", clause.Name)
				}

				// asset
				stk, err = compileExpr(b, stk, contract, clause, env, counts, req.assetExpr)
				if err != nil {
					return errors.Wrapf(err, "in lock statement in clause \"%s\"", clause.Name)
				}
			}

			// version
			stk = b.addInt64(stk, 1)

			// prog
			stk, err = compileExpr(b, stk, contract, clause, env, counts, stmt.program)
			if err != nil {
				return errors.Wrapf(err, "in lock statement in clause \"%s\"", clause.Name)
			}

			stk = b.addCheckOutput(stk, fmt.Sprintf("checkOutput(%s, %s)", stmt.locked, stmt.program))
			stk = b.addVerify(stk)

		case *unlockStatement:
			if len(clause.statements) == 1 {
				// This is the only statement in the clause, make sure TRUE is
				// on the stack.
				stk = b.addBoolean(stk, true)
			}
		}
	}

	err = requireAllValuesDisposedOnce(contract, clause)
	if err != nil {
		return err
	}
	err = typeCheckClause(contract, clause, env)
	if err != nil {
		return err
	}
	err = requireAllParamsUsedInClause(clause.Params, clause)
	if err != nil {
		return err
	}

	return nil
}

func compileExpr(b *builder, stk stack, contract *Contract, clause *Clause, env *environ, counts map[string]int, expr expression) (stack, error) {
	var err error

	switch e := expr.(type) {
	case *binaryExpr:
		// Do typechecking after compiling subexpressions (because other
		// compilation errors are more interesting than type mismatch
		// errors).

		stk, err = compileExpr(b, stk, contract, clause, env, counts, e.left)
		if err != nil {
			return nil, errors.Wrapf(err, "in left operand of \"%s\" expression", e.op.op)
		}
		stk, err = compileExpr(b, stk, contract, clause, env, counts, e.right)
		if err != nil {
			return nil, errors.Wrapf(err, "in right operand of \"%s\" expression", e.op.op)
		}

		lType := e.left.typ(env)
		if e.op.left != "" && lType != e.op.left {
			return nil, fmt.Errorf("in \"%s\", left operand has type \"%s\", must be \"%s\"", e, lType, e.op.left)
		}

		rType := e.right.typ(env)
		if e.op.right != "" && rType != e.op.right {
			return nil, fmt.Errorf("in \"%s\", right operand has type \"%s\", must be \"%s\"", e, rType, e.op.right)
		}

		switch e.op.op {
		case "==", "!=":
			if lType != rType {
				// Maybe one is Hash and the other is (more-specific-Hash subtype).
				// TODO(bobg): generalize this mechanism
				if lType == hashType && isHashSubtype(rType) {
					propagateType(contract, clause, env, rType, e.left)
				} else if rType == hashType && isHashSubtype(lType) {
					propagateType(contract, clause, env, lType, e.right)
				} else {
					return nil, fmt.Errorf("type mismatch in \"%s\": left operand has type \"%s\", right operand has type \"%s\"", e, lType, rType)
				}
			}
			if lType == "Boolean" {
				return nil, fmt.Errorf("in \"%s\": using \"%s\" on Boolean values not allowed", e, e.op.op)
			}
		}

		stk = b.addOps(stk.dropN(2), e.op.opcodes, e.String())

	case *unaryExpr:
		// Do typechecking after compiling subexpression (because other
		// compilation errors are more interesting than type mismatch
		// errors).

		var err error
		stk, err = compileExpr(b, stk, contract, clause, env, counts, e.expr)
		if err != nil {
			return nil, errors.Wrapf(err, "in \"%s\" expression", e.op.op)
		}

		if e.op.operand != "" && e.expr.typ(env) != e.op.operand {
			return nil, fmt.Errorf("in \"%s\", operand has type \"%s\", must be \"%s\"", e, e.expr.typ(env), e.op.operand)
		}
		b.addOps(stk.drop(), e.op.opcodes, e.String())

	case *callExpr:
		bi := referencedBuiltin(e.fn)
		if bi == nil {
			if e.fn.typ(env) == contractType {
				if e.fn.String() != contract.Name {
					return nil, fmt.Errorf("calling other contracts not yet supported")
				}
				// xxx TODO contract composition
				return nil, nil
			}
			return nil, fmt.Errorf("unknown function \"%s\"", e.fn)
		}

		if len(e.args) != len(bi.args) {
			return nil, fmt.Errorf("wrong number of args for \"%s\": have %d, want %d", bi.name, len(e.args), len(bi.args))
		}

		// WARNING WARNING WOOP WOOP
		// special-case hack
		// WARNING WARNING WOOP WOOP
		if bi.name == "checkTxMultiSig" {
			if _, ok := e.args[0].(listExpr); !ok {
				return nil, fmt.Errorf("checkTxMultiSig expects list literals, got %T for argument 0", e.args[0])
			}
			if _, ok := e.args[1].(listExpr); !ok {
				return nil, fmt.Errorf("checkTxMultiSig expects list literals, got %T for argument 1", e.args[1])
			}

			var k1, k2 int

			stk, k1, err = compileArg(b, stk, contract, clause, env, counts, e.args[1])
			if err != nil {
				return nil, err
			}

			// stack: [... sigM ... sig1 M]

			var altEntry string
			stk, altEntry = b.addToAltStack(stk) // stack: [... sigM ... sig1]
			stk = b.addTxSigHash(stk)            // stack: [... sigM ... sig1 txsighash]

			stk, k2, err = compileArg(b, stk, contract, clause, env, counts, e.args[0])
			if err != nil {
				return nil, err
			}

			// stack: [... sigM ... sig1 txsighash pubkeyN ... pubkey1 N]

			stk = b.addFromAltStack(stk, altEntry) // stack: [... sigM ... sig1 txsighash pubkeyN ... pubkey1 N M]
			stk = b.addSwap(stk)                   // stack: [... sigM ... sig1 txsighash pubkeyN ... pubkey1 M N]
			stk = b.addCheckMultisig(stk, k1+k2, e.String())

			return stk, nil
		}

		var k int

		for i := len(e.args) - 1; i >= 0; i-- {
			a := e.args[i]
			var k2 int
			var err error
			stk, k2, err = compileArg(b, stk, contract, clause, env, counts, a)
			if err != nil {
				return nil, errors.Wrapf(err, "compiling argument %d in call expression", i)
			}
			k += k2
		}

		// Do typechecking after compiling subexpressions (because other
		// compilation errors are more interesting than type mismatch
		// errors).
		for i, actual := range e.args {
			if bi.args[i] != "" && actual.typ(env) != bi.args[i] {
				return nil, fmt.Errorf("argument %d to \"%s\" has type \"%s\", must be \"%s\"", i, bi.name, actual.typ(env), bi.args[i])
			}
		}

		b.addOps(stk.dropN(k), bi.opcodes, e.String())

		// special-case reporting
		switch bi.name {
		case "sha3", "sha256":
			clause.HashCalls = append(clause.HashCalls, HashCall{bi.name, e.args[0].String(), string(e.args[0].typ(env))})
		}

	case varRef:
		return compileRef(b, stk, counts, e)

	case integerLiteral:
		stk = b.addInt64(stk, int64(e))

	case bytesLiteral:
		stk = b.addData(stk, []byte(e))

	case booleanLiteral:
		stk = b.addBoolean(stk, e)

	case listExpr:
		// Lists are excluded here because they disobey the invariant of
		// this function: namely, that it increases the stack size by
		// exactly one. (A list pushes its items and its length on the
		// stack.) But they're OK as function-call arguments because the
		// function (presumably) consumes all the stack items added.
		return nil, fmt.Errorf("encountered list outside of function-call context")
	}
	return stk, nil
}

func compileArg(b *builder, stk stack, contract *Contract, clause *Clause, env *environ, counts map[string]int, expr expression) (stack, int, error) {
	var n int
	if list, ok := expr.(listExpr); ok {
		for i := 0; i < len(list); i++ {
			elt := list[len(list)-i-1]
			var err error
			stk, err = compileExpr(b, stk, contract, clause, env, counts, elt)
			if err != nil {
				return nil, 0, err
			}
			n++
		}
		stk = b.addInt64(stk, int64(len(list)))
		n++
		return stk, n, nil
	}
	var err error
	stk, err = compileExpr(b, stk, contract, clause, env, counts, expr)
	return stk, 1, err
}

func compileRef(b *builder, stk stack, counts map[string]int, ref varRef) (stack, error) {
	depth := stk.find(ref)
	if depth < 0 {
		return nil, fmt.Errorf("undefined reference: \"%s\"", ref)
	}

	var isFinal bool
	if count, ok := counts[string(ref)]; ok && count > 0 {
		count--
		counts[string(ref)] = count
		isFinal = count == 0
	}

	switch depth {
	case 0:
		if !isFinal {
			stk = b.addDup(stk)
		}
	case 1:
		if isFinal {
			stk = b.addSwap(stk)
		} else {
			stk = b.addOver(stk)
		}
	default:
		if isFinal {
			stk = b.addRoll(depth)
		} else {
			stk = b.addPick(depth)
		}
	}
	return stack, nil
}

func (a *ContractArg) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}
	if r, ok := m["boolean"]; ok {
		var bval bool
		err = json.Unmarshal(r, &bval)
		if err != nil {
			return err
		}
		a.B = &bval
		return nil
	}
	if r, ok := m["integer"]; ok {
		var ival int64
		err = json.Unmarshal(r, &ival)
		if err != nil {
			return err
		}
		a.I = &ival
		return nil
	}
	r, ok := m["string"]
	if !ok {
		return fmt.Errorf("contract arg must define one of boolean, integer, string")
	}
	var sval chainjson.HexBytes
	err = json.Unmarshal(r, &sval)
	if err != nil {
		return err
	}
	a.S = &sval
	return nil
}
