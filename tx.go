package regtest

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// ScantxoutsetUnspent represents an unspent output found by scantxoutset.
type ScantxoutsetUnspent struct {
	TxID         string  `json:"txid"`
	Vout         uint32  `json:"vout"`
	ScriptPubKey string  `json:"scriptPubKey"`
	Desc         string  `json:"desc"`
	Amount       float64 `json:"amount"`
	Height       int64   `json:"height"`
}

// ScantxoutsetResult represents the result of scantxoutset RPC call.
type ScantxoutsetResult struct {
	Success   bool                  `json:"success"`
	Searched  int                   `json:"searched_items"`
	Unspents  []ScantxoutsetUnspent `json:"unspents"`
	TotalAmt  float64               `json:"total_amount"`
	BestBlock string                `json:"bestblock"`
}

// SendToAddress sends the specified amount of satoshis to the given address.
// This creates and broadcasts a transaction from the loaded wallet's UTXOs
// to the destination address.
//
// Parameters:
//   - addressStr: Destination Bitcoin address (must be valid for regtest)
//   - sats: Amount to send in satoshis (must be > 0)
//
// Returns:
//   - *chainhash.Hash: Transaction ID of the created transaction
//   - error: Error if parameters are invalid, insufficient funds, or sending fails
//
// The transaction:
//   - Is automatically funded from available UTXOs in the wallet
//   - Uses the wallet's default fee rate
//   - Is immediately broadcast to the network
//   - Can be tracked using the returned transaction ID
//
// Example:
//
//	txid, err := rt.SendToAddress("bcrt1q...", 100000) // Send 0.001 BTC
//	if err != nil {
//	    return fmt.Errorf("failed to send transaction: %w", err)
//	}
//	fmt.Printf("Transaction sent: %s\n", txid.String())
func (r *Regtest) SendToAddress(addressStr string, sats int64) (*chainhash.Hash, error) {
	return r.SendToAddressContext(context.Background(), addressStr, sats)
}

// SendToAddressContext is the context-aware variant of SendToAddress.
func (r *Regtest) SendToAddressContext(ctx context.Context, addressStr string, sats int64) (*chainhash.Hash, error) {
	return sendToAddress(ctx, r, addressStr, sats)
}

func sendToAddress(ctx context.Context, c caller, addressStr string, sats int64) (*chainhash.Hash, error) {
	if sats <= 0 {
		return nil, fmt.Errorf("amount must be greater than 0")
	}
	if addressStr == "" {
		return nil, fmt.Errorf("address is empty")
	}

	address, err := btcutil.DecodeAddress(addressStr, &chaincfg.RegressionNetParams)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address: %w", err)
	}

	txid, err := callHash(ctx, c, "sendtoaddress", address.EncodeAddress(), btcutil.Amount(sats).ToBTC())
	if err != nil {
		return nil, fmt.Errorf("failed to send to address: %w", err)
	}
	return txid, nil
}

// GetTxOut retrieves information about a specific transaction output (UTXO).
// This is useful for checking if an output exists, is unspent, and getting
// details about its value and script.
//
// Parameters:
//   - txid: Transaction ID containing the output
//   - vout: Output index within the transaction (0-based)
//   - includeMempool: Whether to include unconfirmed transactions from mempool
//
// Returns:
//   - *btcjson.GetTxOutResult: Output information including:
//   - BestBlock: Hash of the block containing the transaction
//   - Confirmations: Number of confirmations
//   - Value: Output value in BTC
//   - ScriptPubKey: Output script details
//   - Coinbase: Whether this is a coinbase output
//   - error: RPC error if output doesn't exist or request fails
//
// Returns nil (without error) if the output is spent or doesn't exist.
//
// Example:
//
//	txid, _ := chainhash.NewHashFromStr("abc123...")
//	output, err := rt.GetTxOut(txid, 0, true)
//	if err != nil {
//	    return fmt.Errorf("failed to get output: %w", err)
//	}
//	if output != nil {
//	    fmt.Printf("Output value: %.8f BTC\n", output.Value)
//	} else {
//	    fmt.Println("Output is spent or doesn't exist")
//	}
func (r *Regtest) GetTxOut(txid *chainhash.Hash, vout uint32, includeMempool bool) (*btcjson.GetTxOutResult, error) {
	return r.GetTxOutContext(context.Background(), txid, vout, includeMempool)
}

