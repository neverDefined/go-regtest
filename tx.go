package regtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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

	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}

	txid, err := runWithContext(ctx, func() (*chainhash.Hash, error) {
		return client.SendToAddress(address, btcutil.Amount(sats))
	})
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
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	res, err := runWithContext(ctx, func() (*btcjson.GetTxOutResult, error) {
		return client.GetTxOut(txid, vout, includeMempool)
	})
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

	resp, err := r.rawRPC(ctx, "scantxoutset", "start", []string{descriptor})
	if err != nil {
		return nil, fmt.Errorf("scantxoutset failed: %w", err)
	}

	var result ScantxoutsetResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
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
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	txHex := hex.EncodeToString(buf.Bytes())

	resp, err := r.rawRPC(ctx, "signrawtransactionwithwallet", txHex)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	var result struct {
		Hex      string `json:"hex"`
		Complete bool   `json:"complete"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Complete {
		return nil, fmt.Errorf("transaction signing incomplete")
	}

	signedTxBytes, err := hex.DecodeString(result.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signed tx hex: %w", err)
	}

	var signedTx wire.MsgTx
	if err := signedTx.Deserialize(bytes.NewReader(signedTxBytes)); err != nil {
		return nil, fmt.Errorf("failed to deserialize signed tx: %w", err)
	}
	return &signedTx, nil
}

// BroadcastTransaction broadcasts a signed transaction to the Bitcoin network
// and returns the resulting transaction ID. See BroadcastTransactionContext
// for details on why the raw sendrawtransaction RPC is used.
func (r *Regtest) BroadcastTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	return r.BroadcastTransactionContext(context.Background(), tx)
}

// BroadcastTransactionContext is the context-aware variant of BroadcastTransaction.
//
// Uses the raw sendrawtransaction RPC rather than btcd's typed
// SendRawTransaction wrapper, which fails against Bitcoin Core 26+ because the
// "warnings" field changed from string to array.
func (r *Regtest) BroadcastTransactionContext(ctx context.Context, tx *wire.MsgTx) (*chainhash.Hash, error) {
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %w", err)
	}
	txHex := hex.EncodeToString(buf.Bytes())

	resp, err := r.rawRPC(ctx, "sendrawtransaction", txHex)
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	var txidStr string
	if err := json.Unmarshal(resp, &txidStr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal txid: %w", err)
	}

	txid, err := chainhash.NewHashFromStr(txidStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse txid: %w", err)
	}
	return txid, nil
}
