package regtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// randomString generates a random string of the given length.
func randomString(length int) string {
	b := make([]byte, length+2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[2 : length+2]
}

var (
	minerWallet = "miner"
	userWallet  = "user"
)

func TestRPC_Connection(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	err = rt.HealthCheck()
	if err != nil {
		t.Fatalf("failed to check health: %v", err)
	}

	t.Log("health check passed")
}

func TestRPC_WalletInformation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	err = rt.EnsureWallet(minerWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	t.Logf("ensured wallet: %s", minerWallet)

	info, err := rt.GetWalletInformation()
	if err != nil {
		t.Fatalf("failed to get wallet info: %v", err)
	}

	t.Logf("wallet info: %+v", info)
}

func TestRPC_WalletManagement(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	err = rt.EnsureWallet(userWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	t.Logf("ensured wallet: %s", userWallet)
}

func TestRPC_GenerateAddress(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	err = rt.EnsureWallet(userWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	addr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate address: %v", err)
	}

	t.Logf("generated bech32 address: %s", addr)

	if _, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams); err != nil {
		t.Fatalf("failed to decode address: %v", err)
	}

	t.Log("address is valid")

	bech32m, err := rt.GenerateBech32m(randomString(10))
	if err != nil {
		t.Fatalf("failed to generate bech32m address: %v", err)
	}

	t.Logf("generated bech32m address: %s", bech32m)

	if _, err := btcutil.DecodeAddress(bech32m, &chaincfg.RegressionNetParams); err != nil {
		t.Fatalf("failed to decode bech32m address: %v", err)
	}

	t.Log("bech32m address is valid")
}

func TestRPC_Warp(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	err = rt.EnsureWallet(minerWallet)
	if err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	startHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count: %v", err)
	}
	t.Logf("starting block count: %d", startHeight)

	startingBalance, err := rt.Client().GetBalance("*")
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}
	t.Logf("starting balance: %v", startingBalance)

	minerAddr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("failed to generate miner address: %v", err)
	}

	if err = rt.Warp(10, minerAddr); err != nil {
		t.Fatalf("failed to warp: %v", err)
	}

	endHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count: %v", err)
	}
	t.Logf("ending block count: %d", endHeight)

	if endHeight != startHeight+10 {
		t.Fatalf("block count did not increase by 10: %d != %d", endHeight, startHeight+10)
	}

	// warp 120 blocks and check if the miner address has the rewards
	if err := rt.Warp(120, minerAddr); err != nil {
		t.Fatalf("failed to warp: %v", err)
	}

	endingBalance, err := rt.Client().GetBalance("*")
	if err != nil {
		t.Fatalf("failed to get balance: %v", err)
	}

	if endingBalance < startingBalance {
		t.Fatalf("balance did not increase: %v < %v", endingBalance, startingBalance)
	}

	t.Logf("balance increased: %v -> %v", startingBalance, endingBalance)
}

func TestRPC_SendToAddress(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	if err = rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	fromAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate address: %v", err)
	}

	toAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate address: %v", err)
	}

	if err := rt.Warp(150, fromAddr); err != nil {
		t.Fatalf("failed to warp: %v", err)
	}

	txid, err := rt.SendToAddress(toAddr, 100000000)
	if err != nil {
		t.Fatalf("failed to send to address: %v", err)
	}

	t.Logf("sent to address: %s", txid)

	res, err := rt.GetTxOut(txid, 0, true)
	if err != nil {
		t.Fatalf("failed to get tx out: %v", err)
	}

	t.Logf("tx out: %+v", res)
}

