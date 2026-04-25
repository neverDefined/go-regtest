package regtest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// InvalidateBlock marks a block as invalid, rolling the chain back past it.
// On a linear chain this reduces the tip height by however many blocks sit
// at or above the invalidated one. The block (and its descendants) remain
// known to the node and can be reactivated with ReconsiderBlock.
//
// Combined with ReconsiderBlock and PreciousBlock this is the core primitive
// for scripted reorgs — required by soft-fork tests that need to verify a
// signature scheme survives the funding UTXO changing identity (eltoo's
// SIGHASH_ANYPREVOUT rebind property, for example).
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - error: validation error for nil hash; errNotConnected before Start;
//     otherwise wrapped RPC error.
//
// Example:
//
//	tip, _ := rt.GetBestBlockHash()
//	rt.InvalidateBlock(tip) // chain rolls back by one
func (r *Regtest) InvalidateBlock(hash *chainhash.Hash) error {
	return r.InvalidateBlockContext(context.Background(), hash)
}

// InvalidateBlockContext is the context-aware variant of InvalidateBlock.
func (r *Regtest) InvalidateBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.InvalidateBlock(hash)
	})
	if err != nil {
		return fmt.Errorf("invalidateblock %s: %w", hash, err)
	}
	return nil
}

// ReconsiderBlock removes the invalid mark from a block previously marked via
// InvalidateBlock, allowing the node to reactivate it (and its descendants)
// if doing so would extend the most-work chain.
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - error: validation error for nil hash; errNotConnected before Start;
//     otherwise wrapped RPC error.
//
// Example:
//
//	rt.InvalidateBlock(tip)
//	// ... do something with the rolled-back chain
//	rt.ReconsiderBlock(tip) // restore
func (r *Regtest) ReconsiderBlock(hash *chainhash.Hash) error {
	return r.ReconsiderBlockContext(context.Background(), hash)
}

// ReconsiderBlockContext is the context-aware variant of ReconsiderBlock.
func (r *Regtest) ReconsiderBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.ReconsiderBlock(hash)
	})
	if err != nil {
		return fmt.Errorf("reconsiderblock %s: %w", hash, err)
	}
	return nil
}

// PreciousBlock marks a block as preferred when fork-choice is otherwise a
// tie — the active chain switches to whichever fork includes the precious
// block, even if its work is equal to the current tip's. Useful for scripted
// reorg tests where two equally-long chains compete and you want to force
// one to win.
//
// btcsuite has no typed wrapper for preciousblock; this method uses rawRPC.
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - error: validation error for nil hash; errNotConnected before Start;
//     otherwise wrapped RPC error.
func (r *Regtest) PreciousBlock(hash *chainhash.Hash) error {
	return r.PreciousBlockContext(context.Background(), hash)
}

// PreciousBlockContext is the context-aware variant of PreciousBlock.
func (r *Regtest) PreciousBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	// preciousblock takes a hex-encoded block hash. The hash bytes need
	// big-endian (display) ordering, which chainhash.Hash.String() provides.
	raw, err := r.rawRPC(ctx, "preciousblock", hash.String())
	if err != nil {
		return fmt.Errorf("preciousblock %s: %w", hash, err)
	}
	// preciousblock returns null on success; tolerate either null or an
	// empty JSON value.
	if len(raw) > 0 && string(raw) != "null" {
		var ignored json.RawMessage
		if err := json.Unmarshal(raw, &ignored); err != nil {
			return fmt.Errorf("preciousblock unexpected response: %s", raw)
		}
	}
	return nil
}
