package ivy

import (
	"fmt"
	"io"

	"chain/protocol/vm"
)

// Compile parses an Ivy contract from the supplied input source and
// produces the compiled bytecode.
func Compile(r io.Reader) ([]byte, error) {
	c, err := ParseReader("input", r, Debug(false))
	if err != nil {
		return nil, err
	}
	return compile(c.(*contract))
}

func compile(contract *contract) ([]byte, error) {
	if len(contract.clauses) == 0 {
		return nil, fmt.Errorf("empty contract")
	}

	err := prohibitDuplicateClauseNames(contract)
	if err != nil {
		return nil, err
	}
	err = prohibitDuplicateVars(contract)
	if err != nil {
		return nil, err
	}
	err = requireValueParam(contract)
	if err != nil {
		return nil, err
	}

	stack := addParamsToStack(nil, contract.params)

	if len(contract.clauses) == 1 {
		b := newBuilder()
		err = compileClause(b, stack, contract, contract.clauses[0])
		if err != nil {
			return nil, err
		}
		return b.build()
	}

	b := newBuilder()
	endTarget := b.newJumpTarget()
	clauseTargets := make([]int, len(contract.clauses))
	for i := range contract.clauses {
		clauseTargets[i] = b.newJumpTarget()
	}

	if len(stack) > 0 {
		// A clause selector is at the bottom of the stack. Roll it to the
		// top.
		b.addInt64(int64(len(stack)))
		b.addOp(vm.OP_ROLL) // stack: [<clause params> <contract params> <clause selector>]
	}

	// clauses 2..N-1
	for i := len(contract.clauses) - 1; i >= 2; i-- {
		b.addOp(vm.OP_DUP)            // stack: [... <clause selector> <clause selector>]
		b.addInt64(int64(i))          // stack: [... <clause selector> <clause selector> <i>]
		b.addOp(vm.OP_NUMEQUAL)       // stack: [... <clause selector> <i == clause selector>]
		b.addJumpIf(clauseTargets[i]) // stack: [... <clause selector>]
	}

	// clause 1
	b.addJumpIf(clauseTargets[1])

	// no jump needed for clause 0

	for i, clause := range contract.clauses {
		b.setJumpTarget(clauseTargets[i])
		b2 := newBuilder()
		err = compileClause(b2, stack, contract, clause)
		if err != nil {
			return nil, err
		}
		prog, err := b2.build()
		if err != nil {
			return nil, err
		}
		b.addRawBytes(prog)
		if i < len(contract.clauses)-1 {
			b.addJump(endTarget)
		}
	}
	b.setJumpTarget(endTarget)
	return b.build()
}

func compileClause(b *builder, contractStack []stackEntry, contract *contract, clause *clause) error {
	err := decorateRefs(contract, clause)
	if err != nil {
		return err
	}
	err = decorateOutputs(contract, clause)
	if err != nil {
		return err
	}
	err = requireAllValuesDisposedOnce(contract, clause)
	if err != nil {
		return err
	}
	assignIndexes(clause)
	stack := addParamsToStack(contractStack, clause.params)
	for _, s := range clause.statements {
		switch stmt := s.(type) {
		case *verifyStatement:
			if stmt.associatedOutput != nil {
				// This verify is associated with an output. Instead of
				// compiling it, contribute its terms to the output
				// statement's CHECKOUTPUT.
				continue
			}
			if typeOf(stmt.expr) != "Boolean" {
				return fmt.Errorf("expression in verify statement is \"%s\", must be Boolean", typeOf(stmt.expr))
			}
			err = compileExpr(b, stack, contract, clause, stmt.expr)
			if err != nil {
				return err
			}
			b.addOp(vm.OP_VERIFY)

		case *outputStatement:
			// index
			b.addInt64(stmt.index)
			stack = append(stack, stackEntry{})

			// refdatahash
			b.addData(nil)
			stack = append(stack, stackEntry{})

			p := stmt.param
			if p == nil {
				// amount
				b.addOp(vm.OP_AMOUNT)
				stack = append(stack, stackEntry{})

				// asset
				b.addOp(vm.OP_ASSET)
				stack = append(stack, stackEntry{})
			} else {
				// amount
				// TODO(bobg): this is a bit of a hack; need a cleaner way to
				// introduce new stack references
				r := &propRef{
					expr: &varRef{
						name: stmt.param.name,
					},
					property: "amount",
				}
				err := decorateRefsInExpr(contract, clause, r)
				if err != nil {
					return err
				}
				err = compileExpr(b, stack, contract, clause, r)
				if err != nil {
					return err
				}
				stack = append(stack, stackEntry{})

				// asset
				r = &propRef{
					expr: &varRef{
						name: stmt.param.name,
					},
					property: "asset",
				}
				err = decorateRefsInExpr(contract, clause, r)
				if err != nil {
					return err
				}
				err = compileExpr(b, stack, contract, clause, r)
				if err != nil {
					return err
				}
				stack = append(stack, stackEntry{})
			}

			// version
			b.addInt64(1)
			stack = append(stack, stackEntry{})

			// prog
			err = compileExpr(b, stack, contract, clause, stmt.call.fn)
			if err != nil {
				return err
			}

			b.addOp(vm.OP_CHECKOUTPUT)
			b.addOp(vm.OP_VERIFY)

		case *returnStatement:
			if referencedParam(stmt.expr) != contract.params[len(contract.params)-1] {
				fmt.Errorf("expression in return statement must be the contract value parameter")
			}
			// xxx add an OP_TRUE if there are no other statements in the clause?
		}
	}
	return nil
}