func TestRPC_ScanTxOutSetForAddress(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	if err = rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	// Fund the miner address
	minerAddr, _ := rt.GenerateBech32("miner")
	if err := rt.Warp(101, minerAddr); err != nil {
		t.Fatal(err)
	}

	// Create a new address
	addr, err := rt.GenerateBech32m("test_scan")
	if err != nil {
		t.Fatal(err)
	}

	// Scan before funding (should be empty)
	results, err := rt.ScanTxOutSetForAddress(addr)
	if err != nil {
		t.Fatalf("ScanTxOutSetForAddress failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	// Send funds to address
	amount := int64(50_000_000) // 0.5 BTC
	txid, err := rt.SendToAddress(addr, amount)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to confirm
	if err := rt.Warp(1, minerAddr); err != nil {
		t.Fatal(err)
	}

	// Scan after funding (should find 1 UTXO)
	results, err = rt.ScanTxOutSetForAddress(addr)
	if err != nil {
		t.Fatalf("ScanTxOutSetForAddress failed after funding: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	utxo := results[0]
	if utxo.TxID != txid.String() {
		t.Errorf("expected txid %s, got %s", txid.String(), utxo.TxID)
	}

	expectedBTC := float64(amount) / 100_000_000
	if utxo.Amount != expectedBTC {
		t.Errorf("expected amount %.8f BTC, got %.8f BTC", expectedBTC, utxo.Amount)
	}

	t.Logf("Found UTXO: %s:%d with %.8f BTC", utxo.TxID, utxo.Vout, utxo.Amount)

	// Send 2 more transactions to same address
	rt.SendToAddress(addr, 1_000_000)
	rt.SendToAddress(addr, 2_000_000)
	rt.Warp(1, minerAddr)

	results, _ = rt.ScanTxOutSetForAddress(addr)
	// Should have at least 2 UTXOs (the original might be spent as change)
	if len(results) < 2 {
		t.Errorf("expected at least 2 UTXOs, got %d", len(results))
	}

	t.Logf("Found %d UTXOs after sending multiple transactions", len(results))
}

func TestRPC_SignRawTransactionWithWallet(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest instance: %v", err)
	}

	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start bitcoin regtest: %v", err)
	}
	defer rt.Stop()

	// Setup wallet and fund it
	if err := rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("failed to ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	fromAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate from address: %v", err)
	}

	// Mine blocks to fund the wallet
	if err := rt.Warp(101, fromAddr); err != nil {
		t.Fatalf("failed to mine blocks: %v", err)
	}

	// Generate destination address
	toAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate to address: %v", err)
	}

	// Create an unsigned transaction using Bitcoin Core's wallet
	// First send coins to create a UTXO
	txid, err := rt.SendToAddress(fromAddr, 50000000) // 0.5 BTC
	if err != nil {
		t.Fatalf("failed to send to address: %v", err)
	}

	if err := rt.Warp(1, fromAddr); err != nil {
		t.Fatalf("failed to confirm transaction: %v", err)
	}

	t.Logf("Created funding transaction: %s", txid)

	// Get the UTXO details
	utxo, err := rt.GetTxOut(txid, 0, true)
	if err != nil {
		t.Fatalf("failed to get tx out: %v", err)
	}

	if utxo == nil {
		t.Fatal("UTXO not found")
	}

	t.Logf("UTXO confirmed with %.8f BTC", utxo.Value)

	// Create a test transaction to sign
	// We'll create a signed transaction, strip its signatures, then re-sign it
	txid2, err := rt.SendToAddress(toAddr, 10000000) // 0.1 BTC
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	t.Logf("Created test transaction: %s", txid2)

	// The transaction is already signed by SendToAddress,
	// but we can test by getting a UTXO and creating an unsigned tx manually
	// For simplicity, let's verify the method works by checking it doesn't error

	// Get the signed transaction
	getRawTxParams := []json.RawMessage{
		json.RawMessage(fmt.Sprintf(`"%s"`, txid2.String())),
	}

	rawTxResp, err := rt.Client().RawRequest("getrawtransaction", getRawTxParams)
	if err != nil {
		t.Fatalf("failed to get raw transaction: %v", err)
	}

	var txHex string
	if err := json.Unmarshal(rawTxResp, &txHex); err != nil {
		t.Fatalf("failed to unmarshal tx hex: %v", err)
	}

	// Decode the transaction
	txBytes, err := hex.DecodeString(txHex)
	if err != nil {
		t.Fatalf("failed to decode tx hex: %v", err)
	}

	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(bytes.NewReader(txBytes)); err != nil {
		t.Fatalf("failed to deserialize transaction: %v", err)
	}

	// Clear witness data to make it unsigned
	for i := range msgTx.TxIn {
		msgTx.TxIn[i].Witness = nil
		msgTx.TxIn[i].SignatureScript = nil
	}

	t.Log("Created unsigned transaction")

	// Now sign the unsigned transaction
	signedTx, err := rt.SignRawTransactionWithWallet(&msgTx)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}

	if signedTx == nil {
		t.Fatal("signed transaction is nil")
	}

	// Verify the transaction was signed (has witness data)
	hasWitness := false
	for _, txIn := range signedTx.TxIn {
		if len(txIn.Witness) > 0 {
			hasWitness = true
			break
		}
	}

	if !hasWitness {
		t.Error("signed transaction should have witness data")
	}

	t.Log("Transaction signed successfully")
	t.Logf("Signed transaction hash: %s", signedTx.TxHash())

	// Verify we can broadcast it
	broadcastedTxID, err := rt.BroadcastTransaction(signedTx)
	if err != nil {
		t.Fatalf("failed to broadcast signed transaction: %v", err)
	}

	t.Logf("Broadcasted signed transaction: %s", broadcastedTxID)

	// Mine a block to confirm
	if err := rt.Warp(1, fromAddr); err != nil {
		t.Fatalf("failed to mine confirmation block: %v", err)
	}

	// Verify transaction was confirmed
	confirmedUTXO, err := rt.GetTxOut(broadcastedTxID, 0, true)
	if err != nil {
		t.Fatalf("failed to get confirmed UTXO: %v", err)
	}

	if confirmedUTXO == nil {
		t.Fatal("confirmed UTXO not found")
	}

	t.Logf("Transaction confirmed with %d confirmations", confirmedUTXO.Confirmations)
}

// TestRPC_Warp_ValidationErrors covers the validation guards in WarpContext
// (regtest.go:821-826 era; now mining.go). These paths short-circuit before
// any RPC call, so no live node is required.
func TestRPC_Warp_ValidationErrors(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	cases := []struct {
		name   string
		blocks int64
		miner  string
	}{
		{"zero_blocks", 0, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl"},
		{"negative_blocks", -1, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl"},
		{"empty_miner", 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rt.Warp(tc.blocks, tc.miner); err == nil {
				t.Errorf("expected validation error for blocks=%d miner=%q, got nil", tc.blocks, tc.miner)
			}
		})
	}
}

// TestRPC_SendToAddress_ValidationErrors covers SendToAddressContext's input
// guards. As with Warp, these short-circuit before any RPC call.
func TestRPC_SendToAddress_ValidationErrors(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	cases := []struct {
		name string
		addr string
		sats int64
	}{
		{"zero_sats", "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl", 0},
		{"negative_sats", "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl", -1},
		{"empty_address", "", 1000},
		{"malformed_address", "definitely-not-an-address", 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := rt.SendToAddress(tc.addr, tc.sats); err == nil {
				t.Errorf("expected validation error for addr=%q sats=%d, got nil", tc.addr, tc.sats)
			}
		})
	}
}

