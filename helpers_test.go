package regtest

import (
	"context"
	"errors"
	"fmt"
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

// wantArgErr fails the test unless err is an argument error: non-nil and not
// ErrNotConnected. Argument checks run before any RPC, so on an unstarted
// node ErrNotConnected means the check didn't fire.
func wantArgErr(t *testing.T, call string, err error) {
	t.Helper()
	if err == nil || errors.Is(err, ErrNotConnected) {
		t.Errorf("%s: want an argument error before any RPC, got %v", call, err)
	}
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

// waitForConnectionCounts polls until every rt reports exactly want
// connections, failing the test after timeout.
func waitForConnectionCounts(t *testing.T, timeout time.Duration, want int64, rts ...*Regtest) {
	t.Helper()
	waitForConnections(t, timeout, fmt.Sprintf("exactly %d", want),
		func(n int64) bool { return n == want }, rts...)
}

// waitForMinConnections polls until every rt reports at least min
// connections, failing the test after timeout.
func waitForMinConnections(t *testing.T, timeout time.Duration, min int64, rts ...*Regtest) {
	t.Helper()
	waitForConnections(t, timeout, fmt.Sprintf("at least %d", min),
		func(n int64) bool { return n >= min }, rts...)
}

// waitForConnections polls every rt's connection count until each satisfies
// ok. Each GetConnectionCountContext call carries the timeout's deadline, so
// a stalled RPC can't run past it. An RPC error fails the test at once; only
// a count that doesn't satisfy ok yet is retried.
func waitForConnections(t *testing.T, timeout time.Duration, want string, ok func(int64) bool, rts ...*Regtest) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	counts := make([]int64, len(rts))
	for {
		done := true
		for i, rt := range rts {
			n, err := rt.GetConnectionCountContext(ctx)
			if err != nil {
				if ctx.Err() != nil {
					t.Fatalf("timed out after %v waiting for %s connections (last counts %v): %v",
						timeout, want, counts, err)
				}
				t.Fatalf("node %d: %v", i, err)
			}
			counts[i] = n
			done = done && ok(n)
		}
		if done {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out after %v waiting for %s connections, got %v", timeout, want, counts)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
