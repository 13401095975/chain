package appdb

const (
	CustomerPaymentNamespace = 0
	CustomerAssetsNamespace  = 1
)

func ReceiverPath(addrInfo *Address, addrIndex []uint32) []uint32 {
	return []uint32{
		CustomerPaymentNamespace,
		addrInfo.AccountIndex[0],
		addrInfo.AccountIndex[1],
		addrIndex[0],
		addrIndex[1],
	}
}

func IssuancePath(asset *Asset) []uint32 {
	return []uint32{
		CustomerAssetsNamespace,
		asset.INIndex[0],
		asset.INIndex[1],
		asset.AIndex[0],
		asset.AIndex[1],
	}
}