// TestRPC_Concurrent_WarpAndSend stresses the dual-mutex pattern (mu for
// lifecycle, clientMu for client access) by interleaving Warp (which mines
// blocks) with SendToAddress (which spends from the wallet). Run under -race
// -count=10 to surface ordering bugs.
func TestRPC_Concurrent_WarpAndSend(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("ensure wallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	minerAddr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("generate miner addr: %v", err)
	}
	if err := rt.Warp(150, minerAddr); err != nil {
		t.Fatalf("warp to fund: %v", err)
	}

	recipient, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("generate recipient addr: %v", err)
	}

	const iterations = 20
	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := rt.Warp(1, minerAddr); err != nil {
				errs <- fmt.Errorf("warp %d: %w", i, err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := rt.SendToAddress(recipient, 100_000); err != nil {
				errs <- fmt.Errorf("send %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op failed: %v", err)
	}
}

// TestRPC_ChainState exercises the chain inspection wrappers added in
// chain.go: GetBlockChainInfo, GetBestBlockHash, GetBlockHash, GetBlock,
// GetBlockVerbose, GetBlockHeader, GetChainTips. After mining 10 blocks the
// tip hash should agree across queries and there should be exactly one
// active tip on the linear chain.
func TestRPC_ChainState(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt.Warp(10, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	info, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo: %v", err)
	}
	if info.Chain != "regtest" {
		t.Errorf("expected Chain=regtest, got %q", info.Chain)
	}
	if info.Blocks < 10 {
		t.Errorf("expected Blocks >= 10, got %d", info.Blocks)
	}
	if info.BestBlockHash == "" {
		t.Error("BestBlockHash empty")
	}

	bestHash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	if bestHash.String() != info.BestBlockHash {
		t.Errorf("GetBestBlockHash %s != info.BestBlockHash %s", bestHash, info.BestBlockHash)
	}

	tipHash, err := rt.GetBlockHash(info.Blocks)
	if err != nil {
		t.Fatalf("GetBlockHash(%d): %v", info.Blocks, err)
	}
	if !tipHash.IsEqual(bestHash) {
		t.Errorf("GetBlockHash(tip) %s != GetBestBlockHash %s", tipHash, bestHash)
	}

	block, err := rt.GetBlock(bestHash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if len(block.Transactions) == 0 {
		t.Error("expected at least one tx (coinbase) in block")
	}

	verbose, err := rt.GetBlockVerbose(bestHash)
	if err != nil {
		t.Fatalf("GetBlockVerbose: %v", err)
	}
	if verbose.Height != info.Blocks {
		t.Errorf("verbose Height %d != info.Blocks %d", verbose.Height, info.Blocks)
	}

	hdr, err := rt.GetBlockHeader(bestHash)
	if err != nil {
		t.Fatalf("GetBlockHeader: %v", err)
	}
	if hdr == nil {
		t.Error("GetBlockHeader returned nil header")
	}

	tips, err := rt.GetChainTips()
	if err != nil {
		t.Fatalf("GetChainTips: %v", err)
	}
	activeCount := 0
	for _, tip := range tips {
		if tip.Status == "active" {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active tip on linear chain, got %d (tips=%+v)", activeCount, tips)
	}
}

// TestRPC_MineToHeight verifies the idempotent target-height helper.
func TestRPC_MineToHeight(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	if err := rt.MineToHeight(20, addr); err != nil {
		t.Fatalf("MineToHeight(20): %v", err)
	}
	h, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h != 20 {
		t.Errorf("after MineToHeight(20), height = %d", h)
	}

	// Idempotent: second call mines nothing.
	if err := rt.MineToHeight(20, addr); err != nil {
		t.Fatalf("MineToHeight(20) idempotent: %v", err)
	}
	h2, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h2 != 20 {
		t.Errorf("idempotent call advanced height: %d -> %d", h, h2)
	}

	// target < current is also a no-op.
	if err := rt.MineToHeight(5, addr); err != nil {
		t.Fatalf("MineToHeight(5) when at 20: %v", err)
	}
	h3, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if h3 != 20 {
		t.Errorf("backward target rolled chain: %d -> %d", h, h3)
	}

	// Negative target → validation error.
	if err := rt.MineToHeight(-1, addr); err == nil {
		t.Error("MineToHeight(-1) should error")
	}
}

// TestRPC_Reorg_InvalidateReconsider exercises the InvalidateBlock and
// ReconsiderBlock primitives. After mining 5 blocks, invalidating the tip
// must drop the chain by one; reconsidering it must restore the original
// tip.
func TestRPC_Reorg_InvalidateReconsider(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt.Warp(5, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	beforeHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}

	if err := rt.InvalidateBlock(tip); err != nil {
		t.Fatalf("InvalidateBlock: %v", err)
	}
	afterHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount post-invalidate: %v", err)
	}
	if afterHeight != beforeHeight-1 {
		t.Errorf("height after invalidate = %d, want %d", afterHeight, beforeHeight-1)
	}

	if err := rt.ReconsiderBlock(tip); err != nil {
		t.Fatalf("ReconsiderBlock: %v", err)
	}
	restoredHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount post-reconsider: %v", err)
	}
	if restoredHeight != beforeHeight {
		t.Errorf("height after reconsider = %d, want %d", restoredHeight, beforeHeight)
	}
	restoredTip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash post-reconsider: %v", err)
	}
	if !restoredTip.IsEqual(tip) {
		t.Errorf("tip after reconsider = %s, want %s", restoredTip, tip)
	}
}

// TestRPC_Reorg_PreciousBlock_Linear pins that PreciousBlock against the
// current tip on a linear chain is a harmless no-op (no fork to choose
// between). Full fork-choice exercise lands in PR 11 (#80) once multi-node
// P2P is in place.
func TestRPC_Reorg_PreciousBlock_Linear(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt.Warp(3, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	if err := rt.PreciousBlock(tip); err != nil {
		t.Fatalf("PreciousBlock(tip) on linear chain: %v", err)
	}
}

// TestRPC_Reorg_NilHash pins the validation contract for the three reorg
// primitives.
func TestRPC_Reorg_NilHash(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.InvalidateBlock(nil); err == nil {
		t.Error("InvalidateBlock(nil) should error")
	}
	if err := rt.ReconsiderBlock(nil); err == nil {
		t.Error("ReconsiderBlock(nil) should error")
	}
	if err := rt.PreciousBlock(nil); err == nil {
		t.Error("PreciousBlock(nil) should error")
	}
}

// TestRPC_TestMempoolAccept_Valid asks bitcoind to validate a freshly-signed
// (but unbroadcast) tx. Allowed must be true and Fees must be populated.
func TestRPC_TestMempoolAccept_Valid(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	fromAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 from: %v", err)
	}
	toAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 to: %v", err)
	}
	if err := rt.Warp(101, fromAddr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build a skeleton tx with just the destination output; let
	// fundrawtransaction pick a mature UTXO from the wallet and add change.
	skelRaw, err := rt.rawRPC(ctx, "createrawtransaction",
		[]any{},
		map[string]any{toAddr: 0.5},
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
	rawBytes, err := hex.DecodeString(funded.Hex)
	if err != nil {
		t.Fatalf("decode funded hex: %v", err)
	}
	unsignedTx := wire.NewMsgTx(2)
	if err := unsignedTx.Deserialize(bytes.NewReader(rawBytes)); err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	signedTx, err := rt.SignRawTransactionWithWallet(unsignedTx)
	if err != nil {
		t.Fatalf("SignRawTransactionWithWallet: %v", err)
	}

	results, err := rt.TestMempoolAccept(signedTx)
	if err != nil {
		t.Fatalf("TestMempoolAccept: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Allowed {
		t.Fatalf("expected Allowed=true, got reject reason %q", r.RejectReason)
	}
	if r.TxID == "" {
		t.Error("TxID empty")
	}
	if r.VSize <= 0 {
		t.Errorf("VSize = %d, want > 0", r.VSize)
	}
	if r.Fees == nil {
		t.Error("Fees nil for Allowed tx")
	} else if r.Fees.Base <= 0 {
		t.Errorf("Fees.Base = %d, want > 0", r.Fees.Base)
	}
}

// TestRPC_TestMempoolAccept_Invalid verifies the rejection path: a tx whose
// inputs reference a nonexistent prevout must be rejected with a populated
// RejectReason.
func TestRPC_TestMempoolAccept_Invalid(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// Build a tx with a clearly nonexistent prevout (all-zero hash, vout 0)
	// and a single OP_RETURN output to keep parsing simple.
	bogus := wire.NewMsgTx(2)
	bogus.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{}, Index: 0},
		Sequence:         wire.MaxTxInSequenceNum,
	})
	// Minimal OP_RETURN output: scriptPubKey = 0x6a (OP_RETURN).
	bogus.AddTxOut(wire.NewTxOut(0, []byte{0x6a}))

	results, err := rt.TestMempoolAccept(bogus)
	if err != nil {
		t.Fatalf("TestMempoolAccept: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Allowed {
		t.Errorf("expected Allowed=false for bogus tx, got Allowed=true")
	}
	if r.RejectReason == "" {
		t.Error("expected non-empty RejectReason for rejected tx")
	}
	t.Logf("bogus tx rejected with reason: %s", r.RejectReason)
}

