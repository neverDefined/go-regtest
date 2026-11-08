package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
)

func TestRegtest(t *testing.T) {
	requireBitcoind(t)
	// Create new regtest instance with default config
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	// Start bitcoind
	err = rt.Start()
	if err != nil {
		t.Fatalf("failed to start bitcoind: %v", err)
	}

	// Connect via RPC using the instance's config
	rpcClient, err := rpcclient.New(rt.RPCConfig(), nil)
	if err != nil {
		t.Fatalf("failed to connect via rpc client: %v", err)
	}
	defer rpcClient.Shutdown()

	// Health check
	_, err = rpcClient.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count (health check), %v", err)
	}

	// Test stopping bitcoind
	err = rt.Stop()
	if err != nil {
		t.Fatalf("failed to stop bitcoind: %v", err)
	}

	// Test that it's actually stopped
	running, err := rt.IsRunning()
	if err != nil {
		t.Fatalf("failed to check if bitcoind is running: %v", err)
	}
	if running {
		t.Fatal("bitcoind should not be running after stop")
	}
}

func TestMultipleInstances(t *testing.T) {
	requireBitcoind(t)
	// Create first instance on default port
	// Uses RPC port 19000, P2P port 19001, and potentially other service ports
	rt1, err := New(&Config{
		Host:    "127.0.0.1:19000",
		User:    "user1",
		Pass:    "pass1",
		DataDir: "./bitcoind_regtest_1",
	})
	if err != nil {
		t.Fatalf("failed to create first regtest instance: %v", err)
	}

	// Create second instance on non-overlapping ports (spacing by 100 to avoid conflicts)
	// Uses RPC port 19100, P2P port 19101, and potentially other service ports
	rt2, err := New(&Config{
		Host:    "127.0.0.1:19100",
		User:    "user2",
		Pass:    "pass2",
		DataDir: "./bitcoind_regtest_2",
	})
	if err != nil {
		t.Fatalf("failed to create second regtest instance: %v", err)
	}

	// Start first instance
	err = rt1.Start()
	if err != nil {
		t.Fatalf("failed to start first bitcoind: %v", err)
	}

	// Start second instance
	err = rt2.Start()
	if err != nil {
		rt1.Stop() // Clean up first instance
		t.Fatalf("failed to start second bitcoind: %v", err)
	}

	// Verify both are running
	running1, err := rt1.IsRunning()
	if err != nil {
		t.Errorf("failed to check first instance status: %v", err)
	}
	if !running1 {
		t.Error("first instance should be running")
	}

	running2, err := rt2.IsRunning()
	if err != nil {
		t.Errorf("failed to check second instance status: %v", err)
	}
	if !running2 {
		t.Error("second instance should be running")
	}

	// Connect to first instance via RPC
	client1, err := rpcclient.New(rt1.RPCConfig(), nil)
	if err != nil {
		t.Errorf("failed to connect to first instance: %v", err)
	} else {
		defer client1.Shutdown()
		_, err = client1.GetBlockCount()
		if err != nil {
			t.Errorf("failed to query first instance: %v", err)
		}
	}

	// Connect to second instance via RPC
	client2, err := rpcclient.New(rt2.RPCConfig(), nil)
	if err != nil {
		t.Errorf("failed to connect to second instance: %v", err)
	} else {
		defer client2.Shutdown()
		_, err = client2.GetBlockCount()
		if err != nil {
			t.Errorf("failed to query second instance: %v", err)
		}
	}

	// Verify configurations are independent
	if rt1.Config().Host == rt2.Config().Host {
		t.Error("instances should have different hosts")
	}
	if rt1.Config().DataDir == rt2.Config().DataDir {
		t.Error("instances should have different data directories")
	}

	// Stop both instances
	err = rt1.Stop()
	if err != nil {
		t.Errorf("failed to stop first bitcoind: %v", err)
	}

	err = rt2.Stop()
	if err != nil {
		t.Errorf("failed to stop second bitcoind: %v", err)
	}

	// Verify both are stopped
	running1, err = rt1.IsRunning()
	if err != nil {
		t.Errorf("failed to check first instance status after stop: %v", err)
	}
	if running1 {
		t.Error("first instance should not be running after stop")
	}

	running2, err = rt2.IsRunning()
	if err != nil {
		t.Errorf("failed to check second instance status after stop: %v", err)
	}
	if running2 {
		t.Error("second instance should not be running after stop")
	}
}

