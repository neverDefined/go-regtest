package regtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

// TestFundAddress checks FundAddress returns a confirmed output of the right
// value, works repeatedly, and leaves only the caller's wallets loaded.
func TestFundAddress(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	addr, err := rt.Wallet("w").NewAddress("", AddressBech32)
	if err != nil {
		t.Fatal(err)
	}

	for _, sats := range []int64{12_345_678, 1_000} {
		op, err := rt.FundAddress(addr, sats)
		if err != nil {
			t.Fatalf("FundAddress(%d): %v", sats, err)
		}
		out, err := rt.GetTxOut(&op.Hash, op.Index, false)
		if err != nil || out == nil {
			t.Fatalf("GetTxOut(%v) = %v, %v; want a confirmed output", op, out, err)
		}
		if got, _ := btcutil.NewAmount(out.Value); int64(got) != sats || out.Confirmations < 1 {
			t.Errorf("output %v: value %v, confirmations %d; want %d sats, >= 1", op, got, out.Confirmations, sats)
		}
	}

	// The faucet wallet is unloaded again, so the default endpoint (one
	// wallet loaded) still works.
	if _, err := rt.GenerateBech32(""); err != nil {
		t.Errorf("default-wallet call after FundAddress: %v", err)
	}
}

// TestGenerateBlock_NonStandardTx confirms a consensus-valid transaction the
// mempool rejects (tx version 4 is non-standard).
func TestGenerateBlock_NonStandardTx(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	w := rt.Wallet("w")
	addr, err := w.NewAddress("", AddressBech32)
	if err != nil {
		t.Fatal(err)
	}
	op, err := rt.FundAddress(addr, 100_000_000)
	if err != nil {
		t.Fatalf("FundAddress: %v", err)
	}
	dest, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := rt.CreateRawTransaction(
		[]btcjson.TransactionInput{{Txid: op.Hash.String(), Vout: op.Index}},
		map[btcutil.Address]btcutil.Amount{dest: 99_990_000},
		nil,
	)
	if err != nil {
		t.Fatalf("CreateRawTransaction: %v", err)
	}
	tx.Version = 4
	signed, err := w.SignRawTransaction(tx)
	if err != nil {
		t.Fatalf("SignRawTransaction: %v", err)
	}

	res, err := rt.TestMempoolAccept(signed)
	if err != nil {
		t.Fatalf("TestMempoolAccept: %v", err)
	}
	if res[0].Allowed || !strings.Contains(res[0].RejectReason, "version") {
		t.Fatalf("want a 'version' policy rejection, got allowed=%v reason=%q", res[0].Allowed, res[0].RejectReason)
	}

	hash, err := rt.GenerateBlock(addr, signed)
	if err != nil {
		t.Fatalf("GenerateBlock: %v", err)
	}
	block, err := rt.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if len(block.Transactions) != 2 || block.Transactions[1].TxHash() != signed.TxHash() {
		t.Errorf("block should hold the coinbase and our tx, got %d txs", len(block.Transactions))
	}
}

func TestGenerateBlock_Validation(t *testing.T) {
	t.Parallel()
	rt, err := New(&Config{Host: "127.0.0.1:19970"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.GenerateBlock(""); err == nil {
		t.Error("empty miner should fail")
	}
	if _, err := rt.GenerateBlock("not-an-address"); err == nil {
		t.Error("bad miner address should fail")
	}
	const addr = "bcrt1qw508d6qejxtdg4y5r3zarvary0c5xw7kygt080"
	if _, err := rt.GenerateBlock(addr, nil); err == nil {
		t.Error("nil tx should fail")
	}
	if _, err := rt.GenerateBlock(addr); !errors.Is(err, ErrNotConnected) {
		t.Errorf("before Start: want ErrNotConnected, got %v", err)
	}
}

// TestSyncBlocks checks SyncBlocks waits for a connected peer to catch up,
// and times out for nodes that can't sync.
func TestSyncBlocks(t *testing.T) {
	t.Parallel()
	a, b := StartT(t, nil), StartT(t, nil)
	if err := a.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	addr, err := a.Wallet("w").NewAddress("", AddressBech32)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Warp(5, addr); err != nil {
		t.Fatal(err)
	}

	// Not connected: b never sees a's blocks.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := SyncBlocksContext(ctx, a, b); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unconnected nodes: want DeadlineExceeded, got %v", err)
	}

	if err := a.Connect(b); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := SyncBlocks(a, b); err != nil {
		t.Fatalf("SyncBlocks: %v", err)
	}
	ha, _ := a.GetBlockCount()
	hb, _ := b.GetBlockCount()
	if ha != 5 || hb != 5 {
		t.Errorf("heights after sync: a=%d b=%d, want 5", ha, hb)
	}
}
