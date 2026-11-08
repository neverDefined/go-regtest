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

// ScantxoutsetUnspent is one output found by scantxoutset. Amount is in BTC.
type ScantxoutsetUnspent struct {
	TxID         string  `json:"txid"`
	Vout         uint32  `json:"vout"`
	ScriptPubKey string  `json:"scriptPubKey"`
	Desc         string  `json:"desc"`
	Amount       float64 `json:"amount"`
	Height       int64   `json:"height"`
}

// ScantxoutsetResult is the result of a scantxoutset call. TotalAmt is in
// BTC.
type ScantxoutsetResult struct {
	Success   bool                  `json:"success"`
	Searched  int                   `json:"searched_items"`
	Unspents  []ScantxoutsetUnspent `json:"unspents"`
	TotalAmt  float64               `json:"total_amount"`
	BestBlock string                `json:"bestblock"`
}

// SendToAddress sends sats satoshis to address from the default wallet (see
// Wallet) and returns the txid. The wallet picks the inputs and fee and
// broadcasts the transaction.
func (r *Regtest) SendToAddress(addressStr string, sats int64) (*chainhash.Hash, error) {
	return r.SendToAddressContext(context.Background(), addressStr, sats)
}

// SendToAddressContext is like SendToAddress but takes a context.
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

// GetTxOut returns the unspent output txid:vout, or nil with no error if it
// is spent or doesn't exist. includeMempool also takes the mempool into
// account. The result's Value is in BTC.
func (r *Regtest) GetTxOut(txid *chainhash.Hash, vout uint32, includeMempool bool) (*btcjson.GetTxOutResult, error) {
	return r.GetTxOutContext(context.Background(), txid, vout, includeMempool)
}

// GetTxOutContext is like GetTxOut but takes a context.
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

// ScanTxOutSetForAddress scans the whole UTXO set for outputs paying address.
// Unlike the wallet methods, it finds outputs to any address, not only the
// wallet's.
func (r *Regtest) ScanTxOutSetForAddress(address string) ([]ScantxoutsetUnspent, error) {
	return r.ScanTxOutSetForAddressContext(context.Background(), address)
}

// ScanTxOutSetForAddressContext is like ScanTxOutSetForAddress but takes a context.
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

// SignRawTransactionWithWallet signs tx with the default wallet's keys (see
// Wallet) and returns the signed copy. It fails if the wallet can't sign
// every input.
func (r *Regtest) SignRawTransactionWithWallet(tx *wire.MsgTx) (*wire.MsgTx, error) {
	return r.SignRawTransactionWithWalletContext(context.Background(), tx)
}

// SignRawTransactionWithWalletContext is like SignRawTransactionWithWallet
// but takes a context.
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

// BroadcastTransaction submits a signed transaction to the node's mempool
// with sendrawtransaction and returns its txid.
func (r *Regtest) BroadcastTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	return r.BroadcastTransactionContext(context.Background(), tx)
}

// BroadcastTransactionContext is like BroadcastTransaction but takes a context.
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

// CreateRawTransaction builds an unsigned transaction that spends inputs and
// pays amounts, without signing or broadcasting it. A nil lockTime leaves
// nLockTime at 0.
func (r *Regtest) CreateRawTransaction(inputs []btcjson.TransactionInput, amounts map[btcutil.Address]btcutil.Amount, lockTime *int64) (*wire.MsgTx, error) {
	return r.CreateRawTransactionContext(context.Background(), inputs, amounts, lockTime)
}

// CreateRawTransactionContext is like CreateRawTransaction but takes a context.
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

// DecodeRawTransaction returns bitcoind's decoding of tx (decoderawtransaction).
func (r *Regtest) DecodeRawTransaction(tx *wire.MsgTx) (*btcjson.TxRawResult, error) {
	return r.DecodeRawTransactionContext(context.Background(), tx)
}

// DecodeRawTransactionContext is like DecodeRawTransaction but takes a context.
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

// DecodeScript returns bitcoind's decoding of a hex-encoded script: its ASM,
// type (such as "witness_v1_taproot") and address, if any.
func (r *Regtest) DecodeScript(scriptHex string) (*btcjson.DecodeScriptResult, error) {
	return r.DecodeScriptContext(context.Background(), scriptHex)
}

// DecodeScriptContext is like DecodeScript but takes a context.
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

// FundRawTransaction adds inputs from the default wallet (see Wallet), and a
// change output if needed, so tx can be signed and broadcast. opts may be
// nil.
func (r *Regtest) FundRawTransaction(tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return r.FundRawTransactionContext(context.Background(), tx, opts)
}

// FundRawTransactionContext is like FundRawTransaction but takes a context.
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

// MempoolAcceptResult is TestMempoolAccept's result for one transaction.
type MempoolAcceptResult struct {
	// TxID is the transaction hash in hex.
	TxID string
	// Wtxid is the witness transaction hash in hex.
	Wtxid string
	// Allowed reports whether the tx would enter the mempool. If false,
	// RejectReason says why.
	Allowed bool
	// VSize is the virtual transaction size in vbytes (set when Allowed is true).
	VSize int64
	// Fees carries fee metadata when Allowed is true; nil when rejected.
	Fees *MempoolAcceptFees
	// RejectReason is bitcoind's reject string; empty when Allowed is true.
	// It tells policy failures such as "non-mandatory-script-verify-flag"
	// apart from consensus failures such as
	// "bad-txns-inputs-missingorspent".
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

// TestMempoolAccept reports whether bitcoind would accept txs to its mempool,
// without broadcasting them. It returns one result per tx, in order. Use
// RejectReason to tell a non-standard transaction from a consensus-invalid
// one. bitcoind's maximum fee rate check is skipped.
//
// At least one tx is required. Several txs are tested as a package: parents
// must come before children, and package limits apply.
func (r *Regtest) TestMempoolAccept(txs ...*wire.MsgTx) ([]MempoolAcceptResult, error) {
	return r.TestMempoolAcceptContext(context.Background(), txs...)
}

// TestMempoolAcceptContext is like TestMempoolAccept but takes a context.
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

// GetRawTransaction returns the transaction with the given txid. Confirmed
// transactions need -txindex, which Start enables.
func (r *Regtest) GetRawTransaction(txid *chainhash.Hash) (*wire.MsgTx, error) {
	return r.GetRawTransactionContext(context.Background(), txid)
}

// GetRawTransactionContext is like GetRawTransaction but takes a context.
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

// GetRawTransactionVerbose returns bitcoind's decoding of the transaction
// with the given txid.
func (r *Regtest) GetRawTransactionVerbose(txid *chainhash.Hash) (*btcjson.TxRawResult, error) {
	return r.GetRawTransactionVerboseContext(context.Background(), txid)
}

// GetRawTransactionVerboseContext is like GetRawTransactionVerbose but takes
// a context.
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

// GetRawMempoolContext is like GetRawMempool but takes a context.
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

// GetMempoolEntryContext is like GetMempoolEntry but takes a context.
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

// GetTxOutProof returns a merkle proof that txids are in a block. If
// blockHash is nil, bitcoind finds the block.
func (r *Regtest) GetTxOutProof(txids []*chainhash.Hash, blockHash *chainhash.Hash) ([]byte, error) {
	return r.GetTxOutProofContext(context.Background(), txids, blockHash)
}

// GetTxOutProofContext is like GetTxOutProof but takes a context.
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

// VerifyTxOutProofContext is like VerifyTxOutProof but takes a context.
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
