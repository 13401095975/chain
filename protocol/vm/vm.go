package vm

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/sync/errgroup"

	// TODO(bobg): very little of this package depends on bc, consider trying to remove the dependency
	"chain-stealth/crypto/ca"
	"chain-stealth/errors"
	"chain-stealth/protocol/bc"
)

const initialRunLimit = 10000

// ErrFalseVMResult is one of the ways for a transaction to fail validation
var ErrFalseVMResult = errors.New("false VM result")

type virtualMachine struct {
	ctx context.Context

	program      []byte // the program currently executing
	mainprog     []byte // the outermost program, returned by OP_PROGRAM
	pc, nextPC   uint32
	runLimit     int64
	deferredCost int64

	issuanceKey *ca.Point

	expansionReserved bool

	// Stores the data parsed out of an opcode. Used as input to
	// data-pushing opcodes.
	data []byte

	// CHECKPREDICATE spawns a child vm with depth+1
	depth int

	// In each of these stacks, stack[len(stack)-1] is the top element.
	dataStack [][]byte
	altStack  [][]byte

	tx         *bc.Tx
	inputIndex int
	sigHasher  *bc.SigHasher

	block *bc.Block
}

// TraceOut - if non-nil - will receive trace output during
// execution.
var TraceOut io.Writer

func VerifyTxInput(ctx context.Context, tx *bc.Tx, inputIndex int) (err error) {
	defer func() {
		if panErr := recover(); panErr != nil {
			err = ErrUnexpected
		}
	}()
	return verifyTxInput(ctx, tx, inputIndex)
}

func verifyTxInput(ctx context.Context, tx *bc.Tx, inputIndex int) error {
	if inputIndex < 0 || inputIndex >= len(tx.Inputs) {
		return ErrBadValue
	}

	txinput := tx.Inputs[inputIndex]

	expansionReserved := tx.Version == 1 || tx.Version == 2

	sigHasher := bc.NewSigHasher(&tx.TxData)

	f := func(ctx context.Context, vmversion uint64, prog []byte, args [][]byte, issuanceKey *ca.Point) error {
		if vmversion != 1 {
			return ErrUnsupportedVM
		}

		vm := virtualMachine{
			ctx: ctx,

			tx:         tx,
			inputIndex: inputIndex,
			sigHasher:  sigHasher,

			issuanceKey: issuanceKey,

			expansionReserved: expansionReserved,

			mainprog: prog,
			program:  prog,
			runLimit: initialRunLimit,
		}
		for _, arg := range args {
			err := vm.push(arg, false)
			if err != nil {
				return err
			}
		}
		err := vm.run()
		return wrapErr(err, &vm, args)
	}

	switch inp := txinput.TypedInput.(type) {
	case *bc.IssuanceInput1:
		return f(ctx, inp.VMVersion, inp.IssuanceProgram, inp.Arguments, nil)
	case *bc.IssuanceInput2:
		g, newctx := errgroup.WithContext(ctx)
		iarp := inp.IssuanceAssetRangeProof()
		for i, c := range inp.AssetChoices {
			var issuanceKey *ca.Point
			if iarp != nil {
				issuanceKey = &iarp.Y[i]
			}
			g.Go(func() error {
				return f(newctx, c.VMVersion, c.IssuanceProgram, c.Arguments, issuanceKey)
			})
		}
		g.Go(func() error {
			return f(newctx, inp.VMVersion(), inp.Program(), inp.Arguments(), nil)
		})
		return g.Wait()
	case *bc.SpendInput:
		return f(ctx, inp.VMVer(), inp.Program(), inp.Arguments, nil)
	}
	return errors.WithDetailf(ErrUnsupportedTx, "transaction input %d has unknown type %T", inputIndex, txinput.TypedInput)
}

func VerifyBlockHeader(ctx context.Context, prev *bc.BlockHeader, block *bc.Block) (err error) {
	defer func() {
		if panErr := recover(); panErr != nil {
			err = ErrUnexpected
		}
	}()
	return verifyBlockHeader(ctx, prev, block)
}

func verifyBlockHeader(ctx context.Context, prev *bc.BlockHeader, block *bc.Block) error {
	vm := virtualMachine{
		ctx: ctx,

		block: block,

		expansionReserved: true,

		mainprog: prev.ConsensusProgram,
		program:  prev.ConsensusProgram,
		runLimit: initialRunLimit,
	}

	for _, arg := range block.Witness {
		err := vm.push(arg, false)
		if err != nil {
			return err
		}
	}

	err := vm.run()
	return wrapErr(err, &vm, block.Witness)
}