func TestCleanup(t *testing.T) {
	requireBitcoind(t)
	dataDir := filepath.Join(t.TempDir(), "regtest")
	rt, err := New(&Config{Host: "127.0.0.1:19800", DataDir: dataDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("data dir should exist while running: %v", err)
	}
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("Stop should remove the data dir, stat err = %v", err)
	}
	// Stop and Cleanup are safe to call again.
	if err := rt.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := rt.Cleanup(); err != nil {
			t.Errorf("Cleanup #%d: %v", i+1, err)
		}
	}
}

// TestStart_PortInUse checks that a second instance on an occupied RPC port
// fails fast with a clear error instead of talking to the other node.
func TestStart_PortInUse(t *testing.T) {
	requireBitcoind(t)
	dataDir := filepath.Join(t.TempDir(), "a")
	first, err := New(&Config{Host: "127.0.0.1:19810", DataDir: dataDir})
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	if err := first.Start(); err != nil {
		t.Fatalf("Start first: %v", err)
	}
	t.Cleanup(func() { _ = first.Stop() })

	// Same DataDir on purpose: the rejected instance must not touch it.
	second, err := New(&Config{Host: "127.0.0.1:19810", DataDir: dataDir})
	if err != nil {
		t.Fatalf("New second: %v", err)
	}
	err = second.Start()
	if err == nil {
		_ = second.Stop()
		t.Fatal("expected Start on an occupied port to fail")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Errorf("error should say the port is in use, got: %v", err)
	}
	if err := second.Stop(); err != nil {
		t.Errorf("Stop on the rejected instance: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "regtest")); err != nil {
		t.Errorf("first node's data must survive the other instance's Stop: %v", err)
	}
	if err := first.HealthCheck(); err != nil {
		t.Errorf("first node should be unaffected: %v", err)
	}
}

// TestStart_Twice checks that starting a running instance is an error.
func TestStart_Twice(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{Host: "127.0.0.1:19820", DataDir: filepath.Join(t.TempDir(), "regtest")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })
	if err := rt.Start(); err == nil {
		t.Fatal("second Start should fail while the node is running")
	}
	if err := rt.HealthCheck(); err != nil {
		t.Errorf("node should still be healthy: %v", err)
	}
}

// TestIsRunning_AfterCleanup pins the Phase 1.3 contract: IsRunning() must
// remain valid after Cleanup() because it queries the RPC port directly rather
// than depending on the embedded manager script.
func TestIsRunning_AfterCleanup(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:    "127.0.0.1:19200",
		User:    "user",
		Pass:    "pass",
		DataDir: "./bitcoind_regtest_isrunning",
	})
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	running, err := rt.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning errored while node up: %v", err)
	}
	if !running {
		t.Fatal("IsRunning returned false while node is up")
	}

	if err := rt.Stop(); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
	if err := rt.Cleanup(); err != nil {
		t.Fatalf("failed to cleanup: %v", err)
	}

	// Critical: must not panic or return script-related errors.
	running, err = rt.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning errored after Cleanup: %v", err)
	}
	if running {
		t.Error("IsRunning returned true after Stop+Cleanup")
	}
}

// TestStartContext_PreCancelled verifies that StartContext surfaces a
// pre-cancelled context's error rather than spawning bitcoind.
func TestStartContext_PreCancelled(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:    "127.0.0.1:19400",
		User:    "user",
		Pass:    "pass",
		DataDir: "./bitcoind_regtest_startcancel",
	})
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := rt.StartContext(ctx); err == nil {
		t.Fatal("expected error from cancelled StartContext, got nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx.Canceled in error chain, got %v", err)
	}
}