// TestRPC_TestMempoolAccept_Empty pins the validation contract that calling
// TestMempoolAccept with no transactions returns an error.
func TestRPC_TestMempoolAccept_Empty(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if _, err := rt.TestMempoolAccept(); err == nil {
		t.Fatal("expected error on empty input, got nil")
	}
}

// TestRPC_DeploymentStatus_Taproot verifies the typed DeploymentStatus
// wrapper against the well-known buried "taproot" deployment, which is
// always active on modern regtest from block 0.
func TestRPC_DeploymentStatus_Taproot(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	status, err := rt.DeploymentStatus("taproot")
	if err != nil {
		t.Fatalf("DeploymentStatus(taproot): %v", err)
	}
	if status != SoftForkActive {
		t.Errorf("taproot status = %v (%q), want SoftForkActive", status, status)
	}
}

// TestRPC_DeploymentStatus_Unknown pins the contract that an unrecognized
// deployment name returns ErrUnknownDeployment via errors.Is. Tests that
// target a not-yet-mainline soft-fork (APO, CTV, CSFS) rely on this signal
// to skip cleanly when run against mainline Core.
func TestRPC_DeploymentStatus_Unknown(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	_, err = rt.DeploymentStatus("definitely-not-a-real-deployment")
	if err == nil {
		t.Fatal("expected error for unknown deployment, got nil")
	}
	if !errors.Is(err, ErrUnknownDeployment) {
		t.Errorf("expected errors.Is(err, ErrUnknownDeployment), got %v", err)
	}
}

// TestRPC_WaitForDeployment_AlreadyActive exercises the unexported polling
// helper waitForDeployment with a target the deployment has already reached
// (taproot=active on a fresh regtest). The helper should return on the
// first poll without sleeping.
func TestRPC_WaitForDeployment_AlreadyActive(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rt.waitForDeployment(ctx, "taproot", SoftForkActive); err != nil {
		t.Fatalf("waitForDeployment: %v", err)
	}
}

// TestRPC_WaitForDeployment_Cancellation pins that ctx cancellation surfaces
// rather than spinning forever when the target status will not be reached.
func TestRPC_WaitForDeployment_Cancellation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// Wait for taproot=Defined — taproot is buried/active so this status
	// will never be reported. Cancel after a short wait and confirm the
	// helper surfaces ctx.Err().
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = rt.waitForDeployment(ctx, "taproot", SoftForkDefined)
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx error, got %v", err)
	}
}

// TestRPC_GetDeploymentInfo exercises the typed getdeploymentinfo wrapper.
// On a default regtest node we expect entries for the well-known buried
// deployments (taproot, segwit, csv) — taproot is active from block 0 on
// modern Core so its Active flag must be true.
//
// This test requires Bitcoin Core 24+ for the underlying RPC; on older
// builds it will report a clear failure pointing at that minimum version.
func TestRPC_GetDeploymentInfo(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	info, err := rt.GetDeploymentInfo()
	if err != nil {
		t.Fatalf("GetDeploymentInfo (requires Bitcoin Core 24+): %v", err)
	}
	if info.Hash == "" {
		t.Error("Hash empty")
	}
	if info.Deployments == nil {
		t.Fatal("Deployments map nil")
	}

	for _, name := range []string{"taproot", "segwit", "csv"} {
		d, ok := info.Deployments[name]
		if !ok {
			t.Errorf("missing deployment %q in %v", name, deploymentNames(info.Deployments))
			continue
		}
		if d.Type != "buried" && d.Type != "bip9" {
			t.Errorf("%s: unexpected Type %q", name, d.Type)
		}
	}

	if d, ok := info.Deployments["taproot"]; ok {
		if !d.Active {
			t.Errorf("taproot expected Active=true on modern regtest, got %+v", d)
		}
	}
}

func deploymentNames(m map[string]Deployment) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}

// TestRPC_ChainState_NilHash pins the validation contract that hash-taking
// chain wrappers reject nil rather than panicking.
func TestRPC_ChainState_NilHash(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if _, err := rt.GetBlock(nil); err == nil {
		t.Error("GetBlock(nil) should return validation error")
	}
	if _, err := rt.GetBlockVerbose(nil); err == nil {
		t.Error("GetBlockVerbose(nil) should return validation error")
	}
	if _, err := rt.GetBlockHeader(nil); err == nil {
		t.Error("GetBlockHeader(nil) should return validation error")
	}
}

