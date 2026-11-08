package regtest

import (
	"context"
	"fmt"
	"time"
)

// maxMockTime is the largest value Bitcoin Core's setmocktime accepts: the
// value in nanoseconds must fit in an int64. It is around the year 2262.
const maxMockTime int64 = 9_223_372_036

// maxBlockTime is the largest timestamp a block header can hold (nTime is a
// uint32), in the year 2106. Above it the block bitcoind builds overflows and
// generatetoaddress fails with a misleading "time-too-old" error, so
// MineWithTimestamp and WarpTime reject such values up front.
const maxBlockTime int64 = 4_294_967_295

// mtpWindow is the number of blocks whose median timestamp is the Median Time
// Past (BIP113). WarpTime mines mtpWindow + 1 blocks at one timestamp to move
// MTP to it.
const mtpWindow int64 = 11

// SetMockTime sets the node's clock to unix (Unix seconds) with setmocktime.
// Blocks mined afterwards take their timestamps from it. The mock time stays
// in effect until bitcoind stops or SetMockTime is called again. unix must be
// from 1 to 9223372036 (about the year 2262).
func (r *Regtest) SetMockTime(unix int64) error {
	return r.SetMockTimeContext(context.Background(), unix)
}

// SetMockTimeContext is like SetMockTime but takes a context.
func (r *Regtest) SetMockTimeContext(ctx context.Context, unix int64) error {
	if unix <= 0 {
		return fmt.Errorf("SetMockTime: unix must be > 0, got %d", unix)
	}
	if unix > maxMockTime {
		return fmt.Errorf("SetMockTime: unix must be ≤ %d (Core's int64-nanos cap), got %d", maxMockTime, unix)
	}
	if _, err := r.rawRPC(ctx, "setmocktime", unix); err != nil {
		return fmt.Errorf("SetMockTime: %w", err)
	}
	return nil
}

// MineWithTimestamp sets the mock time to unix and mines blocks to miner, so
// they are stamped at unix (to within a second). The mock time stays set
// afterwards, as with SetMockTime. unix must be from 1 to 4294967295 (the
// uint32 block-time limit, in 2106).
func (r *Regtest) MineWithTimestamp(blocks, unix int64, miner string) error {
	return r.MineWithTimestampContext(context.Background(), blocks, unix, miner)
}

// MineWithTimestampContext is like MineWithTimestamp but takes a context.
func (r *Regtest) MineWithTimestampContext(ctx context.Context, blocks, unix int64, miner string) error {
	if blocks <= 0 {
		return fmt.Errorf("MineWithTimestamp: blocks must be > 0, got %d", blocks)
	}
	if miner == "" {
		return fmt.Errorf("MineWithTimestamp: miner must be provided")
	}
	// Stricter than SetMockTime's range: block.nTime is uint32, so anything
	// above maxBlockTime overflows when bitcoind constructs the block and
	// generatetoaddress fails with "time-too-old". Catch it up front.
	if unix > maxBlockTime {
		return fmt.Errorf("MineWithTimestamp: unix must be ≤ %d (uint32 block timestamp cap, ~year 2106), got %d", maxBlockTime, unix)
	}
	if err := r.SetMockTimeContext(ctx, unix); err != nil {
		return err
	}
	return r.WarpContext(ctx, blocks, miner)
}

// WarpTime advances the chain's Median Time Past (MTP) by duration and
// returns the new MTP in Unix seconds. It sets the mock time to the tip's
// MTP plus duration and mines 12 blocks to miner at that time; the mock time
// stays set afterwards. The new MTP is at least the old one plus duration, to
// within a second.
//
// Use it to test rules that depend on MTP, such as BIP9 timeouts, relative
// time locks or BIP113 lock times, without mining a retarget window of
// blocks.
func (r *Regtest) WarpTime(duration time.Duration, miner string) (int64, error) {
	return r.WarpTimeContext(context.Background(), duration, miner)
}

// WarpTimeContext is like WarpTime but takes a context.
func (r *Regtest) WarpTimeContext(ctx context.Context, duration time.Duration, miner string) (int64, error) {
	if duration <= 0 {
		return 0, fmt.Errorf("WarpTime: duration must be > 0, got %s", duration)
	}
	if miner == "" {
		return 0, fmt.Errorf("WarpTime: miner must be provided")
	}

	info, err := r.GetBlockChainInfoContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("WarpTime: read tip: %w", err)
	}
	target := info.MedianTime + int64(duration.Seconds())
	if target > maxBlockTime {
		return 0, fmt.Errorf("WarpTime: target %d exceeds uint32 block-timestamp cap %d (~year 2106); pick a smaller duration",
			target, maxBlockTime)
	}

	// Mine mtpWindow + 1 blocks all stamped at target. After mtpWindow + 1
	// fresh blocks, the most recent mtpWindow are all at target so the new
	// tip's MTP (median of those mtpWindow) equals target.
	if err := r.MineWithTimestampContext(ctx, mtpWindow+1, target, miner); err != nil {
		return 0, fmt.Errorf("WarpTime: %w", err)
	}

	postInfo, err := r.GetBlockChainInfoContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("WarpTime: re-read tip: %w", err)
	}
	return postInfo.MedianTime, nil
}
