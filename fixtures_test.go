package regtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// TestFundAddress checks FundAddress returns confirmed outputs of the right
// value, spends its own change on the next call without waiting for it to
// mature, and loads or unloads no wallets.
func TestFundAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rt := StartT(t, nil)
	if err := rt.EnsureWallet("w"); err != nil {
		t.Fatal(err)
	}
	addr, err := rt.Wallet("w").NewAddress("", AddressBech32)
	if err != nil {
		t.Fatal(err)
	}
	walletsBefore, err := call[[]string](ctx, rt, "listwallets")
	if err != nil {
		t.Fatal(err)
	}

	// The faucet starts empty: it mines 101 blocks, then spends one 50 BTC
	// coinbase and keeps the rest as change.
	first := fundAndCheck(t, rt, addr, 12_345_678)

	// 140 BTC is more than the two 50 BTC coinbases now mature, so the
	// faucet must also spend that change, which has one confirmation.
	height := blockCount(t, rt)
	second := fundAndCheck(t, rt, addr, 14_000_000_000)
	if got := blockCount(t, rt); got != height+1 {
		t.Errorf("second call mined %d blocks, want 1", got-height)
	}
	tx := minedTx(t, rt, &second.Hash)
	change := wire.OutPoint{Hash: first.Hash, Index: 1}
	spent := false
	for _, in := range tx.TxIn {
		spent = spent || in.PreviousOutPoint == change
	}
	if !spent {
		t.Errorf("second funding tx doesn't spend the first one's change %v", change)
	}

	height = blockCount(t, rt)
	fundAndCheck(t, rt, addr, 1_000)
	if got := blockCount(t, rt); got != height+1 {
		t.Errorf("third call mined %d blocks, want 1", got-height)
	}

	walletsAfter, err := call[[]string](ctx, rt, "listwallets")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(walletsBefore, walletsAfter) {
		t.Errorf("loaded wallets changed: before %v, after %v", walletsBefore, walletsAfter)
	}
	if _, err := rt.GenerateBech32(""); err != nil {
		t.Errorf("default-wallet call after FundAddress: %v", err)
	}
}

// TestFundAddress_BlockMinTxFee checks FundAddress confirms its transaction
// even when the fee rate is below -blockmintxfee, which keeps it out of
// blocks built from the mempool.
func TestFundAddress_BlockMinTxFee(t *testing.T) {
	t.Parallel()
	rt := StartT(t, &Config{ExtraArgs: []string{"-blockmintxfee=0.01"}})
	fundAndCheck(t, rt, p2wpkhAddr(t, 1), 100_000)
}

// TestFundAddress_LongChain funds a small amount at height 3200, where the
// block subsidy has halved 21 times to 2,384 sats.
func TestFundAddress_LongChain(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	if err := rt.MineToHeight(3200, p2wpkhAddr(t, 1)); err != nil {
		t.Fatal(err)
	}
	fundAndCheck(t, rt, p2wpkhAddr(t, 2), 1_000)
}

// TestFundAddress_ContextWhileWaiting checks a call waiting for another
// FundAddress gives up when its ctx is done.
func TestFundAddress_ContextWhileWaiting(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	addr := p2wpkhAddr(t, 1)

	sem := rt.faucetLock()
	sem <- struct{}{} // as if another FundAddress were running
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := rt.FundAddressContext(ctx, addr, 1_000)
	elapsed := time.Since(start)
	<-sem
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("returned after %v, want about 100ms", elapsed)
	}

	// Once the faucet is free again, FundAddress works.
	fundAndCheck(t, rt, addr, 1_000)
}

// TestFundAddress_LeavesNamedWalletAlone checks FundAddress doesn't touch a
// wallet named like the faucet wallet it used to load: the wallet stays
// loaded and its balance is unchanged.
func TestFundAddress_LeavesNamedWalletAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rt := StartT(t, nil)
	const name = "go-regtest-faucet"
	if _, err := rt.CreateWallet(name); err != nil {
		t.Fatal(err)
	}
	w := rt.Wallet(name)
	waddr, err := w.NewAddress("", AddressBech32)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Warp(101, waddr); err != nil {
		t.Fatal(err)
	}
	before := walletTotal(t, w)

	fundAndCheck(t, rt, p2wpkhAddr(t, 1), 1_000_000)

	wallets, err := call[[]string](ctx, rt, "listwallets")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(wallets, name) {
		t.Fatalf("wallet %q was unloaded; loaded: %v", name, wallets)
	}
	if after := walletTotal(t, w); after != before {
		t.Errorf("wallet %q balance changed from %v to %v", name, before, after)
	}
}

func TestFundAddress_Validation(t *testing.T) {
	t.Parallel()
	rt, err := New(&Config{Host: "127.0.0.1:19972"})
	if err != nil {
		t.Fatal(err)
	}
	addr := p2wpkhAddr(t, 1)
	for _, sats := range []int64{0, -1, btcutil.MaxSatoshi + 1} {
		if _, err := rt.FundAddress(addr, sats); err == nil {
			t.Errorf("FundAddress(%d sats) should fail", sats)
		}
	}
	if _, err := rt.FundAddress("not-an-address", 1_000); err == nil {
		t.Error("bad address should fail")
	}
	if _, err := rt.FundAddress(addr, 1_000); !errors.Is(err, ErrNotConnected) {
		t.Errorf("before Start: want ErrNotConnected, got %v", err)
	}
}

