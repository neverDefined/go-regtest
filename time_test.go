package regtest

import (
	"errors"
	"testing"
	"time"
)

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
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
			if err := rt.SetMockTime(tc.unix); err == nil {
				t.Errorf("SetMockTime(%d) should reject", tc.unix)
			}
		})
	}

	if err := rt.SetMockTime(time.Now().Unix()); !errors.Is(err, ErrNotConnected) {
		t.Errorf("pre-Start SetMockTime: want ErrNotConnected, got %v", err)
	}

	if err := rt.MineWithTimestamp(0, time.Now().Unix(), "addr"); err == nil {
		t.Error("MineWithTimestamp(0, ...) should reject")
	}
	if err := rt.MineWithTimestamp(1, time.Now().Unix(), ""); err == nil {
		t.Error("MineWithTimestamp(_, _, \"\") should reject")
	}

	if _, err := rt.WarpTime(0, "addr"); err == nil {
		t.Error("WarpTime(0, ...) should reject")
	}
	if _, err := rt.WarpTime(-time.Hour, "addr"); err == nil {
		t.Error("WarpTime(<0, ...) should reject")
	}
	if _, err := rt.WarpTime(time.Hour, ""); err == nil {
		t.Error("WarpTime(_, \"\") should reject")
	}
}
