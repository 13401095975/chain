package txvm

import "encoding/binary"

type vm struct {
	// config, doesn't change after init
	traceOp    func(byte, []byte, stack, stack, [NumStacks]tupleStack)
	traceError func(error)

	pc   int    // program counter
	prog []byte // current program

	data stack
	alt  stack

	tupleStacks [NumStacks]tupleStack
}

// Validate returns whether x is valid.
//
// To get detailed information about a Tx,
// such as determining why an invalid Tx is invalid,
// use Option funcs to trace execution.
func Validate(tx []byte, o ...Option) bool {
	vm := &vm{}
	for _, o := range o {
		o(vm)
	}

	defer func() {
		err := recover()
		if err, ok := err.(error); ok {
			vm.traceError(err)
		}
	}()

	exec(vm, tx)

	// TODO(kr): call some tracing hook here
	// to signal end of execution.

	return vm.tupleStacks[StackSummary].Len() == 1 &&
		vm.tupleStacks[StackInput].Len() == 0 &&
		vm.tupleStacks[StackValue].Len() == 0 &&
		vm.tupleStacks[StackOutput].Len() == 0 &&
		vm.tupleStacks[StackCond].Len() == 0 &&
		vm.tupleStacks[StackNonce].Len() == 0 &&
		vm.tupleStacks[StackRetirement].Len() == 0 &&
		vm.tupleStacks[StackTimeConstraint].Len() == 0 &&
		vm.tupleStacks[StackAnnotation].Len() == 0
}

func exec(vm *vm, prog []byte) {
	ret, rp := vm.pc, vm.prog
	vm.pc = 0
	vm.prog = prog // for errors
	for vm.pc < len(prog) {
		step(vm)
	}
	vm.pc, vm.prog = ret, rp
}

func step(vm *vm) {
	opcode, data, n := decodeInst(vm.prog[vm.pc:])
	vm.traceOp(opcode, data, vm.data, vm.alt, vm.tupleStacks)
	vm.pc += n
	if opcode == BaseData {
		vm.data.PushBytes(data)
	} else if opcode >= BaseInt {
		vm.data.PushInt64(int64(opcode) - BaseInt)
	} else {
		optab[opcode](vm)
	}
}

func decodeInst(buf []byte) (opcode byte, imm []byte, n int) {
	v, n := binary.Uvarint(buf) // note v=0 on error
	if v < BaseData {
		return byte(v), nil, n
	}
	r := v - BaseData + uint64(n)
	return BaseData, append([]byte{}, buf[n:r]...), int(r)
}

func idsEqual(a, b []byte) bool {
	if len(a) != len(b) || len(a) != 32 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func getStack(vm *vm, t int64) *tupleStack {
	return &vm.tupleStacks[t]
}
