package regtest

import (
	"testing"

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

	t.Log("bitcoind management test passed")
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

	t.Log("configuration test passed")
}
