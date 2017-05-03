let assert = require('assert')

let compileTemplate = require('../lib/index.js').compileTemplate

let testTemplates = {
  "TrivialLock": `contract TrivialLock(locked: Value) {
  clause unlock() {
    return locked  
  }
}`,
  "LockWithPublicKey": `contract LockWithPublicKey(publicKey: PublicKey, locked: Value) {
  clause unlock(sig: Signature) {
    verify checkTxSig(publicKey, sig)
    return locked  
  }
}`,
  "LockToOutput": `contract LockToOutput(program: Program, locked: Value) {
  clause unlock() {
    output program(locked)  
  }
}`,
  "TradeOffer": `contract TradeOffer(requested: AssetAmount, sellerControlProgram: Program, cancellationHash: Hash, offered: Value) {
  clause trade(payment: Value) {
    verify payment.assetAmount == requested
    output sellerControlProgram(payment)
    return offered
  }
  clause cancel(cancellationSecret: String) {
    verify sha3(cancellationSecret) == cancellationHash
    output sellerControlProgram(offered)
  }
}`
}

describe('compileTemplate', () => {
  it('should allow value to be locked and returned', () => {
    let compiled = compileTemplate(testTemplates["TrivialLock"])
    assert.equal(compiled.message, undefined);
  });
  it('should allow signature checking', () => {
    let compiled = compileTemplate(testTemplates["LockWithPublicKey"])
    assert.equal(compiled.message, undefined);
  });
  it('should allow value to be output', () => {
    let compiled = compileTemplate(testTemplates["LockToOutput"])
    assert.equal(compiled.message, undefined);
  });
  it('should accept input values', () => {
    let compiled = compileTemplate(testTemplates["TradeOffer"])
    assert.equal(compiled.message, undefined);
  });
});