// GetTxOutContext is the context-aware variant of GetTxOut.
func (r *Regtest) GetTxOutContext(ctx context.Context, txid *chainhash.Hash, vout uint32, includeMempool bool) (*btcjson.GetTxOutResult, error) {
	// A nil txid is sent as "" so bitcoind rejects it, rather than panicking.
	txidStr := ""
	if txid != nil {
		txidStr = txid.String()
	}
	// gettxout returns null for a spent or unknown output, which decodes to
	// a nil result.
	res, err := call[*btcjson.GetTxOutResult](ctx, r, "gettxout", txidStr, vout, includeMempool)
	if err != nil {
		return nil, fmt.Errorf("failed to get tx out: %w", err)
	}
	return res, nil
}

// ScanTxOutSetForAddress scans the entire UTXO set for outputs to a specific address.
// This operation searches through all unspent transaction outputs on the blockchain
// to find those belonging to the given address. Unlike wallet-based methods, this
// does not require the address to be imported into the wallet.
//
// Parameters:
//   - address: Bitcoin address to search for (any valid address format)
//
// Returns:
//   - []ScantxoutsetUnspent: List of unspent outputs including:
//   - TxID: Transaction ID containing the output
//   - Vout: Output index within the transaction
//   - Amount: Output value in BTC
//   - Height: Block height where output was created
//   - ScriptPubKey: Output script as hex string
//   - Desc: Output descriptor
//   - error: RPC error if scan fails
//
// This operation can be slow on large blockchains as it scans the entire UTXO set.
// For regtest and testing purposes, it provides a reliable way to detect deposits
// without wallet imports.
//
// Example:
//
//	utxos, err := rt.ScanTxOutSetForAddress("bcrt1p...")
//	if err != nil {
//	    return fmt.Errorf("failed to scan: %w", err)
//	}
//	for _, utxo := range utxos {
//	    fmt.Printf("Found: %s:%d with %.8f BTC\n", utxo.TxID, utxo.Vout, utxo.Amount)
//	}
func (r *Regtest) ScanTxOutSetForAddress(address string) ([]ScantxoutsetUnspent, error) {
	return r.ScanTxOutSetForAddressContext(context.Background(), address)
}

// ScanTxOutSetForAddressContext is the context-aware variant of ScanTxOutSetForAddress.
func (r *Regtest) ScanTxOutSetForAddressContext(ctx context.Context, address string) ([]ScantxoutsetUnspent, error) {
	descriptor := fmt.Sprintf("addr(%s)", address)

	result, err := call[ScantxoutsetResult](ctx, r, "scantxoutset", "start", []string{descriptor})
	if err != nil {
		return nil, fmt.Errorf("scantxoutset failed: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("scantxoutset was not successful")
	}

	return result.Unspents, nil
}

// SignRawTransactionWithWallet signs a raw transaction using keys from the loaded wallet.
// This is typically used to sign transactions created outside of Bitcoin Core.
//
// Parameters:
//   - tx: The unsigned transaction to sign
//
// Returns:
//   - *wire.MsgTx: The signed transaction
//   - error: Signing error if any
func (r *Regtest) SignRawTransactionWithWallet(tx *wire.MsgTx) (*wire.MsgTx, error) {
	return r.SignRawTransactionWithWalletContext(context.Background(), tx)
}

// SignRawTransactionWithWalletContext is the context-aware variant of
// SignRawTransactionWithWallet.
func (r *Regtest) SignRawTransactionWithWalletContext(ctx context.Context, tx *wire.MsgTx) (*wire.MsgTx, error) {
	return signRawTx(ctx, r, tx)
}

func signRawTx(ctx context.Context, c caller, tx *wire.MsgTx) (*wire.MsgTx, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	h, err := txHex(tx)
	if err != nil {
		return nil, err
	}

	type signResult struct {
		Hex      string `json:"hex"`
		Complete bool   `json:"complete"`
	}
	result, err := call[signResult](ctx, c, "signrawtransactionwithwallet", h)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if !result.Complete {
		return nil, fmt.Errorf("transaction signing incomplete")
	}

	signedTx, err := decodeTxHex(result.Hex)
	if err != nil {
		return nil, fmt.Errorf("signed tx: %w", err)
	}
	return signedTx, nil
}

// BroadcastTransaction broadcasts a signed transaction to the Bitcoin network
// and returns the resulting transaction ID.
func (r *Regtest) BroadcastTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	return r.BroadcastTransactionContext(context.Background(), tx)
}

