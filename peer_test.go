package regtest

import (
	"testing"
	"time"
)

func TestConnect_Sync(t *testing.T) {
	t.Parallel()
	rt1, rt2 := StartT(t, nil), StartT(t, nil)

	if err := rt1.Connect(rt2); err != nil {
		t.Fatalf("rt1.Connect(rt2): %v", err)
	}

	waitForMinConnections(t, 15*time.Second, 1, rt1, rt2)

	if err := rt1.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt1.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt1.Warp(5, miner); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	if err := SyncBlocks(rt1, rt2); err != nil {
		t.Fatalf("SyncBlocks: %v", err)
	}
	want, err := rt1.GetBlockCount()
	if err != nil {
		t.Fatalf("rt1.GetBlockCount: %v", err)
	}
	got, err := rt2.GetBlockCount()
	if err != nil {
		t.Fatalf("rt2.GetBlockCount: %v", err)
	}
	if got != want {
		t.Fatalf("rt2 height %d != rt1 %d", got, want)
	}

	if err := rt1.Disconnect(rt2); err != nil {
		t.Fatalf("rt1.Disconnect(rt2): %v", err)
	}
	waitForConnectionCounts(t, 10*time.Second, 0, rt1, rt2)
}
