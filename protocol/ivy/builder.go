package ivy

import (
	"fmt"
	"strconv"
)

type builder struct {
	items         []builderItem
	pendingVerify *builderItem
}

type builderItem struct {
	opcodes string
	stk     stack
}

func (b *builder) add(opcodes string, newstack stack) stack {
	item := &builderItem{opcodes: opcodes, stk: newstack}
	b.items = append(b.items, item)
	return newstack
}

func (b *builder) addRoll(stk stack, n int) stack {
	return b.add("ROLL", stk.roll(n))
}

func (b *builder) addDup(stk stack) stack {
	return b.add("DUP", stk.dup())
}

func (b *builder) addInt64(stk stack, n int64) stack {
	s := strconv.FormatInt(n, 10)
	return b.add(s, stk.add(s))
}

func (b *builder) addNumEqual(stk stack, desc string) stack {
	return b.add("NUMEQUAL", stk.dropN(2).add(desc))
}

func (b *builder) addJumpIf(stk stack, label string) stack {
	return b.add(fmt.Sprintf("JUMPIF:$%s", label), stk.drop())
}

func (b *builder) addJumpTarget(stk stack, label string) stack {
	return b.add("$"+label, stk)
}

func (b *builder) addDrop(stk stack) stack {
	return b.add("DROP", stk.drop())
}

func (b *builder) forgetPendingVerify() {
	b.pendingVerify = nil
}

func (b *builder) addJump(stk stack, label string) stack {
	return b.add(fmt.Sprintf("JUMP:$%s", label), stk)
}

func (b *builder) addVerify(stk stack) stack {
	return b.add("VERIFY", stk.drop())
}

func (b *builder) addData(stk stack, data []byte) stack {
	// xxx simplest string representation of data
	return b.add(s, stk.add(s))
}

func (b *builder) addAmount(stk stack) stack {
	return b.add("AMOUNT", stk.add("<amount>"))
}

func (b *builder) addAsset(stk stack) stack {
	return b.add("ASSET", stk.add("<asset>"))
}

func (b *builder) addCheckOutput(stk stack, desc string) {
	return b.add("CHECKOUTPUT", stk.dropN(6).add(desc))
}

func (b *builder) addBoolean(stk stack, val bool) stack {
	if val {
		return b.add("TRUE", stk.add("true"))
	}
	return b.add("FALSE", stk.add("false"))
}

func (b *builder) addOps(stk stack, ops string, desc string) {
	return b.add(ops, stk.add(desc))
}

func (b *builder) addToAltStack(stk stack) (stack, string) {
	t := stk.top()
	return b.add("TOALTSTACK", stk.drop()), t
}

func (b *builder) addTxSigHash(stk stack) stack {
	return b.add("TXSIGHASH", stk.add("<txsighash>"))
}

func (b *builder) addFromAltStack(stk stack, alt string) stack {
	return b.add("FROMALTSTACK", stk.add(alt))
}

func (b *builder) addSwap(stk stack) stack {
	return b.add("SWAP", stk.swap())
}

func (b *builder) checkMultisig(stk stack, n int, desc string) stack {
	return b.add("CHECKMULTISIG", stk.dropN(n).add(desc))
}

func (b *builder) addOver(stk stack) stack {
	return b.add("OVER", stk.over())
}

func (b *builder) addPick(stk stack, n int) stack {
	return b.add("PICK", stk.pick(n))
}
