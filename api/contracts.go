package api

import (
	"golang.org/x/net/context"

	"chain/api/appdb"
	"chain/api/smartcontracts/orderbook"
	"chain/api/smartcontracts/voting"
	"chain/api/txbuilder"
	"chain/database/pg"
	"chain/errors"
	"chain/fedchain/bc"
	"chain/net/http/httpjson"
)

type globalFindOrder struct {
	OfferedAssetIDs []bc.AssetID `json:"offered_asset_ids"`
	PaymentAssetIDs []bc.AssetID `json:"payment_asset_ids"`
}

func findOrders(ctx context.Context, req globalFindOrder) ([]*orderbook.OpenOrder, error) {
	qvals := httpjson.Request(ctx).URL.Query()
	if status, ok := qvals["status"]; !ok || status[0] != "open" {
		// TODO(tessr): find closed orders
		return nil, errors.Wrap(httpjson.ErrBadRequest, "unimplemented: find all orders")
	}
	orders, err := orderbook.FindOpenOrders(ctx, req.OfferedAssetIDs, req.PaymentAssetIDs)
	if err != nil {
		return nil, errors.Wrap(err, "finding orders by offered and payment asset ids")
	}

	return orders, nil
}

func findAccountOrders(ctx context.Context, accountID string) ([]*orderbook.OpenOrder, error) {
	qvals := httpjson.Request(ctx).URL.Query()
	if status, ok := qvals["status"]; !ok || status[0] != "open" {
		// TODO(tessr): find closed orders
		return nil, errors.Wrap(httpjson.ErrBadRequest, "unimplemented: find all orders")
	}
	if aids, ok := qvals["asset_id"]; ok {
		var assetIDs []bc.AssetID
		for _, id := range aids {
			var assetID bc.AssetID
			err := assetID.UnmarshalText([]byte(id))
			if err != nil {
				return nil, errors.Wrap(httpjson.ErrBadRequest, "invalid assetID")
			}
			assetIDs = append(assetIDs, assetID)
		}
		orders, err := orderbook.FindOpenOrdersBySellerAndAsset(ctx, accountID, assetIDs)
		if err != nil {
			return nil, errors.Wrap(err, "finding orders by seller and asset")
		}
		return orders, nil
	}
	orders, err := orderbook.FindOpenOrdersBySeller(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return orders, nil
}

func findAccountVotingRights(ctx context.Context, accountID string) ([]map[string]interface{}, error) {
	rightsWithUTXOs, err := voting.FindRightsForAccount(ctx, accountID)
	if err != nil {
		return nil, errors.Wrap(err, "finding account voting rights")
	}

	rights := make([]map[string]interface{}, 0, len(rightsWithUTXOs))
	for _, r := range rightsWithUTXOs {
		var actionTypes []string
		if r.Outpoint.Hash == r.UTXO.Hash && r.Outpoint.Index == r.UTXO.Index {
			actionTypes = append(actionTypes, "vrtoken-transfer", "vrtoken-delegate")
		} else {
			actionTypes = append(actionTypes, "vrtoken-recall")
		}

		rightToken := map[string]interface{}{
			"asset_id":       r.AssetID,
			"action_types":   actionTypes,
			"transaction_id": r.UTXO.Hash,
			"index":          r.UTXO.Index,
		}
		rights = append(rights, rightToken)
	}
	return rights, nil
}

// parseVotingBuildRequest parses `vrtoken` BuildRequest sources and
// destinations. Unlike other asset types, voting request inputs and
// outputs need data from each other in order to build the correct
// txbuilder.Reservers and txbuilder.Receivers.
//
// This function will pair the vrtoken sources and destinations up by
// asset ID, and use the information from both to construct the
// txbuilder.Sources and txbuilder.Destinations.
func parseVotingBuildRequest(ctx context.Context, sources []*Source, destinations []*Destination) (srcs []*txbuilder.Source, dsts []*txbuilder.Destination, err error) {
	var (
		srcsByAssetID = map[bc.AssetID]*Source{}
		dstsByAssetID = map[bc.AssetID]*Destination{}
	)
	// Pair voting rights up by asset id.
	for _, src := range sources {
		if src.AssetID == nil {
			return nil, nil, errors.WithDetail(ErrBadBuildRequest, "asset type unspecified")
		}
		if _, ok := srcsByAssetID[*src.AssetID]; ok {
			return nil, nil, errors.WithDetail(ErrBadBuildRequest, "voting right asset appears twice as source")
		}
		srcsByAssetID[*src.AssetID] = src
	}
	for _, dst := range destinations {
		if dst.AssetID == nil {
			return nil, nil, errors.WithDetail(ErrBadBuildRequest, "asset type unspecified")
		}
		if _, ok := dstsByAssetID[*dst.AssetID]; ok {
			return nil, nil, errors.WithDetail(ErrBadBuildRequest, "voting right asset appears twice as destination")
		}
		dstsByAssetID[*dst.AssetID] = dst
	}
	if len(sources) != len(destinations) {
		// Both the source and destination must be provided in the same build
		// request. This is unavoidable because:
		// - the output contract script requires knowledge of the input's chain of ownership
		// - the sigscript needs to provide the new contract parameters
		return nil, nil, errors.WithDetailf(ErrBadBuildRequest,
			"voting right source and destinations must be provided in the same build request")
	}

	for assetID, src := range srcsByAssetID {
		dst, ok := dstsByAssetID[assetID]
		if !ok {
			return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "unknown voting right destination")
		}
		if src.TxHash == nil {
			src.TxHash = src.TxHashAsID
		}
		if src.TxHash == nil || src.Index == nil {
			return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "bad voting right source")
		}
		if src.Amount != 1 || dst.Amount != 1 {
			return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "voting right amount can only be 1")
		}
		out := bc.Outpoint{Hash: *src.TxHash, Index: *src.Index}

		// Lookup the voting right by the outpoint. We'll need some of its
		// script data, such as the previous chain of ownership.
		old, err := voting.FindRightForOutpoint(ctx, out)
		if err == pg.ErrUserInputNotFound {
			return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "bad voting right source")
		} else if err != nil {
			return nil, nil, err
		}

		var (
			reserver txbuilder.Reserver
			receiver txbuilder.Receiver
		)
		switch src.Type {
		case "vrtoken-transfer":
			script := dst.Script[:]
			if script == nil {
				addr, err := appdb.NewAddress(ctx, dst.AccountID, true)
				if err != nil {
					return nil, nil, errors.Wrapf(err, "generating address, accountID %s", src.AccountID)
				}
				script = addr.PKScript
			}
			reserver, receiver, err = voting.RightTransfer(ctx, old, script)
			if err != nil {
				return nil, nil, err
			}
		case "vrtoken-delegate":
			if !old.Delegatable {
				return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "delegating this voting right is prohibited")
			}
			if dst.Deadline.Unix() > old.Deadline {
				return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "cannot extend deadline beyond current deadline")
			}

			script := dst.Script[:]
			if script == nil {
				addr, err := appdb.NewAddress(ctx, dst.AccountID, true)
				if err != nil {
					return nil, nil, errors.Wrapf(err, "generating address, accountID %s", src.AccountID)
				}
				script = addr.PKScript
			}
			var (
				delegatable = old.Delegatable
				deadline    = old.Deadline
			)
			if dst.Transferable != nil {
				delegatable = *dst.Transferable
			}
			if !dst.Deadline.IsZero() {
				deadline = dst.Deadline.Unix()
			}
			reserver, receiver, err = voting.RightDelegation(ctx, old, script, deadline, delegatable)
			if err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, errors.WithDetailf(ErrBadBuildRequest, "`%s` source type unimplemented", src.Type)
		}
		srcs = append(srcs, &txbuilder.Source{
			AssetAmount: bc.AssetAmount{AssetID: assetID, Amount: 1},
			Reserver:    reserver,
		})
		dsts = append(dsts, &txbuilder.Destination{
			AssetAmount: bc.AssetAmount{AssetID: assetID, Amount: 1},
			Metadata:    dst.Metadata,
			Receiver:    receiver,
		})
	}
	return srcs, dsts, nil
}
