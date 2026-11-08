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

// GetBlockTemplate returns a block template suitable for assembly and
// submission via SubmitBlock. The "no mempool" path: build a block that
// includes a target tx directly, bypassing policy checks. Useful for
// consensus-rule testing where a tx is consensus-valid but policy-rejected
// even with -acceptnonstdtxn.
//
// Parameters:
//   - req: template request (mode, rules, etc.). Pass &btcjson.TemplateRequest{
//     Mode: "template", Rules: []string{"segwit"}} for a basic regtest template;
//     additional rules (e.g. "taproot") may be required to advertise support
//     for active deployments depending on Core version.
//
// Returns:
//   - *btcjson.GetBlockTemplateResult: previous block hash, target, height,
//     coinbase value, witness commitment, and the candidate tx list.
//   - error: ErrNotConnected before Start; otherwise wrapped RPC error.
//
// Example:
//
//	tmpl, err := rt.GetBlockTemplate(&btcjson.TemplateRequest{
//	    Mode: "template", Rules: []string{"segwit"},
//	})
//	if err != nil { return err }
//	fmt.Println("template height:", tmpl.Height)
func (r *Regtest) GetBlockTemplate(req *btcjson.TemplateRequest) (*btcjson.GetBlockTemplateResult, error) {
	return r.GetBlockTemplateContext(context.Background(), req)
}

// GetBlockTemplateContext is the context-aware variant of GetBlockTemplate.
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

// SubmitBlock submits an assembled block to bitcoind. The companion to
// GetBlockTemplate for "include this tx in a block without going through the
// mempool" patterns common in consensus-focused soft-fork tests.
//
// Parameters:
//   - block: the block to submit (must be non-nil). Coinbase, witness
//     commitment, and proof-of-work must already be valid.
//
// Returns:
//   - error: validation error for nil block; ErrNotConnected before Start;
//     otherwise wrapped RPC error including bitcoind's reject reason
//     ("bad-cb-amount", "high-hash", "block-validation-failed", etc.).
//
// Example:
//
//	if err := rt.SubmitBlock(myBlock); err != nil {
//	    return fmt.Errorf("submit: %w", err)
//	}
func (r *Regtest) SubmitBlock(block *wire.MsgBlock) error {
	return r.SubmitBlockContext(context.Background(), block)
}

// SubmitBlockContext is the context-aware variant of SubmitBlock.
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
// block. Wraps generateblock (Bitcoin Core 22+).
//
// Example:
//
//	res, _ := rt.TestMempoolAccept(tx) // rejected: non-standard
//	hash, err := rt.GenerateBlock(minerAddr, tx)
func (r *Regtest) GenerateBlock(miner string, txs ...*wire.MsgTx) (*chainhash.Hash, error) {
	return r.GenerateBlockContext(context.Background(), miner, txs...)
}

// GenerateBlockContext is the context-aware variant of GenerateBlock.
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
