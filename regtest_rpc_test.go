package regtest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
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
