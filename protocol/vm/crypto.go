package vm

import (
	"crypto/sha1"
	"crypto/sha256"
	"hash"

	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/sha3"

	"chain/crypto/ed25519"
	"chain/crypto/ed25519/hd25519"
	"chain/protocol/bc"
)

func opRipemd160(vm *virtualMachine) error {
	return doHash(vm, ripemd160.New)
}

func opSha1(vm *virtualMachine) error {
	return doHash(vm, sha1.New)
}

func opSha256(vm *virtualMachine) error {
	return doHash(vm, sha256.New)
}

func opSha3(vm *virtualMachine) error {
	return doHash(vm, sha3.New256)
}

func doHash(vm *virtualMachine, hashFactory func() hash.Hash) error {
	x, err := vm.pop(false)
	if err != nil {
		return err
	}
	cost := int64(len(x))
	if cost < 64 {
		cost = 64
	}
	err = vm.applyCost(cost)
	if err != nil {
		return err
	}
	h := hashFactory()
	_, err = h.Write(x)
	if err != nil {
		return err
	}
	return vm.push(h.Sum(nil), false)
}

func opCheckSig(vm *virtualMachine) error {
	err := vm.applyCost(1024)
	if err != nil {
		return err
	}
	pubkeyBytes, err := vm.pop(false)
	if err != nil {
		return err
	}
	msg, err := vm.pop(false)
	if err != nil {
		return err
	}
	if len(msg) != 32 {
		return ErrBadValue
	}
	sig, err := vm.pop(false)
	if err != nil {
		return err
	}

	pubkey, err := hd25519.PubFromBytes(pubkeyBytes)
	if err != nil {
		return vm.pushBool(false, false)
	}
	return vm.pushBool(ed25519.Verify(pubkey, msg, sig), false)
}

func opCheckMultiSig(vm *virtualMachine) error {
	msg, err := vm.pop(false)
	if err != nil {
		return err
	}
	if len(msg) != 32 {
		return ErrBadValue
	}
	numPubkeys, err := vm.popInt64(false)
	if err != nil {
		return err
	}
	if numPubkeys < 0 {
		return ErrBadValue
	}
	err = vm.applyCost(1024 * numPubkeys)
	if err != nil {
		return err
	}
	pubkeyByteses := make([][]byte, 0, numPubkeys)
	for i := int64(0); i < numPubkeys; i++ {
		pubkeyBytes, err := vm.pop(false)
		if err != nil {
			return err
		}
		pubkeyByteses = append(pubkeyByteses, pubkeyBytes)
	}
	numSigs, err := vm.popInt64(false)
	if err != nil {
		return err
	}
	if numSigs < 0 || numSigs > numPubkeys || (numPubkeys > 0 && numSigs == 0) {
		return ErrBadValue
	}
	sigs := make([][]byte, 0, numSigs)
	for i := int64(0); i < numSigs; i++ {
		sig, err := vm.pop(false)
		if err != nil {
			return err
		}
		sigs = append(sigs, sig)
	}

	pubkeys := make([]ed25519.PublicKey, 0, numPubkeys)
	for _, p := range pubkeyByteses {
		pubkey, err := hd25519.PubFromBytes(p)
		if err != nil {
			return vm.pushBool(false, false)
		}
		pubkeys = append(pubkeys, pubkey)
	}

	// TODO(jackson): Fix this once we're guaranteed to that signatures
	// and public keys will be in the same order.
	used := make([]bool, len(pubkeys))
	for _, sig := range sigs {
		valid := false
		for i, pubkey := range pubkeys {
			if !used[i] && ed25519.Verify(pubkey, msg, sig) {
				valid, used[i] = true, true
				break
			}
		}
		if !valid {
			return vm.pushBool(false, false)
		}
	}
	return vm.pushBool(true, false)
}

func opTxSigHash(vm *virtualMachine) error {
	if vm.tx == nil {
		return ErrContext
	}
	hashType, err := vm.popInt64(false)
	if err != nil {
		return err
	}
	hashBytes := vm.sigHasher.Hash(int(vm.inputIndex), bc.SigHashType(hashType))
	err = vm.applyCost(4 * int64(len(hashBytes)))
	if err != nil {
		return err
	}
	return vm.push(hashBytes[:], false)
}

func opBlockSigHash(vm *virtualMachine) error {
	if vm.block == nil {
		return ErrContext
	}
	h := vm.block.HashForSig()
	err := vm.applyCost(4 * int64(len(h)))
	if err != nil {
		return err
	}
	return vm.push(h[:], false)
}