// BroadcastTransactionContext is the context-aware variant of BroadcastTransaction.
func (r *Regtest) BroadcastTransactionContext(ctx context.Context, tx *wire.MsgTx) (*chainhash.Hash, error) {
	h, err := txHex(tx)
	if err != nil {
		return nil, err
	}
	txid, err := callHash(ctx, r, "sendrawtransaction", h)
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}
	return txid, nil
}

// CreateRawTransaction builds an unsigned transaction spending the given
// inputs and paying the given amounts. The raw counterpart to SendToAddress —
// returns the wire.MsgTx without signing or broadcasting, so the caller can
// inspect, mutate, or sign it externally before submission.
//
// Parameters:
//   - inputs: outpoints to spend (txid + vout per input).
//   - amounts: address → amount map for the outputs.
//   - lockTime: optional nLockTime; pass nil for none.
//
// Returns:
//   - *wire.MsgTx: the unsigned transaction
//   - error: ErrNotConnected before Start; otherwise wrapped RPC error.
//
// Example:
//
//	addr, _ := btcutil.DecodeAddress("bcrt1q...", &chaincfg.RegressionNetParams)
//	tx, err := rt.CreateRawTransaction(
//	    []btcjson.TransactionInput{{Txid: "abc...", Vout: 0}},
//	    map[btcutil.Address]btcutil.Amount{addr: btcutil.Amount(100_000)},
//	    nil,
//	)
func (r *Regtest) CreateRawTransaction(inputs []btcjson.TransactionInput, amounts map[btcutil.Address]btcutil.Amount, lockTime *int64) (*wire.MsgTx, error) {
	return r.CreateRawTransactionContext(context.Background(), inputs, amounts, lockTime)
}

// CreateRawTransactionContext is the context-aware variant of CreateRawTransaction.
func (r *Regtest) CreateRawTransactionContext(ctx context.Context, inputs []btcjson.TransactionInput, amounts map[btcutil.Address]btcutil.Amount, lockTime *int64) (*wire.MsgTx, error) {
	if inputs == nil {
		inputs = []btcjson.TransactionInput{} // [] rather than null
	}
	outputs := make(map[string]float64, len(amounts))
	for addr, amt := range amounts {
		outputs[addr.String()] = amt.ToBTC()
	}
	args := []any{inputs, outputs}
	if lockTime != nil {
		args = append(args, *lockTime)
	}
	h, err := call[string](ctx, r, "createrawtransaction", args...)
	if err != nil {
		return nil, fmt.Errorf("createrawtransaction: %w", err)
	}
	tx, err := decodeTxHex(h)
	if err != nil {
		return nil, fmt.Errorf("createrawtransaction: %w", err)
	}
	return tx, nil
}

// DecodeRawTransaction returns bitcoind's verbose decoding of a transaction:
// txid/wtxid, version, locktime, and per-input/output details.
//
// Parameters:
//   - tx: the transaction to decode (must be non-nil).
//
// Returns:
//   - *btcjson.TxRawResult: decoded view of the transaction
//   - error: validation error for nil tx; ErrNotConnected before Start;
//     otherwise wrapped RPC error.
//
// Example:
//
//	res, err := rt.DecodeRawTransaction(tx)
//	if err != nil { return err }
//	fmt.Println("vsize:", res.Vsize, "vin:", len(res.Vin))
func (r *Regtest) DecodeRawTransaction(tx *wire.MsgTx) (*btcjson.TxRawResult, error) {
	return r.DecodeRawTransactionContext(context.Background(), tx)
}

// DecodeRawTransactionContext is the context-aware variant of DecodeRawTransaction.
func (r *Regtest) DecodeRawTransactionContext(ctx context.Context, tx *wire.MsgTx) (*btcjson.TxRawResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	h, err := txHex(tx)
	if err != nil {
		return nil, err
	}
	res, err := call[btcjson.TxRawResult](ctx, r, "decoderawtransaction", h)
	if err != nil {
		return nil, fmt.Errorf("decoderawtransaction: %w", err)
	}
	return &res, nil
}

// DecodeScript returns bitcoind's interpretation of a serialized script:
// disassembled ASM, script type (e.g. "witness_v1_taproot"), and the
// derived address(es) when applicable.
//
// Parameters:
//   - scriptHex: serialized script as a hex string (must be non-empty).
//
// Returns:
//   - *btcjson.DecodeScriptResult: ASM, type, and addresses
//   - error: validation error for empty input; ErrNotConnected before Start;
//     otherwise wrapped RPC error.
//
// Example:
//
//	res, err := rt.DecodeScript("5120...") // a P2TR scriptPubKey
//	if err != nil { return err }
//	fmt.Println("type:", res.Type)
func (r *Regtest) DecodeScript(scriptHex string) (*btcjson.DecodeScriptResult, error) {
	return r.DecodeScriptContext(context.Background(), scriptHex)
}