func (vm *virtualMachine) run() error {
	ctx := vm.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for vm.pc = 0; vm.pc < uint32(len(vm.program)); { // handle vm.pc updates in step
		err := ctx.Err()
		if err != nil {
			return err
		}
		err = vm.step()
		if err != nil {
			return err
		}
	}

	ok := len(vm.dataStack) > 0 && AsBool(vm.dataStack[len(vm.dataStack)-1])
	if !ok {
		return ErrFalseVMResult
	}
	return nil
}

func (vm *virtualMachine) step() error {
	inst, err := ParseOp(vm.program, vm.pc)
	if err != nil {
		return err
	}

	vm.nextPC = vm.pc + inst.Len

	if TraceOut != nil {
		opname := inst.Op.String()
		fmt.Fprintf(TraceOut, "vm %d pc %d limit %d %s", vm.depth, vm.pc, vm.runLimit, opname)
		if len(inst.Data) > 0 {
			fmt.Fprintf(TraceOut, " %x", inst.Data)
		}
		fmt.Fprint(TraceOut, "\n")
	}

	if isExpansion[inst.Op] {
		if vm.expansionReserved {
			return ErrDisallowedOpcode
		}
		vm.pc = vm.nextPC
		return vm.applyCost(1)
	}

	vm.deferredCost = 0
	vm.data = inst.Data
	err = ops[inst.Op].fn(vm)
	if err != nil {
		return err
	}
	err = vm.applyCost(vm.deferredCost)
	if err != nil {
		return err
	}
	vm.pc = vm.nextPC

	if TraceOut != nil {
		for i := len(vm.dataStack) - 1; i >= 0; i-- {
			fmt.Fprintf(TraceOut, "  stack %d: %x\n", len(vm.dataStack)-1-i, vm.dataStack[i])
		}
	}

	return nil
}

func (vm *virtualMachine) push(data []byte, deferred bool) error {
	cost := 8 + int64(len(data))
	if deferred {
		vm.deferCost(cost)
	} else {
		err := vm.applyCost(cost)
		if err != nil {
			return err
		}
	}
	vm.dataStack = append(vm.dataStack, data)
	return nil
}

func (vm *virtualMachine) pushBool(b bool, deferred bool) error {
	return vm.push(BoolBytes(b), deferred)
}

func (vm *virtualMachine) pushInt64(n int64, deferred bool) error {
	return vm.push(Int64Bytes(n), deferred)
}

func (vm *virtualMachine) pop(deferred bool) ([]byte, error) {
	if len(vm.dataStack) == 0 {
		return nil, ErrDataStackUnderflow
	}
	res := vm.dataStack[len(vm.dataStack)-1]
	vm.dataStack = vm.dataStack[:len(vm.dataStack)-1]

	cost := 8 + int64(len(res))
	if deferred {
		vm.deferCost(-cost)
	} else {
		vm.runLimit += cost
	}

	return res, nil
}

func (vm *virtualMachine) popInt64(deferred bool) (int64, error) {
	bytes, err := vm.pop(deferred)
	if err != nil {
		return 0, err
	}
	n, err := AsInt64(bytes)
	return n, err
}

func (vm *virtualMachine) top() ([]byte, error) {
	if len(vm.dataStack) == 0 {
		return nil, ErrDataStackUnderflow
	}
	return vm.dataStack[len(vm.dataStack)-1], nil
}

// positive cost decreases runlimit, negative cost increases it
func (vm *virtualMachine) applyCost(n int64) error {
	if n > vm.runLimit {
		return ErrRunLimitExceeded
	}
	vm.runLimit -= n
	return nil
}

func (vm *virtualMachine) deferCost(n int64) {
	vm.deferredCost += n
}

func stackCost(stack [][]byte) int64 {
	result := int64(8 * len(stack))
	for _, item := range stack {
		result += int64(len(item))
	}
	return result
}

type Error struct {
	Err  error
	Prog []byte
	Args [][]byte
}

func (vmerr Error) Error() string {
	dis, err := Disassemble(vmerr.Prog)
	if err != nil {
		dis = "???"
	}

	args := make([]string, 0, len(vmerr.Args))
	for _, a := range vmerr.Args {
		args = append(args, hex.EncodeToString(a))
	}

	return fmt.Sprintf("%s [prog %x = %s; args %s]", vmerr.Err.Error(), vmerr.Prog, dis, strings.Join(args, " "))
}

func wrapErr(err error, vm *virtualMachine, args [][]byte) error {
	if err == nil {
		return nil
	}
	return Error{
		Err:  err,
		Prog: vm.program,
		Args: args,
	}
}
