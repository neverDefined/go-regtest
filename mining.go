package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// Warp advances the blockchain by mining the specified number of blocks.
// This is a regtest-specific function that instantly mines blocks and
// sends the block rewards to the specified miner address.
//
// Parameters:
//   - blocks: Number of blocks to mine (must be > 0)
//   - miner: Bitcoin address to receive the block rewards (must be valid)
//
// Returns:
//   - error: Error if parameters are invalid or mining fails
//
// This function is useful for:
//   - Testing applications that depend on block confirmations
//   - Generating test funds by mining to a specific address
//   - Simulating time passage in regtest environments
//   - Creating UTXOs for testing transaction scenarios
//
// Example:
//
//	// Mine 100 blocks to generate test funds
//	err := rt.Warp(100, "bcrt1q...")
//	if err != nil {
//	    return fmt.Errorf("failed to mine blocks: %w", err)
//	}
//	fmt.Println("Mined 100 blocks successfully")
func (r *Regtest) Warp(blocks int64, miner string) error {
	return r.WarpContext(context.Background(), blocks, miner)
}

// WarpContext is the context-aware variant of Warp.
func (r *Regtest) WarpContext(ctx context.Context, blocks int64, miner string) error {
	if blocks <= 0 {
		return fmt.Errorf("blocks must be greater than 0, got %d", blocks)
	}
	if miner == "" {
		return fmt.Errorf("miner must be provided")
	}

	addr, err := btcutil.DecodeAddress(miner, &chaincfg.RegressionNetParams)
	if err != nil {
		return fmt.Errorf("failed to decode miner address: %w", err)
	}

	client, err := r.lockedClient()
	if err != nil {
		return err
	}

	_, err = runWithContext(ctx, func() ([]*chainhash.Hash, error) {
		return client.GenerateToAddress(blocks, addr, nil)
	})
	if err != nil {
		return fmt.Errorf("failed to generate blocks: %w", err)
	}
	return nil
}
