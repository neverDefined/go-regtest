package regtest

import (
	"context"
	"testing"
	"time"
)

// TestExampleReorg is a narrated end-to-end example of resolving a fork
// across two regtest nodes. The script:
//
//  1. Two nodes are started on independent ports via StartT.
//  2. They Connect and synchronise on a shared height.
//  3. Disconnect partitions the network.
//  4. Each side mines a divergent chain — a real fork.
//  5. Reconnect heals the partition.
//  6. Bitcoin's longest-chain rule resolves the fork: both nodes converge
//     on the longer chain, and the shorter chain's coinbases become orphans
//     in the loser's wallet.
//
// The acceptance shape — same best-block-hash on both nodes — is the same
// pin a soft-fork test would use to confirm that the network agrees on the
// activated rule set after a partition.
func TestExampleReorg(t *testing.T) {
	t.Parallel()
	rt1, rt2 := StartT(t, nil), StartT(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := rt1.ConnectContext(ctx, rt2); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	waitForMinConnections(t, 15*time.Second, 1, rt1, rt2)

	if err := rt1.EnsureWallet("miner1"); err != nil {
		t.Fatalf("EnsureWallet rt1: %v", err)
	}
	if err := rt2.EnsureWallet("miner2"); err != nil {
		t.Fatalf("EnsureWallet rt2: %v", err)
	}
	miner1, err := rt1.GenerateBech32("miner1")
	if err != nil {
		t.Fatalf("GenerateBech32 rt1: %v", err)
	}
	miner2, err := rt2.GenerateBech32("miner2")
	if err != nil {
		t.Fatalf("GenerateBech32 rt2: %v", err)
	}

	const sharedHeight = int64(5)
	if err := rt1.WarpContext(ctx, sharedHeight, miner1); err != nil {
		t.Fatalf("Warp rt1 prefix: %v", err)
	}
	if err := SyncBlocksContext(ctx, rt1, rt2); err != nil {
		t.Fatalf("rt2 follows shared prefix: %v", err)
	}

	prefixTip, err := rt1.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt1.GetBestBlockHash: %v", err)
	}
	t.Logf("shared prefix established at height %d, tip=%s", sharedHeight, prefixTip)

	if err := rt1.DisconnectContext(ctx, rt2); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	waitForConnectionCounts(t, 15*time.Second, 0, rt1, rt2)

	const rt1Extra = int64(3)
	const rt2Extra = int64(10)
	if err := rt1.WarpContext(ctx, rt1Extra, miner1); err != nil {
		t.Fatalf("Warp rt1 fork: %v", err)
	}
	if err := rt2.WarpContext(ctx, rt2Extra, miner2); err != nil {
		t.Fatalf("Warp rt2 fork: %v", err)
	}

	rt1Height, err := rt1.GetBlockCountContext(ctx)
	if err != nil {
		t.Fatalf("rt1.GetBlockCount: %v", err)
	}
	rt2Height, err := rt2.GetBlockCountContext(ctx)
	if err != nil {
		t.Fatalf("rt2.GetBlockCount: %v", err)
	}
	if rt1Height != sharedHeight+rt1Extra {
		t.Fatalf("rt1 height = %d, want %d", rt1Height, sharedHeight+rt1Extra)
	}
	if rt2Height != sharedHeight+rt2Extra {
		t.Fatalf("rt2 height = %d, want %d", rt2Height, sharedHeight+rt2Extra)
	}

	rt1ForkTip, err := rt1.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt1.GetBestBlockHash fork: %v", err)
	}
	rt2ForkTip, err := rt2.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt2.GetBestBlockHash fork: %v", err)
	}
	if rt1ForkTip.IsEqual(rt2ForkTip) {
		t.Fatalf("forks share a tip %s — partition didn't take", rt1ForkTip)
	}
	t.Logf("partitioned: rt1=%d (%s), rt2=%d (%s)", rt1Height, rt1ForkTip, rt2Height, rt2ForkTip)

	if err := rt1.ConnectContext(ctx, rt2); err != nil {
		t.Fatalf("re-Connect: %v", err)
	}
	waitForMinConnections(t, 15*time.Second, 1, rt1, rt2)

	if err := SyncBlocksContext(ctx, rt2, rt1); err != nil {
		t.Fatalf("rt1 follows longer chain: %v", err)
	}

	rt1FinalTip, err := rt1.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt1.GetBestBlockHash final: %v", err)
	}
	rt2FinalTip, err := rt2.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt2.GetBestBlockHash final: %v", err)
	}
	if !rt1FinalTip.IsEqual(rt2FinalTip) {
		t.Fatalf("nodes did not converge: rt1=%s rt2=%s", rt1FinalTip, rt2FinalTip)
	}
	if !rt1FinalTip.IsEqual(rt2ForkTip) {
		t.Errorf("converged tip = %s, expected the longer chain (rt2 fork tip = %s)",
			rt1FinalTip, rt2ForkTip)
	}
	t.Logf("converged on tip %s at height %d (rt1's %d-block fork was reorged out)",
		rt1FinalTip, rt2Height, rt1Extra)
}
