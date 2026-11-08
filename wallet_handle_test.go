package regtest

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// startNode starts a node on a fixed port for this file's tests.
func startNode(t *testing.T, port string) *Regtest {
	t.Helper()
	rt, err := New(&Config{Host: "127.0.0.1:" + port, DataDir: filepath.Join(t.TempDir(), "d")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })
	return rt
}

// Test_Wallet_TwoWallets moves coins between two loaded wallets. With two
// wallets loaded, bitcoind rejects wallet RPCs on the default endpoint, which
// is what the Wallet handle exists for.
func Test_Wallet_TwoWallets(t *testing.T) {
	rt := startNode(t, "19960")
	for _, name := range []string{"alice", "bob"} {
		if err := rt.EnsureWallet(name); err != nil {
			t.Fatalf("EnsureWallet(%s): %v", name, err)
		}
	}
	if _, err := rt.GenerateBech32(""); err == nil {
		t.Fatal("expected the default-endpoint call to fail with two wallets loaded")
	}

	alice, bob := rt.Wallet("alice"), rt.Wallet("bob")
	aliceAddr, err := alice.NewAddress("", AddressBech32)
	if err != nil {
		t.Fatalf("alice.NewAddress: %v", err)
	}
	if err := rt.Warp(101, aliceAddr); err != nil {
		t.Fatalf("Warp: %v", err)
	}
	bobAddr, err := bob.NewAddress("", AddressBech32m)
	if err != nil {
		t.Fatalf("bob.NewAddress: %v", err)
	}
	if _, err := alice.SendToAddress(bobAddr, 100_000_000); err != nil {
		t.Fatalf("alice.SendToAddress: %v", err)
	}
	if err := rt.Warp(1, aliceAddr); err != nil {
		t.Fatalf("Warp: %v", err)
	}
	if bal, err := bob.Balance(); err != nil || bal != btcutil.Amount(100_000_000) {
		t.Fatalf("bob.Balance = %v, %v; want 1 BTC", bal, err)
	}
	if _, err := bob.SendToAddress(aliceAddr, 40_000_000); err != nil {
		t.Fatalf("bob.SendToAddress: %v", err)
	}
	if err := rt.Warp(1, aliceAddr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	// A tx spending one of bob's outputs: bob can sign it, alice can't.
	utxos, err := bob.ListUnspent(1)
	if err != nil || len(utxos) == 0 {
		t.Fatalf("bob.ListUnspent = %v, %v; want at least one", utxos, err)
	}
	dest, err := btcutil.DecodeAddress(aliceAddr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	spend, err := rt.CreateRawTransaction(
		[]btcjson.TransactionInput{{Txid: utxos[0].TxID, Vout: utxos[0].Vout}},
		map[btcutil.Address]btcutil.Amount{dest: 10_000_000},
		nil,
	)
	if err != nil {
		t.Fatalf("CreateRawTransaction: %v", err)
	}
	if _, err := bob.SignRawTransaction(spend); err != nil {
		t.Errorf("bob should sign his own input: %v", err)
	}
	if _, err := alice.SignRawTransaction(spend); err == nil {
		t.Error("alice must not be able to sign bob's input")
	}

	// alice funds an output-only tx from her own coins.
	skeleton := wire.NewMsgTx(2)
	skeleton.AddTxOut(wire.NewTxOut(5_000_000, mustPkScript(t, bobAddr)))
	funded, err := alice.FundRawTransaction(skeleton, nil)
	if err != nil {
		t.Fatalf("alice.FundRawTransaction: %v", err)
	}
	if len(funded.Transaction.TxIn) == 0 {
		t.Error("funded tx has no inputs")
	}
	if info, err := alice.Info(); err != nil || info.WalletName != "alice" {
		t.Errorf("alice.Info = %+v, %v", info, err)
	}
}

// Test_Wallet_AddressTypes checks each AddressType yields that output type.
func Test_Wallet_AddressTypes(t *testing.T) {
	rt := startNode(t, "19962")
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	w := rt.Wallet("w")
	cases := []struct {
		typ   AddressType
		check func(btcutil.Address) bool
	}{
		{AddressLegacy, func(a btcutil.Address) bool { _, ok := a.(*btcutil.AddressPubKeyHash); return ok }},
		{AddressP2SHSegwit, func(a btcutil.Address) bool { _, ok := a.(*btcutil.AddressScriptHash); return ok }},
		{AddressBech32, func(a btcutil.Address) bool { _, ok := a.(*btcutil.AddressWitnessPubKeyHash); return ok }},
		{AddressBech32m, func(a btcutil.Address) bool { _, ok := a.(*btcutil.AddressTaproot); return ok }},
	}
	for _, tc := range cases {
		s, err := w.NewAddress("", tc.typ)
		if err != nil {
			t.Errorf("NewAddress(%s): %v", tc.typ, err)
			continue
		}
		a, err := btcutil.DecodeAddress(s, &chaincfg.RegressionNetParams)
		if err != nil || !tc.check(a) {
			t.Errorf("NewAddress(%s) = %s (%T), err %v", tc.typ, s, a, err)
		}
	}
}

// Test_Wallet_EscapedName checks a wallet name that needs URL escaping.
func Test_Wallet_EscapedName(t *testing.T) {
	rt := startNode(t, "19964")
	const name = "my wallet #1"
	if err := rt.EnsureWallet(name); err != nil {
		t.Fatal(err)
	}
	info, err := rt.Wallet(name).Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.WalletName != name {
		t.Errorf("WalletName = %q, want %q", info.WalletName, name)
	}
}

// Test_Wallet_NotConnected checks Wallet methods before Start.
func Test_Wallet_NotConnected(t *testing.T) {
	rt, err := New(&Config{Host: "127.0.0.1:19966"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Wallet("w").Balance(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("want ErrNotConnected, got %v", err)
	}
}

func mustPkScript(t *testing.T, addr string) []byte {
	t.Helper()
	a, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	script, err := txscript.PayToAddrScript(a)
	if err != nil {
		t.Fatal(err)
	}
	return script
}
