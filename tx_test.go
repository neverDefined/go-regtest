package regtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

func TestSendToAddress(t *testing.T) {
	t.Parallel()
	rt, fromAddr := fundedWallet(t, userWallet)

	toAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("failed to generate address: %v", err)
	}

	if err := rt.Warp(49, fromAddr); err != nil {
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

	if res == nil {
		t.Fatal("GetTxOut returned nil for mempool output")
	}
	if res.Value < 1 {
		t.Errorf("output value = %v, want >= 1 BTC", res.Value)
	}
}

func TestScanTxOutSetForAddress(t *testing.T) {
	t.Parallel()
	rt, minerAddr := fundedNode(t)

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

func TestSignRawTransactionWithWallet(t *testing.T) {
	t.Parallel()
	rt, fromAddr := fundedWallet(t, userWallet)

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

// TestTestMempoolAccept_Valid asks bitcoind to validate a freshly-signed
// (but unbroadcast) tx. Allowed must be true and Fees must be populated.
func TestTestMempoolAccept_Valid(t *testing.T) {
	t.Parallel()
	rt, _ := fundedWallet(t, userWallet)

	toAddr, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 to: %v", err)
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

// TestTestMempoolAccept_Invalid verifies the rejection path: a tx whose
// inputs reference a nonexistent prevout must be rejected with a populated
// RejectReason.
func TestTestMempoolAccept_Invalid(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

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

// TestCreateRawTransaction_DecodeRoundTrip pins the round-trip contract:
// the tx we build through CreateRawTransaction round-trips through
// DecodeRawTransaction with matching inputs and outputs.
func TestCreateRawTransaction_DecodeRoundTrip(t *testing.T) {
	t.Parallel()
	rt, addrStr := fundedWallet(t, userWallet)
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

// TestDecodeScript_P2TR pins that DecodeScript returns the correct script
// type and disassembled ASM for a P2TR (Taproot) scriptPubKey. The script is
// fixed-shape: OP_1 <32-byte x-only pubkey>.
func TestDecodeScript_P2TR(t *testing.T) {
	t.Parallel()
	rt, _ := fundedWallet(t, userWallet)

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

// TestFundRawTransaction pins that FundRawTransaction can take an empty
// (output-only) tx and add inputs plus a change output drawn from the wallet's
// mature UTXOs. This is the bridge between CreateRawTransaction and the
// existing SignRawTransactionWithWallet → BroadcastTransaction flow.
func TestFundRawTransaction(t *testing.T) {
	t.Parallel()
	rt, miner := fundedWallet(t, userWallet)

	dest, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32 dest: %v", err)
	}
	destAddr, err := btcutil.DecodeAddress(dest, &chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress: %v", err)
	}
	_ = miner

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

// TestMempoolAndTxProof exercises GetRawMempool, GetMempoolEntry,
// GetRawTransaction, GetRawTransactionVerbose, GetTxOutProof, and VerifyTxOutProof.
func TestMempoolAndTxProof(t *testing.T) {
	t.Parallel()
	rt, miner := fundedWallet(t, userWallet)

	recv, err := rt.GenerateBech32(userWallet)
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	mempoolBefore, err := rt.GetRawMempool()
	if err != nil {
		t.Fatalf("GetRawMempool (empty): %v", err)
	}
	if mempoolBefore == nil {
		t.Fatal("GetRawMempool: want non-nil slice on empty mempool")
	}
	if len(mempoolBefore) != 0 {
		t.Fatalf("expected empty mempool, got %d txs", len(mempoolBefore))
	}

	txid, err := rt.SendToAddress(recv, 100_000)
	if err != nil {
		t.Fatalf("SendToAddress: %v", err)
	}

	mempool, err := rt.GetRawMempool()
	if err != nil {
		t.Fatalf("GetRawMempool: %v", err)
	}
	found := false
	for _, h := range mempool {
		if h.IsEqual(txid) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("txid %s not in mempool: %v", txid, mempool)
	}

	entry, err := rt.GetMempoolEntry(txid)
	if err != nil {
		t.Fatalf("GetMempoolEntry: %v", err)
	}
	if entry.VSize <= 0 {
		t.Errorf("GetMempoolEntry vsize = %d, want > 0", entry.VSize)
	}
	if entry.Fees.Base <= 0 {
		t.Errorf("GetMempoolEntry fees.base = %v, want > 0", entry.Fees.Base)
	}

	tx, err := rt.GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("GetRawTransaction: %v", err)
	}
	txHash := tx.TxHash()
	if !txid.IsEqual(&txHash) {
		t.Errorf("GetRawTransaction TxHash = %s, want %s", txHash, txid)
	}

	if err := rt.Warp(1, miner); err != nil {
		t.Fatalf("Warp confirm: %v", err)
	}

	verbose, err := rt.GetRawTransactionVerbose(txid)
	if err != nil {
		t.Fatalf("GetRawTransactionVerbose: %v", err)
	}
	if verbose.Confirmations < 1 {
		t.Errorf("confirmations = %d, want >= 1", verbose.Confirmations)
	}

	proof, err := rt.GetTxOutProof([]*chainhash.Hash{txid}, nil)
	if err != nil {
		t.Fatalf("GetTxOutProof: %v", err)
	}
	if len(proof) == 0 {
		t.Fatal("GetTxOutProof returned empty proof")
	}

	verified, err := rt.VerifyTxOutProof(proof)
	if err != nil {
		t.Fatalf("VerifyTxOutProof: %v", err)
	}
	if len(verified) != 1 || !verified[0].IsEqual(txid) {
		t.Fatalf("VerifyTxOutProof = %v, want [%s]", verified, txid)
	}

	if _, err := rt.VerifyTxOutProof([]byte{0x01, 0x02, 0x03}); err == nil {
		t.Error("VerifyTxOutProof(malformed) should fail")
	}

	// After the proof's block leaves the active chain, bitcoind rejects it.
	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	if err := rt.InvalidateBlock(tip); err != nil {
		t.Fatalf("InvalidateBlock: %v", err)
	}
	if _, err := rt.VerifyTxOutProof(proof); err == nil {
		t.Error("VerifyTxOutProof for a block no longer in the active chain should fail")
	}
}

func TestTxValidation(t *testing.T) {
	preStart := unstarted(t)
	t.Cleanup(func() { _ = preStart.Cleanup() })

	const goodAddr = "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl"
	for _, tc := range []struct {
		name string
		addr string
		sats int64
	}{
		{"SendToAddress_zero_sats", goodAddr, 0},
		{"SendToAddress_negative_sats", goodAddr, -1},
		{"SendToAddress_empty_address", "", 1000},
		{"SendToAddress_malformed_address", "definitely-not-an-address", 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := preStart.SendToAddress(tc.addr, tc.sats)
			wantArgErr(t, fmt.Sprintf("SendToAddress(%q, %d)", tc.addr, tc.sats), err)
		})
	}

	_, err := preStart.SignRawTransactionWithWallet(nil)
	wantArgErr(t, "SignRawTransactionWithWallet(nil)", err)

	dummyHash := &chainhash.Hash{}
	proof := []byte{0x01}

	// Argument cases must fail before any RPC; notConnected cases pass valid
	// arguments and must reach the RPC layer.
	tests := []struct {
		name         string
		call         func(*Regtest) error
		notConnected bool
	}{
		{
			name: "DecodeRawTransaction_nil",
			call: func(r *Regtest) error {
				_, err := r.DecodeRawTransaction(nil)
				return err
			},
		},
		{
			name: "DecodeScript_empty",
			call: func(r *Regtest) error {
				_, err := r.DecodeScript("")
				return err
			},
		},
		{
			name: "FundRawTransaction_nil",
			call: func(r *Regtest) error {
				_, err := r.FundRawTransaction(nil, nil)
				return err
			},
		},
		{
			name: "TestMempoolAccept_empty",
			call: func(r *Regtest) error {
				_, err := r.TestMempoolAccept()
				return err
			},
		},
		{
			name: "GetRawTransaction nil txid",
			call: func(r *Regtest) error {
				_, err := r.GetRawTransaction(nil)
				return err
			},
		},
		{
			name: "GetRawTransactionVerbose nil txid",
			call: func(r *Regtest) error {
				_, err := r.GetRawTransactionVerbose(nil)
				return err
			},
		},
		{
			name: "GetMempoolEntry nil txid",
			call: func(r *Regtest) error {
				_, err := r.GetMempoolEntry(nil)
				return err
			},
		},
		{
			name: "GetTxOutProof empty txids",
			call: func(r *Regtest) error {
				_, err := r.GetTxOutProof(nil, nil)
				return err
			},
		},
		{
			name: "VerifyTxOutProof empty proof",
			call: func(r *Regtest) error {
				_, err := r.VerifyTxOutProof(nil)
				return err
			},
		},
		{
			name:         "GetRawTransaction not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.GetRawTransaction(dummyHash)
				return err
			},
		},
		{
			name:         "GetRawTransactionVerbose not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.GetRawTransactionVerbose(dummyHash)
				return err
			},
		},
		{
			name:         "GetRawMempool not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.GetRawMempool()
				return err
			},
		},
		{
			name:         "GetMempoolEntry not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.GetMempoolEntry(dummyHash)
				return err
			},
		},
		{
			name:         "GetTxOutProof not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.GetTxOutProof([]*chainhash.Hash{dummyHash}, nil)
				return err
			},
		},
		{
			name:         "VerifyTxOutProof not connected",
			notConnected: true,
			call: func(r *Regtest) error {
				_, err := r.VerifyTxOutProof(proof)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(preStart)
			if tc.notConnected {
				if !errors.Is(err, ErrNotConnected) {
					t.Errorf("want ErrNotConnected, got %v", err)
				}
				return
			}
			wantArgErr(t, tc.name, err)
		})
	}
}
