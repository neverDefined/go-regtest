package regtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
)

func fakeRPC(t *testing.T, reply func(method, params string) (result, rpcErr string)) *Regtest {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var params bytes.Buffer
		_ = json.Compact(&params, in.Params)
		result, rpcErr := reply(in.Method, params.String())
		fmt.Fprintf(w, `{"result":%s,"error":%s,"id":%s}`, result, rpcErr, in.ID)
	}))
	t.Cleanup(srv.Close)
	client, err := rpcclient.New(&rpcclient.ConnConfig{
		Host:         srv.Listener.Addr().String(),
		User:         "user",
		Pass:         "pass",
		HTTPPostMode: true,
		DisableTLS:   true,
	}, nil)
	if err != nil {
		t.Fatalf("rpcclient.New: %v", err)
	}
	t.Cleanup(client.Shutdown)
	return &Regtest{client: client}
}

// Test_Call_FakeRPC pins call/callHash decoding and error wrapping, and the
// RPCs whose null result is meaningful (submitblock, gettxout).

func TestConfig(t *testing.T) {
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
	bitcoindPath, err := exec.LookPath("bitcoind")
	if err != nil {
		t.Fatalf("bitcoind not found in PATH: %v", err)
	}
	customCfg := &Config{
		Host:            "127.0.0.1:18444",
		User:            "testuser",
		Pass:            "testpass",
		DataDir:         "/tmp/test_regtest",
		ExtraArgs:       []string{"-txindex=1"},
		VBParams:        []VBParam{VBAlwaysActive("testdummy")},
		AcceptNonstdTxn: true,
		BinaryPath:      bitcoindPath,
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
	if cfg2.BinaryPath != bitcoindPath {
		t.Errorf("expected BinaryPath %s to round-trip, got %s", bitcoindPath, cfg2.BinaryPath)
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

func TestParseRPCPort(t *testing.T) {
	cases := []struct {
		host    string
		want    int
		wantErr bool
	}{
		{"127.0.0.1:18443", 18443, false},
		{"localhost:8332", 8332, false},
		{"[::1]:20000", 20000, false},
		{"127.0.0.1", 0, true},       // no port
		{"", 0, true},                // empty
		{"127.0.0.1:", 0, true},      // empty port
		{"127.0.0.1:abc", 0, true},   // non-numeric
		{"127.0.0.1:65535", 0, true}, // no room for the P2P port
		{"127.0.0.1:0", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			got, err := parseRPCPort(tc.host)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseRPCPort(%q) err = %v, wantErr %v", tc.host, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("parseRPCPort(%q) = %d, want %d", tc.host, got, tc.want)
			}
		})
	}
}

// Test_PeerAddress checks the P2P address is the RPC host at RPC port + 1.

func TestPeerAddress(t *testing.T) {
	rt, err := New(&Config{Host: "127.0.0.1:20000"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := peerAddress(rt)
	if err != nil {
		t.Fatalf("peerAddress: %v", err)
	}
	if got != "127.0.0.1:20001" {
		t.Errorf("peerAddress = %q, want 127.0.0.1:20001", got)
	}
	if _, err := peerAddress(nil); err == nil {
		t.Error("peerAddress(nil) should fail")
	}
}

// Test_New_InvalidHost checks that a Host without a usable port fails at New.

func TestNew_InvalidHost(t *testing.T) {
	if _, err := New(&Config{Host: "127.0.0.1"}); err == nil {
		t.Fatal("expected New to reject a Host without a port")
	}
}

// Test_New_PartialConfigDefaults checks that empty Config fields fall back to
// DefaultConfig values.

func TestNew_PartialConfigDefaults(t *testing.T) {
	rt, err := New(&Config{ExtraArgs: []string{"-debug=net"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, def := rt.Config(), DefaultConfig()
	if got.Host != def.Host || got.User != def.User || got.Pass != def.Pass || got.DataDir != def.DataDir {
		t.Errorf("empty fields should take defaults, got %+v", got)
	}
	if len(got.ExtraArgs) != 1 {
		t.Errorf("ExtraArgs should be kept, got %v", got.ExtraArgs)
	}
}

// Test_MultiNode_Connect_Sync exercises the full multi-node story: start two
// regtest nodes, Connect rt1 -> rt2, observe GetConnectionCount go positive
// on both within a timeout, then Warp on rt1 and confirm rt2's height
// matches via header propagation. Closes the harness gap that blocked
// reorg-style soft-fork tests.

func TestConfig_BinaryPath_Invalid(t *testing.T) {
	bogus := "/nonexistent/go-regtest-test/bitcoind"
	_, err := New(&Config{BinaryPath: bogus})
	if err == nil {
		t.Fatal("expected New to fail for nonexistent BinaryPath, got nil")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("error %q should mention the bogus path %q", err.Error(), bogus)
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("error should wrap ErrBinaryNotFound, got %v", err)
	}
}

// Test_RunWithContext_PreCancelled verifies that an already-done ctx returns
// ctx.Err() without running fn. Without the pre-check, the select could still
// return ctx.Err() while fn runs afterwards in its goroutine, so the test
// waits a bounded interval for fn to signal.

func TestRunWithContext_PreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := make(chan struct{}, 1)
	_, err := runWithContext(ctx, func() (int, error) {
		ran <- struct{}{}
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	select {
	case <-ran:
		t.Error("fn ran despite a pre-cancelled ctx")
	case <-time.After(200 * time.Millisecond):
	}
}

// Test_HexHelpers round-trips transactions, blocks, and headers through the
// encode.go helpers, including the legacy-encoding fallback for a tx with no
// inputs.

func TestHexHelpers(t *testing.T) {
	witnessTx := wire.NewMsgTx(2)
	witnessTx.AddTxIn(&wire.TxIn{Witness: wire.TxWitness{{0x01, 0x02}}})
	witnessTx.AddTxOut(wire.NewTxOut(1000, []byte{0x51}))

	noInputTx := wire.NewMsgTx(2)
	noInputTx.AddTxOut(wire.NewTxOut(1000, []byte{0x51}))
	noInputTx.AddTxOut(wire.NewTxOut(2000, []byte{0x52}))

	for name, tx := range map[string]*wire.MsgTx{"witness": witnessTx, "no inputs": noInputTx} {
		h, err := txHex(tx)
		if err != nil {
			t.Fatalf("%s: txHex: %v", name, err)
		}
		got, err := decodeTxHex(h)
		if err != nil {
			t.Fatalf("%s: decodeTxHex: %v", name, err)
		}
		if got.TxHash() != tx.TxHash() || got.WitnessHash() != tx.WitnessHash() {
			t.Errorf("%s: round-trip changed the tx", name)
		}
	}

	block := &wire.MsgBlock{Header: wire.BlockHeader{Version: 4, Nonce: 7}}
	block.AddTransaction(witnessTx)
	h, err := blockHex(block)
	if err != nil {
		t.Fatalf("blockHex: %v", err)
	}
	gotBlock, err := decodeBlockHex(h)
	if err != nil {
		t.Fatalf("decodeBlockHex: %v", err)
	}
	if gotBlock.BlockHash() != block.BlockHash() || len(gotBlock.Transactions) != 1 {
		t.Error("block round-trip changed the block")
	}

	gotHdr, err := decodeHeaderHex(h[:160]) // the header is the first 80 bytes
	if err != nil {
		t.Fatalf("decodeHeaderHex: %v", err)
	}
	if gotHdr.BlockHash() != block.BlockHash() {
		t.Error("header round-trip changed the header")
	}

	if _, err := decodeTxHex("zz"); err == nil {
		t.Error("decodeTxHex accepted invalid hex")
	}
	if _, err := decodeBlockHex("00"); err == nil {
		t.Error("decodeBlockHex accepted a truncated block")
	}
}

// fakeRPC returns a Regtest wired to an in-process JSON-RPC server, so the
// call helpers can be tested without bitcoind. reply receives each request's
// method and compacted params and returns the raw JSON for the response's
// "result" and "error" members.

func TestCall_FakeRPC(t *testing.T) {
	ctx := context.Background()
	const hash = "0f9188f13cb7b2c71f2a335e3a4fc328bf5beb436012afca590b1a11466e2206"
	var mu sync.Mutex
	var result, rpcErr string
	rt := fakeRPC(t, func(string, string) (string, string) {
		mu.Lock()
		defer mu.Unlock()
		return result, rpcErr
	})
	set := func(res, e string) {
		mu.Lock()
		defer mu.Unlock()
		result, rpcErr = res, e
	}

	set(`42`, `null`)
	if n, err := call[int64](ctx, rt, "getblockcount"); err != nil || n != 42 {
		t.Errorf("call[int64] = %d, %v; want 42, nil", n, err)
	}

	set(`"abc"`, `null`)
	_, err := call[int64](ctx, rt, "getblockcount")
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) || !strings.Contains(err.Error(), "unmarshal getblockcount") {
		t.Errorf("expected wrapped unmarshal error, got %v", err)
	}

	set(`null`, `{"code":-18,"message":"Path does not exist."}`)
	_, err = rt.LoadWallet("w")
	if e := asRPCError(err); e == nil || e.Code != btcjson.ErrRPCWalletNotFound {
		t.Errorf("expected *btcjson.RPCError with code -18 in chain, got %v", err)
	}

	set(`"`+hash+`"`, `null`)
	if h, err := rt.GetBestBlockHash(); err != nil || h.String() != hash {
		t.Errorf("GetBestBlockHash = %v, %v; want %s", h, err, hash)
	}
	set(`"not-a-hash"`, `null`)
	if _, err := rt.GetBestBlockHash(); err == nil || !strings.Contains(err.Error(), "parse getbestblockhash result") {
		t.Errorf("expected hash parse error, got %v", err)
	}

	set(`null`, `null`)
	if err := rt.SubmitBlock(&wire.MsgBlock{}); err != nil {
		t.Errorf("SubmitBlock with null result: %v", err)
	}
	set(`"bad-cb-amount"`, `null`)
	if err := rt.SubmitBlock(&wire.MsgBlock{}); err == nil || !strings.Contains(err.Error(), "bad-cb-amount") {
		t.Errorf("expected SubmitBlock to surface the reject reason, got %v", err)
	}

	set(`null`, `null`)
	if out, err := rt.GetTxOut(&chainhash.Hash{}, 0, true); out != nil || err != nil {
		t.Errorf("GetTxOut with null result = %v, %v; want nil, nil", out, err)
	}
}

// Test_RPCParams_FakeRPC pins the JSON params each converted wrapper sends.

func TestRPCParams_FakeRPC(t *testing.T) {
	var mu sync.Mutex
	var got []string
	rt := fakeRPC(t, func(method, params string) (string, string) {
		mu.Lock()
		got = append(got, method+" "+params)
		mu.Unlock()
		return `null`, `null`
	})
	last := func() string {
		mu.Lock()
		defer mu.Unlock()
		return got[len(got)-1]
	}

	const addrStr = "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl"
	addr, err := btcutil.DecodeAddress(addrStr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{})
	tx.AddTxOut(wire.NewTxOut(0, []byte{0x6a}))
	txh, _ := txHex(tx)
	var hash chainhash.Hash
	lockTime := int64(5)

	cases := []struct {
		call func()
		want string
	}{
		{func() { _, _ = rt.TestMempoolAccept(tx) }, `testmempoolaccept [["` + txh + `"],0]`},
		{func() { _, _ = rt.CreateRawTransaction(nil, map[btcutil.Address]btcutil.Amount{addr: 100_000}, nil) },
			`createrawtransaction [[],{"` + addrStr + `":0.001}]`},
		{func() {
			_, _ = rt.CreateRawTransaction([]btcjson.TransactionInput{{Txid: "ab", Vout: 1}}, nil, &lockTime)
		}, `createrawtransaction [[{"txid":"ab","vout":1}],{},5]`},
		{func() { _, _ = rt.FundRawTransaction(tx, nil) }, `fundrawtransaction ["` + txh + `",{}]`},
		{func() { _, _ = rt.SendToAddress(addrStr, 100_000) }, `sendtoaddress ["` + addrStr + `",0.001]`},
		{func() { _ = rt.Warp(3, addrStr) }, `generatetoaddress [3,"` + addrStr + `"]`},
		{func() { _, _ = rt.GetBlock(&hash) }, `getblock ["` + hash.String() + `",0]`},
		{func() { _, _ = rt.GetBlockVerbose(&hash) }, `getblock ["` + hash.String() + `",1]`},
		{func() { _, _ = rt.GetBlockHeader(&hash) }, `getblockheader ["` + hash.String() + `",false]`},
		{func() { _, _ = rt.GetBlockTemplate(nil) }, `getblocktemplate []`},
		{func() {
			_, _ = rt.GetBlockTemplate(&btcjson.TemplateRequest{Mode: "template", Rules: []string{"segwit"}})
		}, `getblocktemplate [{"mode":"template","rules":["segwit"]}]`},
		{func() { _, _ = rt.GetTxOut(&hash, 2, true) }, `gettxout ["` + hash.String() + `",2,true]`},
		{func() { _, _ = rt.CreateWallet("w") }, `createwallet ["w"]`},
		{func() { _ = rt.UnloadWallet("w") }, `unloadwallet ["w"]`},
		{func() { _ = rt.AddNode("127.0.0.1:1") }, `addnode ["127.0.0.1:1","add"]`},
		{func() { _ = rt.InvalidateBlock(&hash) }, `invalidateblock ["` + hash.String() + `"]`},
	}
	for _, c := range cases {
		c.call()
		if g := last(); g != c.want {
			t.Errorf("sent %s\nwant %s", g, c.want)
		}
	}
}

// Test_EnsureWallet_FakeRPC walks EnsureWallet's branches using the Core v30
// error codes: -35 for an already-loaded wallet, -18 for a missing one, and
// the generic -4 (with "already exists") for createwallet on an existing one.

func TestEnsureWallet_FakeRPC(t *testing.T) {
	const (
		ok          = `null`
		notFound    = `{"code":-18,"message":"Wallet file verification failed. Path does not exist."}`
		loaded      = `{"code":-35,"message":"Wallet \"w\" is already loaded."}`
		exists      = `{"code":-4,"message":"Wallet file verification failed. Database already exists."}`
		otherWallet = `{"code":-4,"message":"Wallet file verification failed. Something else."}`
	)
	cases := []struct {
		name    string
		replies []string // error JSON for each successive call
		want    []string // methods called
		wantErr bool
	}{
		{"loads", []string{ok}, []string{"loadwallet"}, false},
		{"already loaded", []string{loaded}, []string{"loadwallet"}, false},
		{"creates", []string{notFound, ok}, []string{"loadwallet", "createwallet"}, false},
		{"create races existing", []string{notFound, exists, ok},
			[]string{"loadwallet", "createwallet", "loadwallet"}, false},
		{"create fails", []string{notFound, otherWallet}, []string{"loadwallet", "createwallet"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var methods []string
			rt := fakeRPC(t, func(method, _ string) (string, string) {
				mu.Lock()
				defer mu.Unlock()
				methods = append(methods, method)
				e := c.replies[len(methods)-1]
				if e == ok {
					return `{"name":"w"}`, `null`
				}
				return `null`, e
			})
			err := rt.EnsureWallet("w")
			if (err != nil) != c.wantErr {
				t.Errorf("EnsureWallet err = %v, wantErr %v", err, c.wantErr)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(methods, c.want) {
				t.Errorf("called %v, want %v", methods, c.want)
			}
		})
	}
}