// assembleTrivialRegtestBlock builds a minimum valid regtest block on top of
// tmpl: a single coinbase tx paying to OP_TRUE, with the witness commitment
// the template provided, then brute-force solves the (trivial) regtest PoW.
// On regtest the difficulty target is essentially MAX_HASH so the loop
// almost always solves at nonce=0.
func assembleTrivialRegtestBlock(t *testing.T, tmpl *btcjson.GetBlockTemplateResult) *wire.MsgBlock {
	t.Helper()

	prev, err := chainhash.NewHashFromStr(tmpl.PreviousHash)
	if err != nil {
		t.Fatalf("parse previous hash: %v", err)
	}
	bitsU64, err := strconv.ParseUint(tmpl.Bits, 16, 32)
	if err != nil {
		t.Fatalf("parse bits %q: %v", tmpl.Bits, err)
	}
	bits := uint32(bitsU64)
	if tmpl.CoinbaseValue == nil {
		t.Fatalf("template missing CoinbaseValue")
	}

	// Coinbase scriptSig: BIP34 height + extranonce.
	cbScript, err := txscript.NewScriptBuilder().
		AddInt64(tmpl.Height).
		AddInt64(0).
		Script()
	if err != nil {
		t.Fatalf("build coinbase script: %v", err)
	}
	coinbase := wire.NewMsgTx(2)
	coinbase.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{}, Index: 0xffffffff},
		SignatureScript:  cbScript,
		Sequence:         0xffffffff,
		Witness:          wire.TxWitness{make([]byte, 32)},
	})
	coinbase.AddTxOut(wire.NewTxOut(*tmpl.CoinbaseValue, []byte{txscript.OP_TRUE}))
	if tmpl.DefaultWitnessCommitment != "" {
		commitScript, err := hex.DecodeString(tmpl.DefaultWitnessCommitment)
		if err != nil {
			t.Fatalf("decode witness commitment: %v", err)
		}
		coinbase.AddTxOut(wire.NewTxOut(0, commitScript))
	}

	// With one tx in the block, merkle root = coinbase txid.
	merkleRoot := coinbase.TxHash()

	block := wire.NewMsgBlock(&wire.BlockHeader{
		Version:    tmpl.Version,
		PrevBlock:  *prev,
		MerkleRoot: merkleRoot,
		Timestamp:  time.Unix(tmpl.MinTime+1, 0),
		Bits:       bits,
	})
	block.AddTransaction(coinbase)

	target := blockchain.CompactToBig(bits)
	for nonce := uint32(0); nonce < (1 << 30); nonce++ {
		block.Header.Nonce = nonce
		h := block.Header.BlockHash()
		if blockchain.HashToBig(&h).Cmp(target) <= 0 {
			return block
		}
	}
	t.Fatal("could not solve regtest PoW within nonce range")
	return nil
}

// TestRPC_GetBlockTemplate_SubmitBlock pins the consensus-test path: assemble
// a block from the template, submit it, observe the height advance. This is
// the "include a tx in a block without going through the mempool" pattern
// soft-fork tests rely on.
func TestRPC_GetBlockTemplate_SubmitBlock(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// assembleTrivialRegtestBlock constructs a coinbase that BIP54 (Consensus
	// Cleanup, active on Inquisition) rejects with bad-cb-locktime. Skip until
	// the helper learns BIP54-correct coinbase assembly. PR2's SupportsBIP
	// will replace this Variant check with a BIP-aware skip.
	if v, _ := rt.Variant(); v == VariantInquisition {
		t.Skip("hand-rolled coinbase fails BIP54 cleanup rules on Inquisition")
	}

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)

	miner, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	if err := rt.Warp(101, miner); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	tmpl, err := rt.GetBlockTemplate(&btcjson.TemplateRequest{
		Mode:  "template",
		Rules: []string{"segwit"},
	})
	if err != nil {
		t.Fatalf("GetBlockTemplate: %v", err)
	}
	if tmpl.Height <= 0 || tmpl.PreviousHash == "" || tmpl.Bits == "" {
		t.Fatalf("template missing required fields: %+v", tmpl)
	}

	startHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if tmpl.Height != startHeight+1 {
		t.Fatalf("template height %d != current+1 (%d)", tmpl.Height, startHeight+1)
	}

	block := assembleTrivialRegtestBlock(t, tmpl)
	if err := rt.SubmitBlock(block); err != nil {
		t.Fatalf("SubmitBlock: %v", err)
	}

	endHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount after submit: %v", err)
	}
	if endHeight != startHeight+1 {
		t.Errorf("expected height %d -> %d, got %d", startHeight, startHeight+1, endHeight)
	}
}

// TestRPC_SubmitBlock_Invalid pins the error-path contract: bitcoind rejects
// a malformed block with a meaningful error rather than a panic. The empty
// block has no coinbase so it trips bitcoind's basic structural validation.
func TestRPC_SubmitBlock_Invalid(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	bogus := &wire.MsgBlock{Header: wire.BlockHeader{}}
	err = rt.SubmitBlock(bogus)
	if err == nil {
		t.Fatal("expected SubmitBlock(empty) to error, got nil")
	}
	if err := rt.SubmitBlock(nil); err == nil {
		t.Error("expected SubmitBlock(nil) to error, got nil")
	}
}

// TestRPC_CreateRawTransaction_DecodeRoundTrip pins the round-trip contract:
// the tx we build through CreateRawTransaction round-trips through
// DecodeRawTransaction with matching inputs and outputs.
func TestRPC_CreateRawTransaction_DecodeRoundTrip(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	addrStr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}
	addr, err := btcutil.DecodeAddress(addrStr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress: %v", err)
	}

	// Synthetic input — DecodeRawTransaction is a pure-decode RPC, so the
	// outpoint doesn't have to exist on chain.
	dummyTxid := "00000000000000000000000000000000000000000000000000000000deadbeef"
	inputs := []btcjson.TransactionInput{{Txid: dummyTxid, Vout: 7}}
	amounts := map[btcutil.Address]btcutil.Amount{addr: btcutil.Amount(50_000)}

	tx, err := rt.CreateRawTransaction(inputs, amounts, nil)
	if err != nil {
		t.Fatalf("CreateRawTransaction: %v", err)
	}
	if len(tx.TxIn) != 1 {
		t.Fatalf("expected 1 vin, got %d", len(tx.TxIn))
	}
	if tx.TxIn[0].PreviousOutPoint.Index != 7 {
		t.Errorf("vout = %d, want 7", tx.TxIn[0].PreviousOutPoint.Index)
	}
	if got := tx.TxIn[0].PreviousOutPoint.Hash.String(); got != dummyTxid {
		t.Errorf("vin txid = %s, want %s", got, dummyTxid)
	}
	if len(tx.TxOut) != 1 {
		t.Fatalf("expected 1 vout, got %d", len(tx.TxOut))
	}
	if tx.TxOut[0].Value != 50_000 {
		t.Errorf("vout value = %d, want 50000", tx.TxOut[0].Value)
	}

	res, err := rt.DecodeRawTransaction(tx)
	if err != nil {
		t.Fatalf("DecodeRawTransaction: %v", err)
	}
	if len(res.Vin) != 1 {
		t.Errorf("decoded vin len = %d, want 1", len(res.Vin))
	}
	if len(res.Vout) != 1 {
		t.Errorf("decoded vout len = %d, want 1", len(res.Vout))
	}
	if res.Vin[0].Txid != dummyTxid {
		t.Errorf("decoded vin txid = %s, want %s", res.Vin[0].Txid, dummyTxid)
	}
	if res.Vin[0].Vout != 7 {
		t.Errorf("decoded vin vout = %d, want 7", res.Vin[0].Vout)
	}
	if res.Vout[0].Value != 0.0005 { // 50000 sats = 0.0005 BTC
		t.Errorf("decoded vout value = %v, want 0.0005", res.Vout[0].Value)
	}
}

