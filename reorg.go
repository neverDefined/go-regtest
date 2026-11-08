package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// InvalidateBlock marks the block with the given hash invalid, which removes
// it and its descendants from the active chain. On a chain without forks the
// new tip is the block's parent. The node keeps the blocks, and
// ReconsiderBlock undoes the mark.
func (r *Regtest) InvalidateBlock(hash *chainhash.Hash) error {
	return r.InvalidateBlockContext(context.Background(), hash)
}

// InvalidateBlockContext is like InvalidateBlock but takes a context.
func (r *Regtest) InvalidateBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	if _, err := r.rawRPC(ctx, "invalidateblock", hash.String()); err != nil {
		return fmt.Errorf("invalidateblock %s: %w", hash, err)
	}
	return nil
}

// ReconsiderBlock clears the invalid mark from the block, its ancestors and
// its descendants, undoing InvalidateBlock. The node then switches to the
// chain with the most work, which may include them.
func (r *Regtest) ReconsiderBlock(hash *chainhash.Hash) error {
	return r.ReconsiderBlockContext(context.Background(), hash)
}

// ReconsiderBlockContext is like ReconsiderBlock but takes a context.
func (r *Regtest) ReconsiderBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	if _, err := r.rawRPC(ctx, "reconsiderblock", hash.String()); err != nil {
		return fmt.Errorf("reconsiderblock %s: %w", hash, err)
	}
	return nil
}

// PreciousBlock makes the node prefer the chain containing the block over
// other chains with the same work, switching to it if needed. Use it to pick
// the winner between two equally long forks.
func (r *Regtest) PreciousBlock(hash *chainhash.Hash) error {
	return r.PreciousBlockContext(context.Background(), hash)
}

// PreciousBlockContext is like PreciousBlock but takes a context.
func (r *Regtest) PreciousBlockContext(ctx context.Context, hash *chainhash.Hash) error {
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}
	if _, err := r.rawRPC(ctx, "preciousblock", hash.String()); err != nil {
		return fmt.Errorf("preciousblock %s: %w", hash, err)
	}
	return nil
}