// DecodeScriptContext is the context-aware variant of DecodeScript.
func (r *Regtest) DecodeScriptContext(ctx context.Context, scriptHex string) (*btcjson.DecodeScriptResult, error) {
	if scriptHex == "" {
		return nil, fmt.Errorf("scriptHex must not be empty")
	}
	scriptBytes, err := hex.DecodeString(scriptHex)
	if err != nil {
		return nil, fmt.Errorf("decode script hex: %w", err)
	}
	res, err := call[btcjson.DecodeScriptResult](ctx, r, "decodescript", hex.EncodeToString(scriptBytes))
	if err != nil {
		return nil, fmt.Errorf("decodescript: %w", err)
	}
	return &res, nil
}

// FundRawTransaction adds inputs and a change output to a transaction so it
// can be signed and broadcast. The wallet picks UTXOs from its mature balance.
//
// Parameters:
//   - tx: the partially-built transaction (outputs at minimum; inputs optional).
//   - opts: fund options; pass nil for defaults.
//
// Returns:
//   - *btcjson.FundRawTransactionResult: the funded tx, fee paid, and the
//     change-output index.
//   - error: validation error for nil tx; ErrNotConnected before Start;
//     otherwise wrapped RPC error (e.g. "Insufficient funds").
//
// Example:
//
//	out := wire.NewTxOut(50_000, p2trScript)
//	tx := wire.NewMsgTx(2); tx.AddTxOut(out)
//	res, err := rt.FundRawTransaction(tx, nil)
func (r *Regtest) FundRawTransaction(tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return r.FundRawTransactionContext(context.Background(), tx, opts)
}

// FundRawTransactionContext is the context-aware variant of FundRawTransaction.
func (r *Regtest) FundRawTransactionContext(ctx context.Context, tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return fundRawTx(ctx, r, tx, opts)
}

func fundRawTx(ctx context.Context, c caller, tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	o := btcjson.FundRawTransactionOpts{}
	if opts != nil {
		o = *opts
	}
	h, err := txHex(tx)
	if err != nil {
		return nil, err
	}
	// FundRawTransactionResult's UnmarshalJSON decodes the hex tx and the fee.
	res, err := call[btcjson.FundRawTransactionResult](ctx, c, "fundrawtransaction", h, o)
	if err != nil {
		return nil, fmt.Errorf("fundrawtransaction: %w", err)
	}
	return &res, nil
}

// MempoolAcceptResult is the per-tx result of TestMempoolAccept.
type MempoolAcceptResult struct {
	// TxID is the transaction hash in hex.
	TxID string
	// Wtxid is the witness transaction hash in hex.
	Wtxid string
	// Allowed reports whether the tx would enter the mempool. False means
	// either policy or consensus rejection — see RejectReason.
	Allowed bool
	// VSize is the virtual transaction size in vbytes (set when Allowed is true).
	VSize int64
	// Fees carries fee metadata when Allowed is true; nil when rejected.
	Fees *MempoolAcceptFees
	// RejectReason is bitcoind's policy or consensus reject string. Empty when
	// Allowed is true. This is the single most useful field for soft-fork
	// debugging — it distinguishes policy issues like "non-mandatory-script-
	// verify-flag" from consensus issues like "bad-txns-inputs-missingorspent".
	RejectReason string
	// PackageError is set when a multi-tx submission fails package validation.
	PackageError string
}

// MempoolAcceptFees is the fee section of MempoolAcceptResult.
type MempoolAcceptFees struct {
	// Base is the absolute transaction fee.
	Base btcutil.Amount
	// EffectiveFeeRate is the effective fee rate (BTC per KvB), accounting
	// for prioritisetransaction or package-feerate adjustments. Bitcoin
	// Core 25+; zero on older nodes.
	EffectiveFeeRate float64
	// EffectiveIncludes lists wtxids whose fees and vsizes contribute to
	// EffectiveFeeRate. Bitcoin Core 25+; nil on older nodes.
	EffectiveIncludes []string
}