// TestRPC_DecodeRawTransaction_Nil pins the nil-input validation path.
func TestRPC_DecodeRawTransaction_Nil(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if _, err := rt.DecodeRawTransaction(nil); err == nil {
		t.Error("DecodeRawTransaction(nil) should return validation error")
	}
}

// TestRPC_DecodeScript_P2TR pins that DecodeScript returns the correct script
// type and disassembled ASM for a P2TR (Taproot) scriptPubKey. The script is
// fixed-shape: OP_1 <32-byte x-only pubkey>.
func TestRPC_DecodeScript_P2TR(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	taprootAddr, err := rt.GenerateBech32m(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32m: %v", err)
	}
	addr, err := btcutil.DecodeAddress(taprootAddr, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("PayToAddrScript: %v", err)
	}

	res, err := rt.DecodeScript(hex.EncodeToString(pkScript))
	if err != nil {
		t.Fatalf("DecodeScript: %v", err)
	}
	if res.Type != "witness_v1_taproot" {
		t.Errorf("script type = %q, want witness_v1_taproot", res.Type)
	}
	if res.Asm == "" {
		t.Error("Asm should be non-empty")
	}
}

// TestRPC_DecodeScript_Empty pins the empty-input validation path.
func TestRPC_DecodeScript_Empty(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if _, err := rt.DecodeScript(""); err == nil {
		t.Error("DecodeScript(\"\") should return validation error")
	}
}

// TestRPC_FundRawTransaction pins that FundRawTransaction can take an empty
// (output-only) tx and add inputs plus a change output drawn from the wallet's
// mature UTXOs. This is the bridge between CreateRawTransaction and the
// existing SignRawTransactionWithWallet → BroadcastTransaction flow.
func TestRPC_FundRawTransaction(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(userWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(userWallet)

	miner, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 miner: %v", err)
	}
	dest, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 dest: %v", err)
	}
	destAddr, err := btcutil.DecodeAddress(dest, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress: %v", err)
	}
	if err := rt.Warp(101, miner); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	pkScript, err := txscript.PayToAddrScript(destAddr)
	if err != nil {
		t.Fatalf("PayToAddrScript: %v", err)
	}
	skel := wire.NewMsgTx(2)
	skel.AddTxOut(wire.NewTxOut(50_000, pkScript))

	res, err := rt.FundRawTransaction(skel, nil)
	if err != nil {
		t.Fatalf("FundRawTransaction: %v", err)
	}
	if res.Transaction == nil {
		t.Fatal("Transaction is nil")
	}
	if len(res.Transaction.TxIn) == 0 {
		t.Error("expected at least one input added")
	}
	if len(res.Transaction.TxOut) < 2 {
		t.Errorf("expected at least 2 outputs (target + change), got %d", len(res.Transaction.TxOut))
	}
	if res.ChangePosition < 0 {
		t.Errorf("ChangePosition = %d, want >= 0", res.ChangePosition)
	}
	if res.Fee <= 0 {
		t.Errorf("Fee = %v, want > 0", res.Fee)
	}
}

// TestRPC_FundRawTransaction_Nil pins the nil-input validation path.
func TestRPC_FundRawTransaction_Nil(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if _, err := rt.FundRawTransaction(nil, nil); err == nil {
		t.Error("FundRawTransaction(nil) should return validation error")
	}
}

// TestRPC_Variant_Returns confirms Variant() resolves to a non-Unknown value
// against a running node. Either Core or Inquisition is acceptable — whichever
// the binary on PATH happens to be — but Unknown indicates a parse failure.
func TestRPC_Variant_Returns(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	v, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant: %v", err)
	}
	if v == VariantUnknown {
		t.Errorf("Variant should resolve to Core or Inquisition, got Unknown")
	}
	t.Logf("running against variant: %s", v)
}

// TestRPC_Variant_Cached confirms repeat calls return the same value without
// surfacing transient errors. Functional check — does not directly count RPC
// hits, but a cache miss on the second call would re-issue getnetworkinfo
// against an already-stopped node and fail.
func TestRPC_Variant_Cached(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	v1, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant (first): %v", err)
	}

	// Stop the node; a cache miss on the next call would fail.
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	v2, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant (cached): %v", err)
	}
	if v1 != v2 {
		t.Errorf("cached Variant changed: first=%s second=%s", v1, v2)
	}
}

// TestRPC_Variant_PreStart confirms Variant() returns errNotConnected before
// Start has been called.
func TestRPC_Variant_PreStart(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	if _, err := rt.Variant(); !errors.Is(err, errNotConnected) {
		t.Errorf("pre-Start Variant: want errNotConnected, got %v", err)
	}
}

