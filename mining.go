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

// MineToHeight advances the chain to a specific block height. It reads the
// current height and mines (target - current) blocks via Warp. Idempotent:
// if target is at or below the current height, MineToHeight is a no-op.
//
// Parameters:
//   - target: target block height (must be >= 0)
//   - miner: Bitcoin address to receive coinbase rewards
//
// Returns:
//   - error: validation error for negative target or empty miner;
//     errNotConnected before Start; otherwise wrapped RPC error.
//
// Example:
//
//	if err := rt.MineToHeight(101, addr); err != nil { ... } // mature coinbase
//	if err := rt.MineToHeight(101, addr); err != nil { ... } // no-op
func (r *Regtest) MineToHeight(target int64, miner string) error {
	return r.MineToHeightContext(context.Background(), target, miner)
}

// MineToHeightContext is the context-aware variant of MineToHeight.
func (r *Regtest) MineToHeightContext(ctx context.Context, target int64, miner string) error {
	if target < 0 {
		return fmt.Errorf("target must be >= 0, got %d", target)
	}
	if miner == "" {
		return fmt.Errorf("miner must be provided")
	}
	current, err := r.GetBlockCountContext(ctx)
	if err != nil {
		return fmt.Errorf("get current height: %w", err)
	}
	delta := target - current
	if delta <= 0 {
		return nil
	}
	return r.WarpContext(ctx, delta, miner)
}

// MineUntilActive mines blocks one retarget window at a time until the
// named BIP9 deployment reaches SoftForkActive. Returns the number of
// blocks mined. Polls DeploymentStatus after each window so the BIP9
// state machine has a chance to advance.
//
// This is the canonical "set up a soft-fork for testing" primitive. For
// the typical regtest deployment with start_time=0 and a far-future
// timeout, activation completes in ~3 retarget windows (~432 blocks)
// because Bitcoin Core's default block-version policy signals for all
// currently-STARTED deployments.
//
// Parameters:
//   - deployment: deployment name as known to bitcoind
//   - miner: Bitcoin address to receive coinbase rewards
//   - maxBlocks: hard cap on blocks mined (must be > 0). Activation
//     within fewer than maxBlocks blocks is the success path; reaching
//     maxBlocks without activation is an error reporting the final
//     status.
//
// Returns:
//   - int64: blocks actually mined
//   - error: validation error for empty miner or zero maxBlocks;
//     ErrUnknownDeployment when the deployment name isn't known to
//     bitcoind; SoftForkFailed-related error if the deployment fails;
//     wrapped RPC error otherwise.
//
// Example:
//
//	mined, err := rt.MineUntilActive("testdummy", addr, 2000)
//	if err != nil { return err }
//	fmt.Printf("activated after %d blocks\n", mined)
func (r *Regtest) MineUntilActive(deployment, miner string, maxBlocks int64) (int64, error) {
	return r.MineUntilActiveContext(context.Background(), deployment, miner, maxBlocks)
}

// MineUntilActiveContext is the context-aware variant of MineUntilActive.
func (r *Regtest) MineUntilActiveContext(ctx context.Context, deployment, miner string, maxBlocks int64) (int64, error) {
	if miner == "" {
		return 0, fmt.Errorf("miner must be provided")
	}
	if maxBlocks <= 0 {
		return 0, fmt.Errorf("maxBlocks must be > 0, got %d", maxBlocks)
	}

	// Validate the deployment is known up front so callers get a clean
	// ErrUnknownDeployment instead of mining 1500 blocks for nothing.
	status, err := r.DeploymentStatusContext(ctx, deployment)
	if err != nil {
		return 0, err
	}
	if status == SoftForkActive {
		return 0, nil
	}
	if status == SoftForkFailed {
		return 0, fmt.Errorf("deployment %q is in SoftForkFailed (cannot reach Active)", deployment)
	}

	// Mine in retarget-window-sized chunks (the natural cadence at which
	// the BIP9 state machine advances on regtest). The last chunk is
	// truncated so we never overshoot maxBlocks.
	const window = int64(144)
	var mined int64
	for mined < maxBlocks {
		chunk := window
		if remaining := maxBlocks - mined; remaining < chunk {
			chunk = remaining
		}
		if err := r.WarpContext(ctx, chunk, miner); err != nil {
			return mined, fmt.Errorf("mine chunk: %w", err)
		}
		mined += chunk

		status, err := r.DeploymentStatusContext(ctx, deployment)
		if err != nil {
			return mined, err
		}
		if status == SoftForkActive {
			return mined, nil
		}
		if status == SoftForkFailed {
			return mined, fmt.Errorf("deployment %q reached SoftForkFailed after %d blocks", deployment, mined)
		}
	}

	final, fErr := r.DeploymentStatusContext(ctx, deployment)
	if fErr != nil {
		return mined, fmt.Errorf("deployment %q did not reach Active within %d blocks (status check after maxBlocks failed: %w)", deployment, maxBlocks, fErr)
	}
	return mined, fmt.Errorf("deployment %q did not reach Active within %d blocks (final status: %s)", deployment, maxBlocks, final)
}
