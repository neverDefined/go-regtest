package regtest

import (
	"testing"

	"github.com/btcsuite/btcd/rpcclient"
)

func Test_Regtest(t *testing.T) {
	err := StartBitcoinRegtest()
	if err != nil {
		t.Fatalf("failed to start bitcoind: %v", err)
	}

	rpcClient, err := rpcclient.New(DefaultRegtestConfig(), nil)
	if err != nil {
		t.Fatalf("failed to connect via rpc client: %v", err)
	}
	defer rpcClient.Shutdown()

	_, err = rpcClient.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count (health check), %v", err)
	}

	// Test stopping bitcoind
	err = StopBitcoinRegtest()
	if err != nil {
		t.Fatalf("failed to stop bitcoind: %v", err)
	}

	// Test that it's actually stopped
	running, err := IsBitcoindRunning()
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

	// Test GetConfig returns default when no custom config is set
	cfg := GetConfig()
	if cfg.Host != defaultCfg.Host || cfg.User != defaultCfg.User {
		t.Error("GetConfig should return default config when none is set")
	}

	// Test SetConfig
	customCfg := &Config{
		Host:      "127.0.0.1:18444",
		User:      "testuser",
		Pass:      "testpass",
		DataDir:   "/tmp/test_regtest",
		ExtraArgs: []string{"-txindex=1"},
	}
	SetConfig(customCfg)

	// Verify custom config is returned
	cfg = GetConfig()
	if cfg.Host != customCfg.Host {
		t.Errorf("expected host %s, got %s", customCfg.Host, cfg.Host)
	}
	if cfg.User != customCfg.User {
		t.Errorf("expected user %s, got %s", customCfg.User, cfg.User)
	}
	if cfg.Pass != customCfg.Pass {
		t.Errorf("expected pass %s, got %s", customCfg.Pass, cfg.Pass)
	}
	if cfg.DataDir != customCfg.DataDir {
		t.Errorf("expected datadir %s, got %s", customCfg.DataDir, cfg.DataDir)
	}
	if len(cfg.ExtraArgs) != 1 || cfg.ExtraArgs[0] != "-txindex=1" {
		t.Errorf("expected extra args [-txindex=1], got %v", cfg.ExtraArgs)
	}

	// Test that DefaultRegtestConfig uses custom config
	rpcCfg := DefaultRegtestConfig()
	if rpcCfg.Host != customCfg.Host {
		t.Errorf("DefaultRegtestConfig should use custom host, got %s", rpcCfg.Host)
	}
	if rpcCfg.User != customCfg.User {
		t.Errorf("DefaultRegtestConfig should use custom user, got %s", rpcCfg.User)
	}

	// Test ResetConfig
	ResetConfig()
	cfg = GetConfig()
	if cfg.Host != defaultCfg.Host {
		t.Error("ResetConfig should restore default config")
	}

	// Test immutability - modifying returned config shouldn't affect stored config
	cfg = GetConfig()
	cfg.Host = "modified"
	cfg2 := GetConfig()
	if cfg2.Host == "modified" {
		t.Error("GetConfig should return a copy, not the original config")
	}

	t.Log("configuration test passed")
}
