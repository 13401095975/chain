package testutil

import (
	"github.com/btcsuite/btcutil/hdkeychain"

	"chain/fedchain/hdkey"
)

var (
	TestXPub, TestXPrv *hdkey.XKey
)

func init() {
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		panic(err)
	}
	xprv, err := hdkeychain.NewMaster(seed)
	if err != nil {
		panic(err)
	}
	xpub, err := xprv.Neuter()
	if err != nil {
		panic(err)
	}
	TestXPub = &hdkey.XKey{ExtendedKey: *xpub}
	TestXPrv = &hdkey.XKey{ExtendedKey: *xprv}
}

// XPubs parses the serialized xpubs in a.
// If there is a parsing error, it panics.
func XPubs(a ...string) (ks []*hdkey.XKey) {
	for _, s := range a {
		xk, err := hdkey.NewXKey(s)
		if err != nil {
			panic(err)
		}
		ks = append(ks, xk)
	}
	return ks
}
