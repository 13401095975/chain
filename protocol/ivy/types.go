package ivy

type typeDesc string

var (
	amountType   = typeDesc("Amount")
	assetType    = typeDesc("Asset")
	boolType     = typeDesc("Boolean")
	contractType = typeDesc("Contract")
	hashType     = typeDesc("Hash")
	intType      = typeDesc("Integer")
	listType     = typeDesc("List")
	nilType      = typeDesc("")
	predType     = typeDesc("Predicate")
	progType     = typeDesc("Program")
	pubkeyType   = typeDesc("PublicKey")
	sigType      = typeDesc("Signature")
	strType      = typeDesc("String")
	timeType     = typeDesc("Time")
	valueType    = typeDesc("Value")

	sha3StrType      = typeDesc("Sha3(String)")
	sha3PubkeyType   = typeDesc("Sha3(PublicKey)")
	sha256StrType    = typeDesc("Sha256(String)")
	sha256PubkeyType = typeDesc("Sha256(PublicKey)")
)

var types = map[string]typeDesc{
	string(amountType): amountType,
	string(assetType):  assetType,
	string(boolType):   boolType,
	string(hashType):   hashType,
	string(intType):    intType,
	string(listType):   listType,
	string(nilType):    nilType,
	string(predType):   predType,
	string(progType):   progType,
	string(pubkeyType): pubkeyType,
	string(sigType):    sigType,
	string(strType):    strType,
	string(timeType):   timeType,
	string(valueType):  valueType,

	string(sha3StrType):      sha3StrType,
	string(sha3PubkeyType):   sha3PubkeyType,
	string(sha256StrType):    sha256StrType,
	string(sha256PubkeyType): sha256PubkeyType,
}

func isHashSubtype(t typeDesc) bool {
	switch t {
	case sha3StrType, sha3PubkeyType, sha256StrType, sha256PubkeyType:
		return true
	}
	return false
}

func propagateType(contract *contract, clause *clause, env environ, t typeDesc, e expression) {
	v, ok := e.(varRef)
	if !ok {
		return
	}
	for _, p := range contract.params {
		if p.name == string(v) {
			p.inferredType = t
			if entry, ok := env[string(v)]; ok {
				entry.t = t
			}
			return
		}
	}
	for _, p := range clause.params {
		if p.name == string(v) {
			p.inferredType = t
			if entry, ok := env[string(v)]; ok {
				entry.t = t
			}
			return
		}
	}
}
