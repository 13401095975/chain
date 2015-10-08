package utxodb

type byKey []Input

func (a byKey) Len() int      { return len(a) }
func (a byKey) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byKey) Less(i, j int) bool {
	if a[i].BucketID == a[j].BucketID {
		return a[i].AssetID < a[j].AssetID
	}
	return a[i].BucketID < a[j].BucketID
}

type byKeyUTXO []*UTXO

func (u byKeyUTXO) Len() int      { return len(u) }
func (u byKeyUTXO) Swap(i, j int) { u[i], u[j] = u[j], u[i] }
func (u byKeyUTXO) Less(i, j int) bool {
	if u[i].BucketID == u[j].BucketID {
		return u[i].AssetID < u[j].AssetID
	}
	return u[i].BucketID < u[j].BucketID
}

type utxosByResvExpires []*UTXO

func (u utxosByResvExpires) Len() int { return len(u) }
func (u utxosByResvExpires) Swap(i, j int) {
	u[i], u[j] = u[j], u[i]
	u[i].heapIndex = i
	u[j].heapIndex = j
}
func (u *utxosByResvExpires) Push(x interface{}) {
	utxo := x.(*UTXO)
	utxo.heapIndex = len(*u)
	*u = append(*u, utxo)
}
func (u *utxosByResvExpires) Pop() interface{} {
	x := (*u)[len(*u)-1]
	*u = (*u)[:len(*u)-1]
	return x
}
func (u utxosByResvExpires) Less(i, j int) bool {
	if u[i].ResvExpires.Equal(u[j].ResvExpires) {
		// TODO(kr): sort by something better (age?)
		return byKeyUTXO(u).Less(i, j)
	}
	return u[i].ResvExpires.Before(u[j].ResvExpires)
}
