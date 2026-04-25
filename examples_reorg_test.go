package regtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestExampleReorg is a narrated end-to-end example of resolving a fork
// across two regtest nodes. The script:
//
//  1. Two nodes are started on widely-spaced ports.
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
	// 1) Two nodes on isolated data dirs and ports. Use widely-spaced ports
	//    so the P2P-port-derivation (P2P = RPC + 1) doesn't collide with
	//    other tests' RPC ports.
	rt1, err := New(&Config{
		Host:    "127.0.0.1:20200",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "rt1"),
	})
	if err != nil {
		t.Fatalf("New rt1: %v", err)
	}
	t.Cleanup(func() { _ = rt1.Stop(); _ = rt1.Cleanup() })

	rt2, err := New(&Config{
		Host:    "127.0.0.1:20300",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "rt2"),
	})
	if err != nil {
		t.Fatalf("New rt2: %v", err)
	}
	t.Cleanup(func() { _ = rt2.Stop(); _ = rt2.Cleanup() })

	if err := rt1.Start(); err != nil {
		t.Fatalf("Start rt1: %v", err)
	}
	if err := rt2.Start(); err != nil {
		t.Fatalf("Start rt2: %v", err)
	}

	// Each node mines to its own wallet — the wallet on the losing side
	// will see its coinbases marked "conflicted" after the reorg, which is
	// exactly the production-style behaviour we want to demonstrate.
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 2) Connect and sync. addnode is asynchronous, so poll until both
	//    sides report at least one connection.
	if err := rt1.ConnectContext(ctx, rt2); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	waitForConnections(t, ctx, rt1, rt2, 1, "initial connect")

	// Mine a shared prefix on rt1. With the link up rt2 should follow.
	const sharedHeight = int64(5)
	if err := rt1.WarpContext(ctx, sharedHeight, miner1); err != nil {
		t.Fatalf("Warp rt1 prefix: %v", err)
	}
	waitForHeight(t, ctx, rt2, sharedHeight, "rt2 follows shared prefix")

	prefixTip, err := rt1.GetBestBlockHashContext(ctx)
	if err != nil {
		t.Fatalf("rt1.GetBestBlockHash: %v", err)
	}
	t.Logf("shared prefix established at height %d, tip=%s", sharedHeight, prefixTip)

	// 3) Partition the network. After Disconnect, the two nodes mine in
	//    isolation.
	if err := rt1.DisconnectContext(ctx, rt2); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	waitForConnections(t, ctx, rt1, rt2, 0, "post-disconnect")

	// 4) Mine divergent chains. rt2 builds a longer chain (10 blocks beyond
	//    the prefix) than rt1 (3 blocks), so by Bitcoin's longest-chain
	//    rule rt2's chain wins on reconnect.
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

	// 5) Heal the partition. Re-Connect; bitcoind's header sync resolves the
	//    fork.
	if err := rt1.ConnectContext(ctx, rt2); err != nil {
		t.Fatalf("re-Connect: %v", err)
	}
	waitForConnections(t, ctx, rt1, rt2, 1, "post-reconnect")

	// 6) Both nodes converge on the longer chain (rt2's). Wait for rt1 to
	//    adopt it, then assert tip equality.
	waitForHeight(t, ctx, rt1, rt2Height, "rt1 follows longer chain")

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

// waitForConnections polls both nodes until each reports the target
// connection count, or the context expires.
func waitForConnections(t *testing.T, ctx context.Context, rt1, rt2 *Regtest, want int64, label string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", label, ctx.Err())
		default:
		}
		n1, err := rt1.GetConnectionCountContext(ctx)
		if err != nil {
			t.Fatalf("%s: rt1.GetConnectionCount: %v", label, err)
		}
		n2, err := rt2.GetConnectionCountContext(ctx)
		if err != nil {
			t.Fatalf("%s: rt2.GetConnectionCount: %v", label, err)
		}
		if n1 == want && n2 == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitForHeight polls rt until it reaches the target block count, or the
// context expires.
func waitForHeight(t *testing.T, ctx context.Context, rt *Regtest, want int64, label string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", label, ctx.Err())
		default:
		}
		got, err := rt.GetBlockCountContext(ctx)
		if err != nil {
			t.Fatalf("%s: GetBlockCount: %v", label, err)
		}
		if got == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
