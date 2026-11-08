package regtest

import (
	"fmt"
	"sync"
	"testing"
)

func TestWarp(t *testing.T) {
	t.Parallel()
	rt, minerAddr := fundedNode(t)

	startHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count: %v", err)
	}
	t.Logf("starting block count: %d", startHeight)

	startingBalance, err := rt.Client().GetBalance("*")
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	t.Logf("starting balance: %v", startingBalance)

	if err := rt.Warp(10, minerAddr); err != nil {
		t.Fatalf("failed to warp: %v", err)
	}

	endHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count: %v", err)
	}
	t.Logf("ending block count: %d", endHeight)

	if endHeight != startHeight+10 {
		t.Fatalf("block count did not increase by 10: %d != %d", endHeight, startHeight+10)
	}

	// warp 120 blocks and check if the miner address has the rewards
	if err := rt.Warp(120, minerAddr); err != nil {
		t.Fatalf("failed to warp: %v", err)
	}

	endingBalance, err := rt.Client().GetBalance("*")
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}

	if endingBalance < startingBalance {
		t.Fatalf("balance did not increase: %v < %v", endingBalance, startingBalance)
	}

	t.Logf("balance increased: %v -> %v", startingBalance, endingBalance)
}

func TestMiningValidation(t *testing.T) {
	rt := unstarted(t)
	t.Cleanup(func() { _ = rt.Cleanup() })

	const miner = "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl"
	for _, tc := range []struct {
		name   string
		blocks int64
		addr   string
	}{
		{"Warp_zero_blocks", 0, miner},
		{"Warp_negative_blocks", -1, miner},
		{"Warp_empty_miner", 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantArgErr(t, fmt.Sprintf("Warp(%d, %q)", tc.blocks, tc.addr), rt.Warp(tc.blocks, tc.addr))
		})
	}
	wantArgErr(t, "MineToHeight(-1)", rt.MineToHeight(-1, miner))
}

// TestConcurrent_WarpAndSend stresses the dual-mutex pattern (mu for
// lifecycle, clientMu for client access) by interleaving Warp (which mines
// blocks) with SendToAddress (which spends from the wallet). Run under -race
// -count=10 to surface ordering bugs.
func TestConcurrent_WarpAndSend(t *testing.T) {
	t.Parallel()
	rt, minerAddr := fundedNode(t)
	if err := rt.Warp(49, minerAddr); err != nil {
		t.Fatalf("warp to fund: %v", err)
	}

	recipient, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("generate recipient addr: %v", err)
	}

	const iterations = 20
	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := rt.Warp(1, minerAddr); err != nil {
				errs <- fmt.Errorf("warp %d: %w", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := rt.SendToAddress(recipient, 100_000); err != nil {
				errs <- fmt.Errorf("send %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op failed: %v", err)
	}
}

// TestMineToHeight verifies the idempotent target-height helper.
func TestMineToHeight(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	if err := rt.MineToHeight(20, addr); err != nil {
		t.Fatalf("MineToHeight(20): %v", err)
	}
	h, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h != 20 {
		t.Errorf("after MineToHeight(20), height = %d", h)
	}

	// Idempotent: second call mines nothing.
	if err := rt.MineToHeight(20, addr); err != nil {
		t.Fatalf("MineToHeight(20) idempotent: %v", err)
	}
	h2, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h2 != 20 {
		t.Errorf("idempotent call advanced height: %d -> %d", h, h2)
	}

	// target < current is also a no-op.
	if err := rt.MineToHeight(5, addr); err != nil {
		t.Fatalf("MineToHeight(5) when at 20: %v", err)
	}
	h3, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h3 != 20 {
		t.Errorf("backward target rolled chain: %d -> %d", h, h3)
	}

}
