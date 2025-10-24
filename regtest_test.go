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
