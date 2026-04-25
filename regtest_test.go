package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

func Test_Regtest(t *testing.T) {
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

func Test_Config(t *testing.T) {
	// Test default config
	defaultCfg := DefaultConfig()
	if defaultCfg.Host != "127.0.0.1:18443" {
		t.Errorf("expected default host 127.0.0.1:18443, got %s", defaultCfg.Host)
	}
	if defaultCfg.User != "user" {
		t.Errorf("expected default user 'user', got %s", defaultCfg.User)
	}
	if defaultCfg.Pass != "pass" {
		t.Errorf("expected default pass 'pass', got %s", defaultCfg.Pass)
	}

	// Test creating instance with nil config uses defaults
	rt1, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest with default config: %v", err)
	}
	cfg1 := rt1.Config()
	if cfg1.Host != defaultCfg.Host || cfg1.User != defaultCfg.User {
		t.Error("New(nil) should use default config")
	}

	// Test creating instance with custom config
	customCfg := &Config{
		Host:      "127.0.0.1:18444",
		User:      "testuser",
		Pass:      "testpass",
		DataDir:   "/tmp/test_regtest",
		ExtraArgs: []string{"-txindex=1"},
	}
	rt2, err := New(customCfg)
	if err != nil {
		t.Fatalf("failed to create regtest with custom config: %v", err)
	}

	// Verify custom config is used
	cfg2 := rt2.Config()
	if cfg2.Host != customCfg.Host {
		t.Errorf("expected host %s, got %s", customCfg.Host, cfg2.Host)
	}
	if cfg2.User != customCfg.User {
		t.Errorf("expected user %s, got %s", customCfg.User, cfg2.User)
	}
	if cfg2.Pass != customCfg.Pass {
		t.Errorf("expected pass %s, got %s", customCfg.Pass, cfg2.Pass)
	}
	if cfg2.DataDir != customCfg.DataDir {
		t.Errorf("expected datadir %s, got %s", customCfg.DataDir, cfg2.DataDir)
	}
	if len(cfg2.ExtraArgs) != 1 || cfg2.ExtraArgs[0] != "-txindex=1" {
		t.Errorf("expected extra args [-txindex=1], got %v", cfg2.ExtraArgs)
	}

	// Test that RPCConfig uses the instance's config
	rpcCfg := rt2.RPCConfig()
	if rpcCfg.Host != customCfg.Host {
		t.Errorf("RPCConfig should use instance's host, got %s", rpcCfg.Host)
	}
	if rpcCfg.User != customCfg.User {
		t.Errorf("RPCConfig should use instance's user, got %s", rpcCfg.User)
	}

	// Test immutability - modifying returned config shouldn't affect stored config
	cfg := rt2.Config()
	cfg.Host = "modified"
	cfg2Again := rt2.Config()
	if cfg2Again.Host == "modified" {
		t.Error("Config() should return a copy, not the original config")
	}

	// Test that each instance is independent
	if rt1.Config().Host == rt2.Config().Host {
		t.Error("different instances should have independent configs")
	}
}

func Test_MultipleInstances(t *testing.T) {
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

func Test_Cleanup(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	err = rt.Start()
	if err != nil {
		t.Fatalf("failed to start bitcoind: %v", err)
	}

	tempDir := rt.scriptTmpDir
	if tempDir == "" {
		t.Fatal("scriptTmpDir should not be empty after start")
	}

	// Stop the node
	err = rt.Stop()
	if err != nil {
		t.Fatalf("failed to stop bitcoind: %v", err)
	}

	// Verify temp directory still exists after Stop()
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Error("temp directory should still exist after Stop()")
	}

	err = rt.Cleanup()
	if err != nil {
		t.Fatalf("failed to cleanup: %v", err)
	}

	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Error("temp directory should be removed after Cleanup()")
	}

	// Verify scriptTmpDir is cleared
	if rt.scriptTmpDir != "" {
		t.Error("scriptTmpDir should be cleared after Cleanup()")
	}

	// Calling Cleanup() again should be safe
	err = rt.Cleanup()
	if err != nil {
		t.Errorf("calling Cleanup() again should not error: %v", err)
	}
}

