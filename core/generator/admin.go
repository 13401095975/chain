package generator

import (
	"golang.org/x/net/context"

	"chain/core/appdb"
	"chain/core/txdb"
	"chain/errors"
)

// TxCount describes the number of transactions in a blockchain.
type TxCount struct {
	Confirmed   uint64 `json:"confirmed"`
	Unconfirmed uint64 `json:"unconfirmed"`
}

// NodePermStatus describes the permission status of a single node on the
// network.
type NodePermStatus struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

// NodePerms describes the complete permission status for all nodes
// in the network.
type NodePerms struct {
	ManagerNodes []NodePermStatus `json:"manager_nodes"`
	IssuerNodes  []NodePermStatus `json:"issuer_nodes"`
	AuditorNodes []NodePermStatus `json:"auditor_nodes"`
}

func newNodePerms() *NodePerms {
	return &NodePerms{
		[]NodePermStatus{},
		[]NodePermStatus{},
		[]NodePermStatus{},
	}
}

// Summary is a composite of useful information about the state of a blockchain
// and its network.
type Summary struct {
	BlockFreqMs      uint64    `json:"block_frequency"`
	BlockCount       uint64    `json:"block_count"`
	TransactionCount TxCount   `json:"transaction_count"`
	Permissions      NodePerms `json:"permissions"`
}

func newSummary() *Summary {
	return &Summary{Permissions: *newNodePerms()}
}

// GetSummary returns a Summary from the perspective of the given project.
func GetSummary(ctx context.Context, store *txdb.Store, projID string) (*Summary, error) {
	res := newSummary()

	res.BlockFreqMs = uint64(blockPeriod.Nanoseconds() / 1000000)

	top, err := store.LatestBlock(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "get latest block")
	}
	res.BlockCount = top.Height + 1 // Height 0 is the genesis block

	res.TransactionCount.Confirmed, err = store.CountBlockTxs(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "count block txs")
	}

	res.TransactionCount.Unconfirmed, err = store.CountPoolTxs(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "count pool txs")
	}

	mnodes, err := appdb.ListManagerNodes(ctx, projID)
	if err != nil {
		return nil, errors.Wrap(err, "list manager nodes")
	}
	for _, n := range mnodes {
		res.Permissions.ManagerNodes = append(res.Permissions.ManagerNodes, NodePermStatus{
			ID:      n.ID,
			Label:   n.Label,
			Enabled: true, // this is spoofed
		})
	}

	inodes, err := appdb.ListIssuerNodes(ctx, projID)
	if err != nil {
		return nil, errors.Wrap(err, "list issuer nodes")
	}
	for _, n := range inodes {
		res.Permissions.IssuerNodes = append(res.Permissions.IssuerNodes, NodePermStatus{
			ID:      n.ID,
			Label:   n.Label,
			Enabled: true, // this is spoofed
		})
	}

	// Spoof an auditor node
	res.Permissions.AuditorNodes = append(res.Permissions.AuditorNodes, NodePermStatus{
		ID:      "audnode-" + projID,
		Label:   "Auditor Node for " + projID,
		Enabled: true,
	})

	return res, nil
}