func compileExpr(b *builder, stack []stackEntry, contract *contract, clause *clause, expr expression) error {
	switch e := expr.(type) {
	case *binaryExpr:
		err := compileExpr(b, stack, contract, clause, e.left)
		if err != nil {
			return err
		}
		err = compileExpr(b, append(stack, stackEntry{}), contract, clause, e.right)
		if err != nil {
			return err
		}
		switch e.op {
		case "==":
			b.addOp(vm.OP_EQUAL)
		case "!=":
			b.addOp(vm.OP_EQUAL)
			b.addOp(vm.OP_NOT)
		case "<=":
			b.addOp(vm.OP_LESSTHANOREQUAL)
		case ">=":
			b.addOp(vm.OP_GREATERTHANOREQUAL)
		case "<":
			b.addOp(vm.OP_LESSTHAN)
		case ">":
			b.addOp(vm.OP_GREATERTHAN)
		case "+":
			b.addOp(vm.OP_ADD)
		case "-":
			b.addOp(vm.OP_SUB)
		default:
			return fmt.Errorf("unknown operator \"%s\"", e.op)
		}

	case *unaryExpr:
		err := compileExpr(b, stack, contract, clause, e.expr)
		if err != nil {
			return err
		}
		switch e.op {
		case "-":
			b.addOp(vm.OP_NEGATE)
		case "!":
			b.addOp(vm.OP_NOT)
		default:
			return fmt.Errorf("unknown operator \"%s\"", e.op)
		}

	case *call:
		bi := referencedBuiltin(e.fn)
		if bi == nil {
			return fmt.Errorf("unknown function \"%s\"", e.fn)
		}
		for _, a := range e.args {
			err := compileExpr(b, stack, contract, clause, a)
			if err != nil {
				return err
			}
			stack = append(stack, stackEntry{})
		}
		b.addRawBytes(bi.ops)

	case *varRef:
		return compileRef(b, stack, e)

	case *propRef:
		return compileRef(b, stack, e)

	case integerLiteral:
		b.addInt64(int64(e))

	case booleanLiteral:
		if e {
			b.addOp(vm.OP_TRUE)
		} else {
			b.addOp(vm.OP_FALSE)
		}
	}
	return nil
}

func compileRef(b *builder, stack []stackEntry, ref expression) error {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].matches(ref) {
			depth := int64(len(stack) - 1 - i)
			switch depth {
			case 0:
				b.addOp(vm.OP_DUP)
			case 1:
				b.addOp(vm.OP_OVER)
			default:
				b.addInt64(depth)
				b.addOp(vm.OP_PICK)
			}
			return nil
		}
	}
	return fmt.Errorf("undefined reference \"%s\"", ref)
}