// TestFaucetMath checks the fee, coin selection and subsidy arithmetic
// behind FundAddress.
func TestFaucetMath(t *testing.T) {
	t.Parallel()
	if got := faucetFee(1, 1); got != 220 { // ceil(10.5+68+31) = 110 vB
		t.Errorf("faucetFee(1, 1) = %d, want 220", got)
	}
	if got := faucetFee(2, 2); got != 418 { // 10.5+136+62 = 208.5 -> 209 vB
		t.Errorf("faucetFee(2, 2) = %d, want 418", got)
	}

	coin := func(v int64) faucetCoin { return faucetCoin{value: v} }
	coins, change, ok := selectCoins([]faucetCoin{coin(5_000), coin(100_000), coin(20_000)}, 50_000)
	if !ok || len(coins) != 1 || coins[0].value != 100_000 || change != 100_000-50_000-faucetFee(1, 2) {
		t.Errorf("largest first: got %v, change %d, ok %v", coins, change, ok)
	}
	// Change under faucetMinChange goes to the fee.
	if _, change, ok := selectCoins([]faucetCoin{coin(50_000 + faucetFee(1, 2) + 999)}, 50_000); !ok || change != 0 {
		t.Errorf("small change: change %d, ok %v; want 0, true", change, ok)
	}
	if _, _, ok := selectCoins([]faucetCoin{coin(50_000)}, 50_000); ok {
		t.Error("coins that can't also pay the fee should not suffice")
	}

	for _, c := range []struct{ height, want int64 }{
		{0, 5_000_000_000}, {149, 5_000_000_000}, {150, 2_500_000_000},
		{3200, 2_384}, {150 * 32, 1}, {150 * 33, 0}, {150 * 64, 0},
	} {
		if got := blockSubsidy(c.height); got != c.want {
			t.Errorf("blockSubsidy(%d) = %d, want %d", c.height, got, c.want)
		}
	}
	if got := subsidyFrom(150*32 + 100); got != 50 {
		t.Errorf("subsidyFrom(4900) = %d, want 50", got)
	}
	// 50 BTC halved with rounding down, 150 blocks per halving.
	if got, want := subsidyFrom(0), int64(150*9_999_999_989); got != want {
		t.Errorf("subsidyFrom(0) = %d, want %d", got, want)
	}
}

// fundAndCheck calls FundAddress and checks the output is confirmed and pays
// sats to addr.
func fundAndCheck(t *testing.T, rt *Regtest, addr string, sats int64) wire.OutPoint {
	t.Helper()
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
	dest, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	script, err := txscript.PayToAddrScript(dest)
	if err != nil {
		t.Fatal(err)
	}
	if out.ScriptPubKey.Hex != hex.EncodeToString(script) {
		t.Errorf("output %v pays script %s, want %x", op, out.ScriptPubKey.Hex, script)
	}
	return op
}

// p2wpkhAddr returns a regtest P2WPKH address whose key hash is 20 copies of
// b. Nobody holds its key, which doesn't matter for mining to it.
func p2wpkhAddr(t *testing.T, b byte) string {
	t.Helper()
	addr, err := btcutil.NewAddressWitnessPubKeyHash(bytes.Repeat([]byte{b}, 20), &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatal(err)
	}
	return addr.EncodeAddress()
}

func blockCount(t *testing.T, rt *Regtest) int64 {
	t.Helper()
	n, err := rt.GetBlockCount()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// minedTx returns the transaction txid from the tip block.
func minedTx(t *testing.T, rt *Regtest, txid *chainhash.Hash) *wire.MsgTx {
	t.Helper()
	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatal(err)
	}
	block, err := rt.GetBlock(tip)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range block.Transactions {
		if tx.TxHash() == *txid {
			return tx
		}
	}
	t.Fatalf("tx %s is not in the tip block %s", txid, tip)
	return nil
}

// walletTotal returns w's balance including immature coinbases, a total
// that doesn't change when blocks mined elsewhere mature them.
func walletTotal(t *testing.T, w *Wallet) btcutil.Amount {
	t.Helper()
	res, err := call[struct {
		Mine struct {
			Trusted          float64 `json:"trusted"`
			UntrustedPending float64 `json:"untrusted_pending"`
			Immature         float64 `json:"immature"`
		} `json:"mine"`
	}](context.Background(), w, "getbalances")
	if err != nil {
		t.Fatal(err)
	}
	total, err := btcutil.NewAmount(res.Mine.Trusted + res.Mine.UntrustedPending + res.Mine.Immature)
	if err != nil {
		t.Fatal(err)
	}
	return total
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
	ha, hb := blockCount(t, a), blockCount(t, b)
	if ha != 5 || hb != 5 {
		t.Errorf("heights after sync: a=%d b=%d, want 5", ha, hb)
	}
	if ta, tb := bestHash(t, a), bestHash(t, b); *ta != *tb {
		t.Errorf("tips after sync differ: a=%s b=%s", ta, tb)
	}
}

// TestSyncBlocks_DivergentTips checks SyncBlocks compares tips, not heights:
// unconnected nodes at the same height on different chains never sync.
func TestSyncBlocks_DivergentTips(t *testing.T) {
	t.Parallel()
	a, b := StartT(t, nil), StartT(t, nil)
	// Different coinbase addresses, so the two chains differ.
	if err := a.Warp(3, p2wpkhAddr(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := b.Warp(3, p2wpkhAddr(t, 2)); err != nil {
		t.Fatal(err)
	}
	if ha, hb := blockCount(t, a), blockCount(t, b); ha != 3 || hb != 3 {
		t.Fatalf("heights: a=%d b=%d, want 3", ha, hb)
	}
	if ta, tb := bestHash(t, a), bestHash(t, b); *ta == *tb {
		t.Fatalf("both nodes have tip %s, want different chains", ta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := SyncBlocksContext(ctx, a, b); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("equal heights, different tips: want DeadlineExceeded, got %v", err)
	}
}

func bestHash(t *testing.T, rt *Regtest) *chainhash.Hash {
	t.Helper()
	h, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
