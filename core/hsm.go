package core

import (
	"golang.org/x/net/context"

	"chain/core/mockhsm"
	"chain/core/txbuilder"
	"chain/crypto/ed25519/hd25519"
	"chain/encoding/json"
	"chain/errors"
	"chain/net/http/httpjson"
)

func (a *api) mockhsmCreateKey(ctx context.Context, in struct{ Alias string }) (result struct {
	XPub  json.HexBytes `json:"xpub"`
	Alias string        `json:"alias"`
}, err error) {
	xpub, err := a.hsm.CreateKey(ctx, in.Alias)
	if err != nil {
		return result, err
	}
	result.XPub = xpub.Bytes()
	result.Alias = xpub.Alias
	return result, nil
}

func (a *api) mockhsmListKeys(ctx context.Context, query struct{ Cursor string }) (page, error) {
	limit := defGenericPageSize

	xpubs, cursor, err := a.hsm.ListKeys(ctx, query.Cursor, limit)
	if err != nil {
		return page{}, err
	}

	return page{
		Items:    httpjson.Array(xpubs),
		LastPage: len(xpubs) < limit,
		Query:    requestQuery{Cursor: cursor},
	}, nil
}

func (a *api) mockhsmDelKey(ctx context.Context, xpubBytes json.HexBytes) error {
	xpub, err := hd25519.XPubFromBytes(xpubBytes)
	if err != nil {
		return err
	}
	return a.hsm.DelKey(ctx, xpub)
}

func (a *api) mockhsmSignTemplates(ctx context.Context, tpls []*txbuilder.Template) []interface{} {
	resp := make([]interface{}, 0, len(tpls))
	for _, tpl := range tpls {
		err := txbuilder.Sign(ctx, tpl, a.mockhsmSignTemplate)
		if err != nil {
			info, _ := errInfo(err)
			resp = append(resp, info)
		} else {
			resp = append(resp, tpl)
		}
	}
	return resp
}

func (a *api) mockhsmSignTemplate(ctx context.Context, sigComponent *txbuilder.SigScriptComponent, sig *txbuilder.Signature) ([]byte, error) {
	xpub, err := hd25519.XPubFromString(sig.XPub)
	if err != nil {
		return nil, errors.Wrap(err, "parsing xpub")
	}
	sigBytes, err := a.hsm.Sign(ctx, xpub, sig.DerivationPath, sigComponent.SignatureData[:])
	if err == mockhsm.ErrNoKey {
		return nil, nil
	}
	return sigBytes, err
}