// TestRPC_Variant_StringRoundTrip pins the human-readable enum strings.
func TestRPC_Variant_StringRoundTrip(t *testing.T) {
	cases := []struct {
		v    Variant
		want string
	}{
		{VariantUnknown, "unknown"},
		{VariantCore, "core"},
		{VariantInquisition, "inquisition"},
	}
	for _, tc := range cases {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("Variant(%d).String() = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// Test_ParseVariant pins the subversion → Variant mapping against the actual
// strings that Bitcoin Core and Bitcoin Inquisition 29.2 report. Lets the
// Inquisition path be exercised without a live Inquisition binary on PATH.
func Test_ParseVariant(t *testing.T) {
	cases := []struct {
		name       string
		subversion string
		want       Variant
	}{
		{"empty", "", VariantUnknown},
		{"core-29", "/Satoshi:29.0.0/", VariantCore},
		{"core-25", "/Satoshi:25.0.0/", VariantCore},
		{"inquisition-29.2-lowercase", "/Satoshi:29.2.0(inquisition)/", VariantInquisition},
		{"inquisition-titlecase", "/Satoshi:29.2.0(Inquisition)/", VariantInquisition},
		{"inquisition-uppercase", "/Satoshi:29.2.0(INQUISITION)/", VariantInquisition},
		{"plain-core-no-marker", "/Satoshi:29.0.0(custom)/", VariantCore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseVariant(tc.subversion); got != tc.want {
				t.Errorf("parseVariant(%q) = %s, want %s", tc.subversion, got, tc.want)
			}
		})
	}
}

// Test_BIPRegistry_Shape pins the integrity of bipRegistry: every BIPID
// constant has exactly one entry; deployment keys, BIPIDs, and BIP numbers
// are unique; no zero/empty leaks. Pure unit test — no node spawned.
func Test_BIPRegistry_Shape(t *testing.T) {
	wantIDs := []BIPID{
		BIPTestdummy, BIPTaproot,
		BIP54, BIP118, BIP119, BIP347, BIP348, BIP349,
	}

	seenID := make(map[BIPID]bool)
	seenDeployment := make(map[string]bool)
	seenBIPNumber := make(map[int]bool)

	for _, m := range bipRegistry {
		if m.id == BIPUnknown {
			t.Errorf("bipRegistry must not contain BIPUnknown")
		}
		if seenID[m.id] {
			t.Errorf("duplicate BIPID in registry: %d", m.id)
		}
		seenID[m.id] = true

		if m.deployment == "" {
			t.Errorf("registry entry %d has empty deployment key", m.id)
		}
		if seenDeployment[m.deployment] {
			t.Errorf("duplicate deployment key in registry: %q", m.deployment)
		}
		seenDeployment[m.deployment] = true

		if m.bipNumber > 0 {
			if seenBIPNumber[m.bipNumber] {
				t.Errorf("duplicate BIP number in registry: %d", m.bipNumber)
			}
			seenBIPNumber[m.bipNumber] = true
		}
		if m.name == "" {
			t.Errorf("registry entry %d has empty name", m.id)
		}
		if m.docURL == "" {
			t.Errorf("registry entry %d has empty docURL", m.id)
		}
	}

	for _, want := range wantIDs {
		if !seenID[want] {
			t.Errorf("BIPID %d (%s) missing from registry", want, want)
		}
	}
}

// Test_BIPID_String pins the human-readable name format for logging.
func Test_BIPID_String(t *testing.T) {
	cases := []struct {
		bip  BIPID
		want string
	}{
		{BIPUnknown, "BIPUnknown"},
		{BIPTestdummy, "testdummy"},
		{BIPTaproot, "BIP341"},
		{BIP54, "BIP54"},
		{BIP119, "BIP119"},
		{BIP347, "BIP347"},
		{BIPID(9999), "BIPUnknown"},
	}
	for _, tc := range cases {
		if got := tc.bip.String(); got != tc.want {
			t.Errorf("BIPID(%d).String() = %q, want %q", tc.bip, got, tc.want)
		}
	}
}

// Test_MetaLookups confirms metaByBIP and metaByDeployment round-trip for
// every registry entry, and return false for unknown lookups.
func Test_MetaLookups(t *testing.T) {
	for _, m := range bipRegistry {
		got, ok := metaByBIP(m.id)
		if !ok || got.deployment != m.deployment {
			t.Errorf("metaByBIP(%d) = (%v, %v), want (%+v, true)", m.id, got, ok, m)
		}
		got, ok = metaByDeployment(m.deployment)
		if !ok || got.id != m.id {
			t.Errorf("metaByDeployment(%q) = (%v, %v), want (%+v, true)", m.deployment, got, ok, m)
		}
	}
	if _, ok := metaByBIP(BIPUnknown); ok {
		t.Error("metaByBIP(BIPUnknown) should return false")
	}
	if _, ok := metaByDeployment("nonexistent-deployment"); ok {
		t.Error("metaByDeployment(nonexistent) should return false")
	}
}

// TestRPC_ListDeployments confirms the registry-joined view returns at least
// taproot (buried, both variants) plus the variant-specific deployments, and
// that registry metadata is populated for known keys.
func TestRPC_ListDeployments(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	deps, err := rt.ListDeployments()
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("expected at least one deployment, got 0")
	}

	// Sorted by Deployment for stable output.
	for i := 1; i < len(deps); i++ {
		if deps[i-1].Deployment > deps[i].Deployment {
			t.Errorf("deps not sorted: %q > %q", deps[i-1].Deployment, deps[i].Deployment)
		}
	}

	byKey := make(map[string]EnrichedDeployment, len(deps))
	for _, d := range deps {
		byKey[d.Deployment] = d
	}

	tap, ok := byKey["taproot"]
	if !ok {
		t.Fatal("expected 'taproot' in ListDeployments output")
	}
	if tap.BIP != BIPTaproot {
		t.Errorf("taproot BIP = %s, want BIPTaproot", tap.BIP)
	}
	if tap.BIPNumber != 341 {
		t.Errorf("taproot BIPNumber = %d, want 341", tap.BIPNumber)
	}
	if !tap.Active {
		t.Error("taproot should be Active on regtest")
	}
	if tap.DocURL == "" {
		t.Error("taproot should have a DocURL from registry join")
	}
}

// TestRPC_SupportsBIP_Testdummy confirms SupportsBIP returns true for a
// deployment that's always in regtest's getdeploymentinfo (testdummy on
// Core, testdummy as heretical on Inquisition).
func TestRPC_SupportsBIP_Testdummy(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	ok, err := rt.SupportsBIP(BIPTestdummy)
	if err != nil {
		t.Fatalf("SupportsBIP: %v", err)
	}
	if !ok {
		t.Error("SupportsBIP(BIPTestdummy) should be true on a regtest node")
	}
}

// TestRPC_SupportsBIP_VariantSpecific confirms variant-specific BIPs resolve
// correctly: BIP119 should be present on Inquisition and absent on Core.
// The test asserts whichever direction the running variant dictates.
func TestRPC_SupportsBIP_VariantSpecific(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	v, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant: %v", err)
	}

	gotCTV, err := rt.SupportsBIP(BIP119)
	if err != nil {
		t.Fatalf("SupportsBIP(BIP119): %v", err)
	}
	switch v {
	case VariantInquisition:
		if !gotCTV {
			t.Error("BIP119 should be present on Inquisition")
		}
	case VariantCore:
		if gotCTV {
			t.Error("BIP119 should be absent on stock Core")
		}
	default:
		t.Fatalf("unexpected variant: %s", v)
	}
}