// TestMempoolAccept asks bitcoind whether the given transactions would be
// accepted to the mempool, without broadcasting. The single most useful RPC
// for soft-fork debugging — RejectReason distinguishes policy from consensus
// rejections, so a test can tell "my APO sig is non-standard" apart from
// "my APO sig is consensus-invalid".
//
// At least one tx is required. When multiple txs are passed, parents must
// come before children and package policies apply (Bitcoin Core 24+).
//
// Parameters:
//   - txs: one or more *wire.MsgTx to test (at least one)
//
// Returns:
//   - []MempoolAcceptResult: one result per input tx, in the same order
//   - error: ErrNotConnected before Start; validation error for empty input;
//     otherwise wrapped RPC error.
//
// Example:
//
//	res, err := rt.TestMempoolAccept(myTx)
//	if err != nil {
//	    return err
//	}
//	if !res[0].Allowed {
//	    t.Fatalf("rejected: %s", res[0].RejectReason)
//	}
func (r *Regtest) TestMempoolAccept(txs ...*wire.MsgTx) ([]MempoolAcceptResult, error) {
	return r.TestMempoolAcceptContext(context.Background(), txs...)
}

// TestMempoolAcceptContext is the context-aware variant of TestMempoolAccept.
func (r *Regtest) TestMempoolAcceptContext(ctx context.Context, txs ...*wire.MsgTx) ([]MempoolAcceptResult, error) {
	if len(txs) == 0 {
		return nil, fmt.Errorf("at least one tx required")
	}
	hexes := make([]string, len(txs))
	for i, tx := range txs {
		h, err := txHex(tx)
		if err != nil {
			return nil, fmt.Errorf("testmempoolaccept: %w", err)
		}
		hexes[i] = h
	}
	// maxfeerate=0 disables bitcoind's fee-rate cap, so the result reflects
	// only policy and consensus checks.
	raw, err := call[[]*btcjson.TestMempoolAcceptResult](ctx, r, "testmempoolaccept", hexes, 0)
	if err != nil {
		return nil, fmt.Errorf("testmempoolaccept: %w", err)
	}
	out := make([]MempoolAcceptResult, len(raw))
	for i, e := range raw {
		out[i] = MempoolAcceptResult{
			TxID:         e.Txid,
			Wtxid:        e.Wtxid,
			Allowed:      e.Allowed,
			VSize:        int64(e.Vsize),
			RejectReason: e.RejectReason,
			PackageError: e.PackageError,
		}
		if e.Fees != nil {
			base, err := btcutil.NewAmount(e.Fees.Base)
			if err != nil {
				return nil, fmt.Errorf("converting fee base %v: %w", e.Fees.Base, err)
			}
			out[i].Fees = &MempoolAcceptFees{
				Base:              base,
				EffectiveFeeRate:  e.Fees.EffectiveFeeRate,
				EffectiveIncludes: append([]string(nil), e.Fees.EffectiveIncludes...),
			}
		}
	}
	return out, nil
}

// GetRawTransaction fetches a transaction by txid and returns the deserialized wire form.
// Confirmed transactions require -txindex (enabled by default in this fixture).
func (r *Regtest) GetRawTransaction(txid *chainhash.Hash) (*wire.MsgTx, error) {
	return r.GetRawTransactionContext(context.Background(), txid)
}

// GetRawTransactionContext is the context-aware variant of GetRawTransaction.
func (r *Regtest) GetRawTransactionContext(ctx context.Context, txid *chainhash.Hash) (*wire.MsgTx, error) {
	if txid == nil {
		return nil, fmt.Errorf("txid must not be nil")
	}
	h, err := call[string](ctx, r, "getrawtransaction", txid.String(), 0)
	if err != nil {
		return nil, fmt.Errorf("getrawtransaction: %w", err)
	}
	tx, err := decodeTxHex(h)
	if err != nil {
		return nil, fmt.Errorf("getrawtransaction: %w", err)
	}
	return tx, nil
}

// GetRawTransactionVerbose returns bitcoind's verbose decoding of a transaction by txid.
func (r *Regtest) GetRawTransactionVerbose(txid *chainhash.Hash) (*btcjson.TxRawResult, error) {
	return r.GetRawTransactionVerboseContext(context.Background(), txid)
}

// GetRawTransactionVerboseContext is the context-aware variant of GetRawTransactionVerbose.
func (r *Regtest) GetRawTransactionVerboseContext(ctx context.Context, txid *chainhash.Hash) (*btcjson.TxRawResult, error) {
	if txid == nil {
		return nil, fmt.Errorf("txid must not be nil")
	}
	res, err := call[btcjson.TxRawResult](ctx, r, "getrawtransaction", txid.String(), 1)
	if err != nil {
		return nil, fmt.Errorf("getrawtransaction: %w", err)
	}
	return &res, nil
}

