package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// GetBlockTemplate returns a template for assembling a block by hand and
// passing it to SubmitBlock, for example to include a transaction that mempool
// policy rejects even with -acceptnonstdtxn. GenerateBlock is simpler when you
// only need to choose the transactions.
//
// A basic request is &btcjson.TemplateRequest{Mode: "template", Rules:
// []string{"segwit"}}. Depending on the Core version, Rules may also need to
// list active deployments such as "taproot".
func (r *Regtest) GetBlockTemplate(req *btcjson.TemplateRequest) (*btcjson.GetBlockTemplateResult, error) {
	return r.GetBlockTemplateContext(context.Background(), req)
}

// GetBlockTemplateContext is like GetBlockTemplate but takes a context.
func (r *Regtest) GetBlockTemplateContext(ctx context.Context, req *btcjson.TemplateRequest) (*btcjson.GetBlockTemplateResult, error) {
	var args []any
	if req != nil {
		args = append(args, req)
	}
	res, err := call[btcjson.GetBlockTemplateResult](ctx, r, "getblocktemplate", args...)
	if err != nil {
		return nil, fmt.Errorf("getblocktemplate: %w", err)
	}
	return &res, nil
}

// SubmitBlock submits an assembled block. Its coinbase, witness commitment and
// proof of work must already be valid. If bitcoind rejects the block, the error
// contains the reason, such as "bad-cb-amount" or "high-hash".
func (r *Regtest) SubmitBlock(block *wire.MsgBlock) error {
	return r.SubmitBlockContext(context.Background(), block)
}

// SubmitBlockContext is like SubmitBlock but takes a context.
func (r *Regtest) SubmitBlockContext(ctx context.Context, block *wire.MsgBlock) error {
	if block == nil {
		return fmt.Errorf("block must not be nil")
	}
	h, err := blockHex(block)
	if err != nil {
		return err
	}
	// submitblock returns null on success and a reject reason (e.g.
	// "bad-cb-amount") as a plain result, not an RPC error, on failure.
	reason, err := call[*string](ctx, r, "submitblock", h)
	if err != nil {
		return fmt.Errorf("submitblock: %w", err)
	}
	if reason != nil {
		return fmt.Errorf("submitblock: %s", *reason)
	}
	return nil
}

// GenerateBlock mines one block that contains exactly txs, in order, and
// pays the coinbase to miner. The transactions skip mempool policy but must
// be consensus-valid, so this is how to confirm a transaction that
// TestMempoolAccept rejects as non-standard. With no txs it mines an empty
// block.
func (r *Regtest) GenerateBlock(miner string, txs ...*wire.MsgTx) (*chainhash.Hash, error) {
	return r.GenerateBlockContext(context.Background(), miner, txs...)
}

// GenerateBlockContext is like GenerateBlock but takes a context.
func (r *Regtest) GenerateBlockContext(ctx context.Context, miner string, txs ...*wire.MsgTx) (*chainhash.Hash, error) {
	if miner == "" {
		return nil, fmt.Errorf("miner must be provided")
	}
	addr, err := btcutil.DecodeAddress(miner, &chaincfg.RegressionNetParams)
	if err != nil {
		return nil, fmt.Errorf("decode miner address: %w", err)
	}
	raw := make([]string, len(txs))
	for i, tx := range txs {
		if tx == nil {
			return nil, fmt.Errorf("txs[%d] must not be nil", i)
		}
		if raw[i], err = txHex(tx); err != nil {
			return nil, err
		}
	}
	res, err := call[struct {
		Hash string `json:"hash"`
	}](ctx, r, "generateblock", addr.EncodeAddress(), raw)
	if err != nil {
		return nil, fmt.Errorf("generateblock: %w", err)
	}
	hash, err := chainhash.NewHashFromStr(res.Hash)
	if err != nil {
		return nil, fmt.Errorf("generateblock: %w", err)
	}
	return hash, nil
}