// TestRPC_SupportsBIP_UnknownBIP pins the validation contract: passing a
// BIPID outside the registry returns ErrUnknownBIP without touching the
// node.
func TestRPC_SupportsBIP_UnknownBIP(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	_, err = rt.SupportsBIP(BIPUnknown)
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("SupportsBIP(BIPUnknown): want ErrUnknownBIP, got %v", err)
	}
	_, err = rt.SupportsBIP(BIPID(9999))
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("SupportsBIP(BIPID(9999)): want ErrUnknownBIP, got %v", err)
	}
}

// TestRPC_MineUntilActiveBIP_UnknownBIP confirms the early-exit contract for
// the typed wrapper: an out-of-registry BIPID returns ErrUnknownBIP without
// mining.
func TestRPC_MineUntilActiveBIP_UnknownBIP(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	_, err = rt.MineUntilActiveBIP(BIPID(9999), "addr", 100)
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("MineUntilActiveBIP(9999): want ErrUnknownBIP, got %v", err)
	}
}

// TestRPC_SetMockTime_RoundTrip confirms setmocktime takes effect: mining a
// fresh block under a mocktime far in the future advances getblockchaininfo's
// mediantime once enough blocks accumulate at that timestamp. Asserts the
// tip's block.time matches the mocked value within 1s.
func TestRPC_SetMockTime_RoundTrip(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	target := time.Now().Add(7 * 24 * time.Hour).Unix()
	if err := rt.SetMockTime(target); err != nil {
		t.Fatalf("SetMockTime: %v", err)
	}
	if err := rt.Warp(1, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	hash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	block, err := rt.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	got := block.Header.Timestamp.Unix()
	if delta := got - target; delta < -1 || delta > 1 {
		t.Errorf("tip block.time = %d, want %d ±1 (delta=%d)", got, target, delta)
	}
}

// TestRPC_SetMockTime_Validation pins the input bounds: ≤0 and >maxMockTime
// both reject without touching the node.
func TestRPC_SetMockTime_Validation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	cases := []struct {
		name string
		unix int64
	}{
		{"zero", 0},
		{"negative", -1},
		{"over-cap", maxMockTime + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rt.SetMockTime(tc.unix); err == nil {
				t.Errorf("SetMockTime(%d) should reject", tc.unix)
			}
		})
	}
}

// TestRPC_SetMockTime_PreStart pins that calling SetMockTime before Start
// returns errNotConnected (propagated through rawRPC).
func TestRPC_SetMockTime_PreStart(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	if err := rt.SetMockTime(time.Now().Unix()); !errors.Is(err, errNotConnected) {
		t.Errorf("pre-Start SetMockTime: want errNotConnected, got %v", err)
	}
}

// TestRPC_MineWithTimestamp confirms a single MineWithTimestamp call places
// the new tip's block.time at the requested timestamp (within 1s). This is
// the mid-level building block underneath WarpTime.
func TestRPC_MineWithTimestamp(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	target := time.Now().Add(30 * 24 * time.Hour).Unix()
	if err := rt.MineWithTimestamp(1, target, addr); err != nil {
		t.Fatalf("MineWithTimestamp: %v", err)
	}

	hash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	block, err := rt.GetBlock(hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	got := block.Header.Timestamp.Unix()
	if delta := got - target; delta < -1 || delta > 1 {
		t.Errorf("tip block.time = %d, want %d ±1 (delta=%d)", got, target, delta)
	}
}

// TestRPC_MineWithTimestamp_Validation pins the input bounds: blocks ≤ 0
// and empty miner reject before the RPC is issued.
func TestRPC_MineWithTimestamp_Validation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	if err := rt.MineWithTimestamp(0, time.Now().Unix(), "addr"); err == nil {
		t.Error("MineWithTimestamp(0, ...) should reject")
	}
	if err := rt.MineWithTimestamp(1, time.Now().Unix(), ""); err == nil {
		t.Error("MineWithTimestamp(_, _, \"\") should reject")
	}
}

// TestRPC_WarpTime confirms that advancing time by a duration drags MTP
// forward by approximately the same amount, observable via
// getblockchaininfo.mediantime. Tolerates 2s jitter.
func TestRPC_WarpTime(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet(minerWallet); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	defer rt.UnloadWallet(minerWallet)
	addr, err := rt.GenerateBech32(minerWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	// Mine enough blocks so MTP has a populated 11-block window before warp.
	if err := rt.Warp(11, addr); err != nil {
		t.Fatalf("Warp pre-fill: %v", err)
	}

	pre, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo pre: %v", err)
	}

	const advance = 48 * time.Hour
	newMTP, err := rt.WarpTime(advance, addr)
	if err != nil {
		t.Fatalf("WarpTime: %v", err)
	}

	wantMin := pre.MedianTime + int64(advance.Seconds()) - 2
	if newMTP < wantMin {
		t.Errorf("newMTP = %d, want >= %d (pre=%d advance=%s)",
			newMTP, wantMin, pre.MedianTime, advance)
	}

	// Round-trip: getblockchaininfo agrees with the returned newMTP.
	post, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo post: %v", err)
	}
	if post.MedianTime != newMTP {
		t.Errorf("getblockchaininfo.mediantime = %d, returned newMTP = %d", post.MedianTime, newMTP)
	}
}

// TestRPC_WarpTime_Validation pins duration > 0 and miner non-empty.
func TestRPC_WarpTime_Validation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	if _, err := rt.WarpTime(0, "addr"); err == nil {
		t.Error("WarpTime(0, ...) should reject")
	}
	if _, err := rt.WarpTime(-time.Hour, "addr"); err == nil {
		t.Error("WarpTime(<0, ...) should reject")
	}
	if _, err := rt.WarpTime(time.Hour, ""); err == nil {
		t.Error("WarpTime(_, \"\") should reject")
	}
}
