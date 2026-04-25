package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
)

// testdummyConfig builds a Config that points bitcoind at a fresh data dir
// (auto-cleaned by t.TempDir) on the given port, with -acceptnonstdtxn=1 and
// the testdummy BIP9 deployment configured for fast activation. Shared by
// soft-fork tests so the activation parameters stay consistent.
func testdummyConfig(t *testing.T, port int) *Config {
	t.Helper()
	return &Config{
		Host:            fmt.Sprintf("127.0.0.1:%d", port),
		User:            "user",
		Pass:            "pass",
		DataDir:         filepath.Join(t.TempDir(), "regtest"),
		AcceptNonstdTxn: true,
		VBParams: []VBParam{{
			Deployment:          "testdummy",
			StartTime:           0,
			Timeout:             9999999999,
			MinActivationHeight: 0,
		}},
	}
}

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
		Host:            "127.0.0.1:18444",
		User:            "testuser",
		Pass:            "testpass",
		DataDir:         "/tmp/test_regtest",
		ExtraArgs:       []string{"-txindex=1"},
		VBParams:        []VBParam{VBAlwaysActive("testdummy")},
		AcceptNonstdTxn: true,
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
	if len(cfg2.VBParams) != 1 || cfg2.VBParams[0].Deployment != "testdummy" {
		t.Errorf("expected VBParams [testdummy], got %v", cfg2.VBParams)
	}
	if !cfg2.AcceptNonstdTxn {
		t.Error("expected AcceptNonstdTxn=true to round-trip")
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
		{"DeploymentStatus", func() error { _, err := rt.DeploymentStatus("taproot"); return err }},
		{"TestMempoolAccept", func() error {
			tx := wire.NewMsgTx(2)
			tx.AddTxIn(&wire.TxIn{})
			tx.AddTxOut(wire.NewTxOut(0, []byte{0x6a}))
			_, err := rt.TestMempoolAccept(tx)
			return err
		}},
		{"InvalidateBlock", func() error { return rt.InvalidateBlock(&chainhash.Hash{}) }},
		{"ReconsiderBlock", func() error { return rt.ReconsiderBlock(&chainhash.Hash{}) }},
		{"PreciousBlock", func() error { return rt.PreciousBlock(&chainhash.Hash{}) }},
		{"MineToHeight", func() error {
			return rt.MineToHeight(1, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
		}},
		{"MineUntilActive", func() error {
			_, err := rt.MineUntilActive("testdummy", "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl", 100)
			return err
		}},
		{"GetBlockTemplate", func() error {
			_, err := rt.GetBlockTemplate(&btcjson.TemplateRequest{Mode: "template", Rules: []string{"segwit"}})
			return err
		}},
		{"SubmitBlock", func() error { return rt.SubmitBlock(&wire.MsgBlock{}) }},
		{"CreateRawTransaction", func() error {
			_, err := rt.CreateRawTransaction(nil, nil, nil)
			return err
		}},
		{"DecodeRawTransaction", func() error {
			_, err := rt.DecodeRawTransaction(wire.NewMsgTx(2))
			return err
		}},
		{"DecodeScript", func() error { _, err := rt.DecodeScript("00"); return err }},
		{"FundRawTransaction", func() error {
			_, err := rt.FundRawTransaction(wire.NewMsgTx(2), nil)
			return err
		}},
		{"Connect", func() error { return rt.Connect(&Regtest{config: DefaultConfig()}) }},
		{"Disconnect", func() error { return rt.Disconnect(&Regtest{config: DefaultConfig()}) }},
		{"AddNode", func() error { return rt.AddNode("127.0.0.1:18444") }},
		{"GetConnectionCount", func() error { _, err := rt.GetConnectionCount(); return err }},
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

// Test_SoftForkStatus_String verifies that SoftForkStatus.String() returns
// the BIP9 status strings bitcoind uses, and that parseSoftForkStatus is its
// inverse — pinning the round-trip contract for the typed enum.
func Test_SoftForkStatus_String(t *testing.T) {
	cases := []struct {
		status SoftForkStatus
		s      string
	}{
		{SoftForkDefined, "defined"},
		{SoftForkStarted, "started"},
		{SoftForkLockedIn, "locked_in"},
		{SoftForkActive, "active"},
		{SoftForkFailed, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			if got := tc.status.String(); got != tc.s {
				t.Errorf("String() = %q, want %q", got, tc.s)
			}
			if got := parseSoftForkStatus(tc.s); got != tc.status {
				t.Errorf("parseSoftForkStatus(%q) = %v, want %v", tc.s, got, tc.status)
			}
		})
	}
	if got := SoftForkUnknown.String(); got != "unknown" {
		t.Errorf("SoftForkUnknown.String() = %q, want unknown", got)
	}
	if got := parseSoftForkStatus("garbage"); got != SoftForkUnknown {
		t.Errorf("parseSoftForkStatus(garbage) = %v, want SoftForkUnknown", got)
	}
}

// Test_TestdummyConfig pins the shape of the shared testdummyConfig helper
// so future soft-fork tests (#71, #81) can rely on it. No node spawned.
func Test_TestdummyConfig(t *testing.T) {
	cfg := testdummyConfig(t, 19702)
	if cfg.Host != "127.0.0.1:19702" {
		t.Errorf("Host = %q, want 127.0.0.1:19702", cfg.Host)
	}
	if !cfg.AcceptNonstdTxn {
		t.Error("expected AcceptNonstdTxn=true")
	}
	if len(cfg.VBParams) != 1 {
		t.Fatalf("expected 1 VBParam, got %d", len(cfg.VBParams))
	}
	vb := cfg.VBParams[0]
	if vb.Deployment != "testdummy" {
		t.Errorf("Deployment = %q, want testdummy", vb.Deployment)
	}
	if vb.StartTime != 0 || vb.Timeout != 9999999999 || vb.MinActivationHeight != 0 {
		t.Errorf("VBParam = %+v, want {testdummy 0 9999999999 0}", vb)
	}
}

// Test_VBParams_Render unit-tests Config.renderExtraArgs (no node spawned).
// Pins the wire format for -vbparams and the composition order:
// ExtraArgs first, then VBParams in declaration order, then -acceptnonstdtxn.
func Test_VBParams_Render(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "empty",
			cfg:  Config{},
			want: nil,
		},
		{
			name: "extra-args-only",
			cfg:  Config{ExtraArgs: []string{"-debug=net"}},
			want: []string{"-debug=net"},
		},
		{
			name: "vbparams-explicit",
			cfg: Config{
				VBParams: []VBParam{
					{Deployment: "testdummy", StartTime: 0, Timeout: 9999999999, MinActivationHeight: 0},
				},
			},
			want: []string{"-vbparams=testdummy:0:9999999999:0"},
		},
		{
			name: "vbparams-helpers",
			cfg: Config{
				VBParams: []VBParam{
					VBAlwaysActive("anyprevout"),
					VBNeverActive("checktemplateverify"),
				},
			},
			want: []string{
				"-vbparams=anyprevout:-1:0:0",
				"-vbparams=checktemplateverify:-2:0:0",
			},
		},
		{
			name: "all-three-combine-in-order",
			cfg: Config{
				ExtraArgs: []string{"-debug=net", "-printtoconsole=0"},
				VBParams: []VBParam{
					{Deployment: "testdummy", StartTime: 0, Timeout: 9999999999, MinActivationHeight: 0},
				},
				AcceptNonstdTxn: true,
			},
			want: []string{
				"-debug=net",
				"-printtoconsole=0",
				"-vbparams=testdummy:0:9999999999:0",
				"-acceptnonstdtxn=1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.renderExtraArgs()
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Test_New_EmptyVBParamDeployment pins the validation contract that an empty
// Deployment field is rejected at New time rather than silently producing a
// malformed -vbparams= flag.
func Test_New_EmptyVBParamDeployment(t *testing.T) {
	_, err := New(&Config{
		VBParams: []VBParam{{Deployment: "", StartTime: 0, Timeout: 0, MinActivationHeight: 0}},
	})
	if err == nil {
		t.Fatal("expected error from empty Deployment, got nil")
	}
}

// Test_AcceptNonstdTxn verifies that Config.AcceptNonstdTxn maps to
// -acceptnonstdtxn=1 and actually changes mempool policy. Combined with
// -datacarrier=0 (which marks any OP_RETURN output as non-standard
// regardless of payload size — robust across Core versions that have
// relaxed OP_RETURN size limits), a tx with an OP_RETURN output should be
// rejected by default but accepted when AcceptNonstdTxn is on.
func Test_AcceptNonstdTxn(t *testing.T) {
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

// Test_ExtractP2PPort pins the host-parsing contract: the P2P port is RPC+1
// per scripts/bitcoind_manager.sh, and unparseable hosts return the empty
// string so callers can fall back cleanly.
func Test_ExtractP2PPort(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"127.0.0.1:18443", "18444"},
		{"127.0.0.1:20000", "20001"},
		{"localhost:8332", "8333"},
		{"127.0.0.1", ""},     // no port
		{"", ""},              // empty
		{"127.0.0.1:", ""},    // trailing colon
		{"127.0.0.1:abc", ""}, // non-numeric port
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := extractP2PPort(tc.host); got != tc.want {
				t.Errorf("extractP2PPort(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

// Test_MultiNode_Connect_Sync exercises the full multi-node story: start two
// regtest nodes, Connect rt1 -> rt2, observe GetConnectionCount go positive
// on both within a timeout, then Warp on rt1 and confirm rt2's height
// matches via header propagation. Closes the harness gap that blocked
// reorg-style soft-fork tests.
func Test_MultiNode_Connect_Sync(t *testing.T) {
	rt1, err := New(&Config{
		Host:    "127.0.0.1:20000",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "rt1"),
	})
	if err != nil {
		t.Fatalf("New rt1: %v", err)
	}
	t.Cleanup(func() { _ = rt1.Stop(); _ = rt1.Cleanup() })

	rt2, err := New(&Config{
		Host:    "127.0.0.1:20100",
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

	if err := rt1.Connect(rt2); err != nil {
		t.Fatalf("rt1.Connect(rt2): %v", err)
	}

	// addnode is asynchronous; poll until both sides see the link.
	deadline := time.Now().Add(15 * time.Second)
	for {
		n1, err := rt1.GetConnectionCount()
		if err != nil {
			t.Fatalf("rt1.GetConnectionCount: %v", err)
		}
		n2, err := rt2.GetConnectionCount()
		if err != nil {
			t.Fatalf("rt2.GetConnectionCount: %v", err)
		}
		if n1 >= 1 && n2 >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nodes never connected: n1=%d n2=%d", n1, n2)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := rt1.EnsureWallet("miner"); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt1.GenerateBech32("miner")
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt1.Warp(5, miner); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	want, err := rt1.GetBlockCount()
	if err != nil {
		t.Fatalf("rt1.GetBlockCount: %v", err)
	}

	deadline = time.Now().Add(15 * time.Second)
	for {
		got, err := rt2.GetBlockCount()
		if err != nil {
			t.Fatalf("rt2.GetBlockCount: %v", err)
		}
		if got == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rt2 never caught up: rt1=%d rt2=%d", want, got)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Disconnect should drop the link; verify both connection counts return to 0.
	if err := rt1.Disconnect(rt2); err != nil {
		t.Fatalf("rt1.Disconnect(rt2): %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		n1, err := rt1.GetConnectionCount()
		if err != nil {
			t.Fatalf("rt1.GetConnectionCount post-disconnect: %v", err)
		}
		n2, err := rt2.GetConnectionCount()
		if err != nil {
			t.Fatalf("rt2.GetConnectionCount post-disconnect: %v", err)
		}
		if n1 == 0 && n2 == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nodes never disconnected: n1=%d n2=%d", n1, n2)
		}
		time.Sleep(200 * time.Millisecond)
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
