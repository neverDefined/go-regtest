package regtest

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// TestWallet_TwoWallets moves coins between two loaded wallets. With two
// wallets loaded, bitcoind rejects wallet RPCs on the default endpoint, which
// is what the Wallet handle exists for.
func TestWallet_TwoWallets(t *testing.T) {
	t.Parallel()
	rt, aliceAddr := fundedWallet(t, "alice")
	if err := rt.EnsureWallet("bob"); err != nil {
		t.Fatalf("EnsureWallet(bob): %v", err)
	}
	if _, err := rt.GenerateBech32(""); err == nil {
		t.Fatal("expected the default-endpoint call to fail with two wallets loaded")
	}

	alice, bob := rt.Wallet("alice"), rt.Wallet("bob")
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

// TestWallet_AddressTypes checks each AddressType yields that output type.
func TestWallet_AddressTypes(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
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

// TestWallet_EscapedName checks a wallet name that needs URL escaping.
func TestWallet_EscapedName(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
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

func TestWalletValidation(t *testing.T) {
	rt := unstarted(t)
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

// TestWallet_AcrossRestart checks a handle kept across Stop/Start reports
// ErrNotConnected while stopped and works again once its wallet exists in
// the new chain (Start wipes the data dir).
func TestWallet_AcrossRestart(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	w := rt.Wallet("w")
	if _, err := w.Info(); err != nil {
		t.Fatalf("Info before restart: %v", err)
	}

	if err := rt.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Info(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("while stopped: want ErrNotConnected, got %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	if info, err := w.Info(); err != nil || info.WalletName != "w" {
		t.Errorf("after restart: Info = %+v, %v", info, err)
	}
}

// TestWallet_ConcurrentFirstUse checks concurrent first calls share one
// cached client per wallet.
func TestWallet_ConcurrentFirstUse(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	const n = 8
	clients := make(chan any, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := rt.walletClient("w")
			if err != nil {
				errs <- err
				return
			}
			clients <- c
		}()
	}
	wg.Wait()
	close(clients)
	close(errs)
	for err := range errs {
		t.Fatalf("walletClient: %v", err)
	}
	var first any
	for c := range clients {
		if first == nil {
			first = c
		} else if c != first {
			t.Fatal("concurrent first use created more than one client for the same wallet")
		}
	}
}

// TestEnsureWallet_States drives EnsureWallet through a missing, an
// already-loaded, and an unloaded-on-disk wallet, and pins the Core error
// codes its branches rely on.
func TestEnsureWallet_States(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	const name = "ensure_wallet"
	if err := rt.EnsureWallet(name); err != nil { // missing: created
		t.Fatalf("EnsureWallet (missing): %v", err)
	}
	if err := rt.EnsureWallet(name); err != nil { // already loaded
		t.Fatalf("EnsureWallet (loaded): %v", err)
	}
	if err := rt.UnloadWallet(name); err != nil {
		t.Fatalf("UnloadWallet: %v", err)
	}
	if err := rt.EnsureWallet(name); err != nil { // on disk, unloaded
		t.Fatalf("EnsureWallet (unloaded): %v", err)
	}

	_, err := rt.LoadWallet(name)
	if e := asRPCError(err); e == nil || e.Code != rpcWalletAlreadyLoaded {
		t.Errorf("loadwallet on a loaded wallet: want code %d, got %v", rpcWalletAlreadyLoaded, err)
	}
	_, err = rt.CreateWallet(name)
	if e := asRPCError(err); e == nil || e.Code != btcjson.ErrRPCWallet || !strings.Contains(e.Message, "already exists") {
		t.Errorf("createwallet on an existing wallet: want code -4 \"already exists\", got %v", err)
	}
	_, err = rt.LoadWallet("missing_wallet")
	if e := asRPCError(err); e == nil || e.Code != btcjson.ErrRPCWalletNotFound {
		t.Errorf("loadwallet on a missing wallet: want code -18, got %v", err)
	}
}

func TestWalletInformation(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	err := rt.EnsureWallet(minerWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}

	t.Logf("ensured wallet: %s", minerWallet)

	info, err := rt.GetWalletInformation()
	if err != nil {
		t.Fatalf("failed to get wallet info: %v", err)
	}

	if info.WalletName == "" {
		t.Error("WalletName empty")
	}
}

func TestWalletManagement(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	err := rt.EnsureWallet(userWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}

	if _, err := rt.GetWalletInformation(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateAddress(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	err := rt.EnsureWallet(userWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}

	addr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate address: %v", err)
	}

	t.Logf("generated bech32 address: %s", addr)

	if _, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams); err != nil {
		t.Fatalf("failed to decode address: %v", err)
	}

	t.Log("address is valid")

	bech32m, err := rt.GenerateBech32m(userWallet)
	if err != nil {
		t.Fatalf("failed to generate bech32m address: %v", err)
	}

	t.Logf("generated bech32m address: %s", bech32m)

	if _, err := btcutil.DecodeAddress(bech32m, &chaincfg.RegressionNetParams); err != nil {
		t.Fatalf("failed to decode bech32m address: %v", err)
	}

	t.Log("bech32m address is valid")
}
