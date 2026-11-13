package regtest

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestSetMockTime_RoundTrip confirms setmocktime takes effect: mining a
// fresh block under a mocktime far in the future advances getblockchaininfo's
// mediantime once enough blocks accumulate at that timestamp. Asserts the
// tip's block.time matches the mocked value within 1s.
func TestSetMockTime_RoundTrip(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)

	target := time.Now().Add(7 * 24 * time.Hour).Unix()
	if err := rt.SetMockTime(target); err != nil {
		t.Fatalf("SetMockTime: %v", err)
	}
	if err := rt.Warp(1, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	hash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	block, err := rt.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	got := block.Header.Timestamp.Unix()
	if delta := got - target; delta < -1 || delta > 1 {
		t.Errorf("tip block.time = %d, want %d ±1 (delta=%d)", got, target, delta)
	}
}

// TestMineWithTimestamp confirms a single MineWithTimestamp call places
// the new tip's block.time at the requested timestamp (within 1s). This is
// the mid-level building block underneath WarpTime.
func TestMineWithTimestamp(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)

	target := time.Now().Add(30 * 24 * time.Hour).Unix()
	if err := rt.MineWithTimestamp(1, target, addr); err != nil {
		t.Fatalf("MineWithTimestamp: %v", err)
	}

	hash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	block, err := rt.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	got := block.Header.Timestamp.Unix()
	if delta := got - target; delta < -1 || delta > 1 {
		t.Errorf("tip block.time = %d, want %d ±1 (delta=%d)", got, target, delta)
	}
}

// TestWarpTime confirms that advancing time by a duration drags MTP
// forward by approximately the same amount, observable via
// getblockchaininfo.mediantime. Tolerates 2s jitter.
func TestWarpTime(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)

	if err := rt.Warp(11, addr); err != nil {
		t.Fatalf("Warp pre-fill: %v", err)
	}

	pre, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo pre: %v", err)
	}

	const advance = 48 * time.Hour
	newMTP, err := rt.WarpTime(advance, addr)
	if err != nil {
		t.Fatalf("WarpTime: %v", err)
	}

	wantMin := pre.MedianTime + int64(advance.Seconds()) - 2
	if newMTP < wantMin {
		t.Errorf("newMTP = %d, want >= %d (pre=%d advance=%s)",
			newMTP, wantMin, pre.MedianTime, advance)
	}

	post, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo post: %v", err)
	}
	if post.MedianTime != newMTP {
		t.Errorf("getblockchaininfo.mediantime = %d, returned newMTP = %d", post.MedianTime, newMTP)
	}
}

func TestTimeValidation(t *testing.T) {
	rt := unstarted(t)
	t.Cleanup(func() { _ = rt.Cleanup() })

	for _, tc := range []struct {
		name string
		unix int64
	}{
		{"SetMockTime_zero", 0},
		{"SetMockTime_negative", -1},
		{"SetMockTime_over_cap", maxMockTime + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantArgErr(t, fmt.Sprintf("SetMockTime(%d)", tc.unix), rt.SetMockTime(tc.unix))
		})
	}

	if err := rt.SetMockTime(time.Now().Unix()); !errors.Is(err, ErrNotConnected) {
		t.Errorf("pre-Start SetMockTime: want ErrNotConnected, got %v", err)
	}

	wantArgErr(t, "MineWithTimestamp(0, ...)", rt.MineWithTimestamp(0, time.Now().Unix(), "addr"))
	wantArgErr(t, `MineWithTimestamp(_, _, "")`, rt.MineWithTimestamp(1, time.Now().Unix(), ""))

	for _, tc := range []struct {
		d     time.Duration
		miner string
	}{
		{0, "addr"},
		{-time.Hour, "addr"},
		{time.Hour, ""},
	} {
		_, err := rt.WarpTime(tc.d, tc.miner)
		wantArgErr(t, fmt.Sprintf("WarpTime(%v, %q)", tc.d, tc.miner), err)
	}
}