// TestRestartCycle exercises Start → Stop → Start. After the second Start
// the live client should once again work.
func TestRestartCycle(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:    "127.0.0.1:19500",
		User:    "user",
		Pass:    "pass",
		DataDir: "./bitcoind_regtest_restart",
	})
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	if err := rt.Start(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := rt.HealthCheck(); err != nil {
		t.Fatalf("health check after first start: %v", err)
	}

	if err := rt.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := rt.HealthCheck(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("expected ErrNotConnected after Stop, got %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := rt.HealthCheck(); err != nil {
		t.Fatalf("health check after restart: %v", err)
	}
}

// TestContext_Cancellation verifies that *Context variants surface context
// errors when the supplied context is already cancelled.
func TestContext_Cancellation(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:    "127.0.0.1:19300",
		User:    "user",
		Pass:    "pass",
		DataDir: "./bitcoind_regtest_ctxcancel",
	})
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	_, err = rt.GetBlockCountContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// A timeout-bound ctx should also propagate.
	tctx, tcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer tcancel()
	_, err = rt.GetBlockCountContext(tctx)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx error, got %v", err)
	}
}

// TestAcceptNonstdTxn verifies that Config.AcceptNonstdTxn maps to
// -acceptnonstdtxn=1 and actually changes mempool policy. Combined with
// -datacarrier=0 (which marks any OP_RETURN output as non-standard
// regardless of payload size — robust across Core versions that have
// relaxed OP_RETURN size limits), a tx with an OP_RETURN output should be
// rejected by default but accepted when AcceptNonstdTxn is on.
func TestAcceptNonstdTxn(t *testing.T) {
	requireBitcoind(t)
	tryBroadcast := func(t *testing.T, port int, accept bool) error {
		t.Helper()
		rt, err := New(&Config{
			Host:            fmt.Sprintf("127.0.0.1:%d", port),
			User:            "user",
			Pass:            "pass",
			DataDir:         filepath.Join(t.TempDir(), "regtest"),
			ExtraArgs:       []string{"-datacarrier=0"},
			AcceptNonstdTxn: accept,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := rt.Start(); err != nil {
			t.Fatalf("Start (port %d, accept=%v): %v", port, accept, err)
		}
		t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := rt.EnsureWallet("test"); err != nil {
			t.Fatalf("EnsureWallet: %v", err)
		}
		addr, err := rt.GenerateBech32("test")
		if err != nil {
			t.Fatalf("GenerateBech32: %v", err)
		}
		if err := rt.Warp(101, addr); err != nil {
			t.Fatalf("Warp: %v", err)
		}

		// Small OP_RETURN payload — combined with -datacarrier=0 the tx is
		// non-standard regardless of size.
		data := strings.Repeat("aa", 8)

		// Create a skeleton tx with just the OP_RETURN; let the wallet fund it.
		skelRaw, err := rt.rawRPC(ctx, "createrawtransaction",
			[]any{}, // no manual inputs
			map[string]any{"data": data},
		)
		if err != nil {
			t.Fatalf("createrawtransaction: %v", err)
		}
		var skelHex string
		if err := json.Unmarshal(skelRaw, &skelHex); err != nil {
			t.Fatalf("unmarshal skeleton: %v", err)
		}

		fundedRaw, err := rt.rawRPC(ctx, "fundrawtransaction", skelHex)
		if err != nil {
			t.Fatalf("fundrawtransaction: %v", err)
		}
		var funded struct {
			Hex string `json:"hex"`
		}
		if err := json.Unmarshal(fundedRaw, &funded); err != nil {
			t.Fatalf("unmarshal funded: %v", err)
		}

		signedRaw, err := rt.rawRPC(ctx, "signrawtransactionwithwallet", funded.Hex)
		if err != nil {
			t.Fatalf("signrawtransactionwithwallet: %v", err)
		}
		var signed struct {
			Hex      string `json:"hex"`
			Complete bool   `json:"complete"`
		}
		if err := json.Unmarshal(signedRaw, &signed); err != nil {
			t.Fatalf("unmarshal signed: %v", err)
		}
		if !signed.Complete {
			t.Fatal("sign incomplete")
		}

		_, err = rt.rawRPC(ctx, "sendrawtransaction", signed.Hex)
		return err
	}

	// Spacing of 10 between RPC ports avoids collision with the P2P port
	// (RPC+1) of the prior instance, which is still alive via t.Cleanup.
	if err := tryBroadcast(t, 19700, true); err != nil {
		t.Errorf("AcceptNonstdTxn=true should accept large OP_RETURN, got: %v", err)
	}
	if err := tryBroadcast(t, 19710, false); err == nil {
		t.Error("AcceptNonstdTxn=false should reject large OP_RETURN, got nil error")
	}
}

// TestExtraArgs_Forwarded verifies that Config.ExtraArgs are passed through
// to bitcoind. Sets -debug=mempool and asserts the mempool category is
// enabled via the `logging` RPC.
func TestExtraArgs_Forwarded(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:      "127.0.0.1:19600",
		User:      "user",
		Pass:      "pass",
		DataDir:   "./bitcoind_regtest_extraargs",
		ExtraArgs: []string{"-debug=mempool"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := rt.rawRPC(ctx, "logging")
	if err != nil {
		t.Fatalf("logging RPC: %v", err)
	}
	var categories map[string]bool
	if err := json.Unmarshal(raw, &categories); err != nil {
		t.Fatalf("unmarshal logging response: %v: %s", err, raw)
	}
	if !categories["mempool"] {
		t.Errorf("expected mempool=true with -debug=mempool, got: %v", categories)
	}
}

// TestExtraArgs_UnknownFlag verifies that an invalid bitcoind flag passed
// via Config.ExtraArgs surfaces as a Start error rather than silently
// succeeding. Pins the contract that ExtraArgs are actually forwarded.
func TestExtraArgs_UnknownFlag(t *testing.T) {
	requireBitcoind(t)
	rt, err := New(&Config{
		Host:      "127.0.0.1:19601",
		User:      "user",
		Pass:      "pass",
		DataDir:   "./bitcoind_regtest_extraargs_bad",
		ExtraArgs: []string{"-this-flag-does-not-exist=1"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	err = rt.Start()
	if err == nil {
		t.Fatal("expected Start to fail for unknown flag, got nil")
	}
	if !strings.Contains(err.Error(), "this-flag-does-not-exist") {
		t.Errorf("error should include bitcoind's output naming the flag, got: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error should wrap bitcoind's *exec.ExitError, got: %v", err)
	}
}

// TestConfig_BinaryPath_Resolved confirms that an explicit Config.BinaryPath
// takes precedence over the auto-detect chain and that the node starts and
// stops cleanly under it. Pins the contract that resolveBinary is actually
// honoured by the script invocation.
func TestConfig_BinaryPath_Resolved(t *testing.T) {
	bitcoindPath, err := exec.LookPath("bitcoind")
	if err != nil {
		t.Skipf("bitcoind not in PATH: %v", err)
	}

	rt, err := New(&Config{
		Host:       "127.0.0.1:19710",
		User:       "user",
		Pass:       "pass",
		DataDir:    filepath.Join(t.TempDir(), "regtest"),
		BinaryPath: bitcoindPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(); _ = rt.Cleanup() })

	if rt.bitcoindPath != bitcoindPath {
		t.Errorf("rt.bitcoindPath = %q, want %q", rt.bitcoindPath, bitcoindPath)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	running, err := rt.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Fatal("node should be running after Start")
	}
}