// GetRawMempool returns the txids of all transactions in the node's mempool.
func (r *Regtest) GetRawMempool() ([]*chainhash.Hash, error) {
	return r.GetRawMempoolContext(context.Background())
}

// GetRawMempoolContext is the context-aware variant of GetRawMempool.
func (r *Regtest) GetRawMempoolContext(ctx context.Context) ([]*chainhash.Hash, error) {
	ids, err := call[[]string](ctx, r, "getrawmempool")
	if err != nil {
		return nil, fmt.Errorf("getrawmempool: %w", err)
	}
	return parseHashes("getrawmempool", ids)
}

// GetMempoolEntry returns mempool metadata for an unconfirmed transaction.
// Read fees from the nested Fees field (Fees.Base, Fees.Modified, ...): the
// legacy top-level Fee, ModifiedFee, AncestorFees, DescendantFees and Size
// fields are not sent by current Bitcoin Core and stay zero.
func (r *Regtest) GetMempoolEntry(txid *chainhash.Hash) (*btcjson.GetMempoolEntryResult, error) {
	return r.GetMempoolEntryContext(context.Background(), txid)
}

// GetMempoolEntryContext is the context-aware variant of GetMempoolEntry.
func (r *Regtest) GetMempoolEntryContext(ctx context.Context, txid *chainhash.Hash) (*btcjson.GetMempoolEntryResult, error) {
	if txid == nil {
		return nil, fmt.Errorf("txid must not be nil")
	}
	res, err := call[btcjson.GetMempoolEntryResult](ctx, r, "getmempoolentry", txid.String())
	if err != nil {
		return nil, fmt.Errorf("getmempoolentry: %w", err)
	}
	return &res, nil
}

// GetTxOutProof builds a merkle proof that the given transactions were included in a block.
// When blockHash is non-nil, the proof is limited to that block; otherwise bitcoind picks a suitable block.
func (r *Regtest) GetTxOutProof(txids []*chainhash.Hash, blockHash *chainhash.Hash) ([]byte, error) {
	return r.GetTxOutProofContext(context.Background(), txids, blockHash)
}

// GetTxOutProofContext is the context-aware variant of GetTxOutProof.
func (r *Regtest) GetTxOutProofContext(ctx context.Context, txids []*chainhash.Hash, blockHash *chainhash.Hash) ([]byte, error) {
	if len(txids) == 0 {
		return nil, fmt.Errorf("at least one txid required")
	}
	idStrs := make([]string, len(txids))
	for i, id := range txids {
		if id == nil {
			return nil, fmt.Errorf("txid must not be nil")
		}
		idStrs[i] = id.String()
	}
	args := []any{idStrs}
	if blockHash != nil {
		args = append(args, blockHash.String())
	}
	proofHex, err := call[string](ctx, r, "gettxoutproof", args...)
	if err != nil {
		return nil, fmt.Errorf("gettxoutproof: %w", err)
	}
	proof, err := hex.DecodeString(proofHex)
	if err != nil {
		return nil, fmt.Errorf("gettxoutproof: %w", err)
	}
	return proof, nil
}

// VerifyTxOutProof checks a merkle proof and returns the txids it commits to.
// It returns an empty slice when the proof's merkle data doesn't validate,
// and an error when bitcoind rejects the proof outright: malformed bytes, or
// a block that is not in the active chain (for example after a reorg).
func (r *Regtest) VerifyTxOutProof(proof []byte) ([]*chainhash.Hash, error) {
	return r.VerifyTxOutProofContext(context.Background(), proof)
}

// VerifyTxOutProofContext is the context-aware variant of VerifyTxOutProof.
func (r *Regtest) VerifyTxOutProofContext(ctx context.Context, proof []byte) ([]*chainhash.Hash, error) {
	if len(proof) == 0 {
		return nil, fmt.Errorf("proof must not be empty")
	}
	ids, err := call[[]string](ctx, r, "verifytxoutproof", hex.EncodeToString(proof))
	if err != nil {
		return nil, fmt.Errorf("verifytxoutproof: %w", err)
	}
	return parseHashes("verifytxoutproof", ids)
}

// parseHashes parses the txid strings an RPC returned.
func parseHashes(method string, ids []string) ([]*chainhash.Hash, error) {
	out := make([]*chainhash.Hash, 0, len(ids))
	for _, id := range ids {
		h, err := chainhash.NewHashFromStr(id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		out = append(out, h)
	}
	return out, nil
}
