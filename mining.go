package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

// Warp mines the given number of blocks and pays their coinbases to miner.
// blocks must be positive.
func (r *Regtest) Warp(blocks int64, miner string) error {
	return r.WarpContext(context.Background(), blocks, miner)
}

// WarpContext is like Warp but takes a context.
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

	if _, err := r.rawRPC(ctx, "generatetoaddress", blocks, addr.EncodeAddress()); err != nil {
		return fmt.Errorf("failed to generate blocks: %w", err)
	}
	return nil
}

// MineToHeight mines blocks to miner until the chain tip is at height
// target. It does nothing if the tip is already at or above target.
func (r *Regtest) MineToHeight(target int64, miner string) error {
	return r.MineToHeightContext(context.Background(), target, miner)
}

// MineToHeightContext is like MineToHeight but takes a context.
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

// MineUntilActive mines blocks to miner, one 144-block retarget window at a
// time, until the named deployment is SoftForkActive, and returns how many
// blocks it mined. It mines nothing if the deployment is already active.
//
// A deployment with start time 0 and a far-future timeout activates after
// about three windows (432 blocks), because Bitcoin Core's default block
// version signals for every started deployment.
//
// It mines at most maxBlocks blocks, which must be positive, and returns an
// error if the deployment isn't active by then or reaches SoftForkFailed. If
// bitcoind doesn't know the deployment, it returns ErrUnknownDeployment before
// mining anything.
func (r *Regtest) MineUntilActive(deployment, miner string, maxBlocks int64) (int64, error) {
	return r.MineUntilActiveContext(context.Background(), deployment, miner, maxBlocks)
}

// MineUntilActiveContext is like MineUntilActive but takes a context.
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