// Test_IsRunning_AfterCleanup pins the Phase 1.3 contract: IsRunning() must
// remain valid after Cleanup() because it queries the RPC port directly rather
// than depending on the embedded manager script.
func Test_IsRunning_AfterCleanup(t *testing.T) {
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

// Test_RPCMethods_BeforeStart pins the contract that all RPC-issuing methods
// return errNotConnected when called before Start() (or after Stop() has
// cleared the client). No bitcoind required.
func Test_RPCMethods_BeforeStart(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	checks := []struct {
		name string
		call func() error
	}{
		{"GetBlockCount", func() error { _, err := rt.GetBlockCount(); return err }},
		{"HealthCheck", func() error { return rt.HealthCheck() }},
		{"GetWalletInformation", func() error { _, err := rt.GetWalletInformation(); return err }},
		{"CreateWallet", func() error { _, err := rt.CreateWallet("w"); return err }},
		{"LoadWallet", func() error { _, err := rt.LoadWallet("w"); return err }},
		{"UnloadWallet", func() error { return rt.UnloadWallet("w") }},
		{"GenerateBech32", func() error { _, err := rt.GenerateBech32("l"); return err }},
		{"GenerateBech32m", func() error { _, err := rt.GenerateBech32m("l"); return err }},
		{"ScanTxOutSetForAddress", func() error {
			_, err := rt.ScanTxOutSetForAddress("bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
			return err
		}},
		{"Warp", func() error {
			return rt.Warp(1, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
		}},
		{"GetBlockChainInfo", func() error { _, err := rt.GetBlockChainInfo(); return err }},
		{"GetBestBlockHash", func() error { _, err := rt.GetBestBlockHash(); return err }},
		{"GetBlockHash", func() error { _, err := rt.GetBlockHash(0); return err }},
		{"GetBlock", func() error { _, err := rt.GetBlock(&chainhash.Hash{}); return err }},
		{"GetBlockVerbose", func() error { _, err := rt.GetBlockVerbose(&chainhash.Hash{}); return err }},
		{"GetBlockHeader", func() error { _, err := rt.GetBlockHeader(&chainhash.Hash{}); return err }},
		{"GetChainTips", func() error { _, err := rt.GetChainTips(); return err }},
		{"GetDeploymentInfo", func() error { _, err := rt.GetDeploymentInfo(); return err }},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); !errors.Is(err, errNotConnected) {
				t.Errorf("expected errNotConnected, got %v", err)
			}
		})
	}
}

// Test_StartContext_PreCancelled verifies that StartContext surfaces a
// pre-cancelled context's error rather than spawning bitcoind.
func Test_StartContext_PreCancelled(t *testing.T) {
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

// Test_RestartCycle exercises Start → Stop → Start. After the second Start
// the live client should once again work.
func Test_RestartCycle(t *testing.T) {
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
	if err := rt.HealthCheck(); !errors.Is(err, errNotConnected) {
		t.Errorf("expected errNotConnected after Stop, got %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := rt.HealthCheck(); err != nil {
		t.Fatalf("health check after restart: %v", err)
	}
}

// Test_Context_Cancellation verifies that *Context variants surface context
// errors when the supplied context is already cancelled.
func Test_Context_Cancellation(t *testing.T) {
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

// Test_ExtraArgs_Forwarded verifies that Config.ExtraArgs are passed through
// to bitcoind. Sets -debug=mempool and asserts the mempool category is
// enabled via the `logging` RPC.
func Test_ExtraArgs_Forwarded(t *testing.T) {
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

// Test_ExtraArgs_UnknownFlag verifies that an invalid bitcoind flag passed
// via Config.ExtraArgs surfaces as a Start error rather than silently
// succeeding. Pins the contract that ExtraArgs are actually forwarded.
func Test_ExtraArgs_UnknownFlag(t *testing.T) {
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

	if err := rt.Start(); err == nil {
		t.Fatal("expected Start to fail for unknown flag, got nil")
	}
}
