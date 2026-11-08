package regtest

import (
	"os"
	"testing"
	"time"
)

const (
	minerWallet = "miner"
	userWallet  = "user"
)

// unstarted returns a Regtest that is never started, for argument-validation
// and not-connected tests. BinaryPath points at the test binary itself so New
// succeeds without bitcoind installed; it's never executed.
func unstarted(t *testing.T) *Regtest {
	t.Helper()
	rt, err := New(&Config{Host: "127.0.0.1:1", BinaryPath: os.Args[0]})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt
}

func fundedWallet(t *testing.T, wallet string) (*Regtest, string) {
	t.Helper()
	rt := StartT(t, nil)
	if err := rt.EnsureWallet(wallet); err != nil {
		t.Fatal(err)
	}
	addr, err := rt.GenerateBech32(wallet)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Warp(101, addr); err != nil {
		t.Fatal(err)
	}
	return rt, addr
}

func fundedNode(t *testing.T) (*Regtest, string) {
	return fundedWallet(t, minerWallet)
}

// waitFor polls cond until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("waitFor: timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForConnectionCounts polls until every rt reports count.
func waitForConnectionCounts(t *testing.T, timeout time.Duration, want int64, rts ...*Regtest) {
	t.Helper()
	waitFor(t, timeout, func() bool {
		for _, rt := range rts {
			n, err := rt.GetConnectionCount()
			if err != nil || n != want {
				return false
			}
		}
		return true
	})
}

// waitForMinConnections polls until each rt has at least min connections.
func waitForMinConnections(t *testing.T, timeout time.Duration, min int64, rts ...*Regtest) {
	t.Helper()
	waitFor(t, timeout, func() bool {
		for _, rt := range rts {
			n, err := rt.GetConnectionCount()
			if err != nil || n < min {
				return false
			}
		}
		return true
	})
}
