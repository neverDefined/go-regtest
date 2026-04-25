package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
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
//   - error: errNotConnected before Start; otherwise wrapped RPC error.
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
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	res, err := runWithContext(ctx, func() (*btcjson.GetBlockTemplateResult, error) {
		return client.GetBlockTemplate(req)
	})
	if err != nil {
		return nil, fmt.Errorf("getblocktemplate: %w", err)
	}
	return res, nil
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
//   - error: validation error for nil block; errNotConnected before Start;
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
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.SubmitBlock(btcutil.NewBlock(block), nil)
	})
	if err != nil {
		return fmt.Errorf("submitblock: %w", err)
	}
	return nil
}
