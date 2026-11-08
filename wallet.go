package regtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// rpcWalletAlreadyLoaded is Bitcoin Core's RPC_WALLET_ALREADY_LOADED, returned
// by loadwallet for a wallet that is already loaded. btcjson doesn't define it.
const rpcWalletAlreadyLoaded btcjson.RPCErrorCode = -35

// GetWalletInformation retrieves detailed information about the currently loaded wallet.
// This includes wallet name, balance, transaction count, and other metadata.
//
// Returns:
//   - *btcjson.GetWalletInfoResult: Detailed wallet information including:
//   - WalletName: Name of the loaded wallet
//   - WalletVersion: Version of the wallet format
//   - Balance: Total confirmed balance in BTC
//   - UnconfirmedBalance: Unconfirmed balance in BTC
//   - ImmatureBalance: Immature coinbase balance
//   - TxCount: Number of transactions in the wallet
//   - KeyPoolSize: Size of the key pool
//   - UnlockedUntil: Timestamp when wallet will be locked (0 if unlocked)
//   - PayTxFee: Transaction fee setting
//   - HdMasterKeyId: HD master key ID (if applicable)
//   - error: RPC error if no wallet is loaded or request fails
//
// Example:
//
//	info, err := rt.GetWalletInformation()
//	if err != nil {
//	    return fmt.Errorf("failed to get wallet info: %w", err)
//	}
//	fmt.Printf("Wallet: %s, Balance: %.8f BTC\n", info.WalletName, info.Balance)
func (r *Regtest) GetWalletInformation() (*btcjson.GetWalletInfoResult, error) {
	return r.GetWalletInformationContext(context.Background())
}

// GetWalletInformationContext is the context-aware variant of GetWalletInformation.
func (r *Regtest) GetWalletInformationContext(ctx context.Context) (*btcjson.GetWalletInfoResult, error) {
	return walletInfo(ctx, r)
}

func walletInfo(ctx context.Context, c caller) (*btcjson.GetWalletInfoResult, error) {
	info, err := call[btcjson.GetWalletInfoResult](ctx, c, "getwalletinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet info: %w", err)
	}
	return &info, nil
}

// CreateWallet creates a new Bitcoin wallet with the specified name.
// The wallet will be created in the Bitcoin node's wallet directory and
// will be automatically loaded after creation.
//
// Parameters:
//   - walletName: Unique name for the new wallet (must not already exist)
//
// Returns:
//   - *btcjson.CreateWalletResult: Result containing wallet creation details:
//   - Name: Name of the created wallet
//   - Warning: Any warnings from the creation process
//   - error: RPC error if wallet already exists or creation fails
//
// Example:
//
//	result, err := rt.CreateWallet("my_wallet")
//	if err != nil {
//	    return fmt.Errorf("failed to create wallet: %w", err)
//	}
//	fmt.Printf("Created wallet: %s\n", result.Name)
func (r *Regtest) CreateWallet(walletName string) (*btcjson.CreateWalletResult, error) {
	return r.CreateWalletContext(context.Background(), walletName)
}

// CreateWalletContext is the context-aware variant of CreateWallet.
func (r *Regtest) CreateWalletContext(ctx context.Context, walletName string) (*btcjson.CreateWalletResult, error) {
	result, err := call[btcjson.CreateWalletResult](ctx, r, "createwallet", walletName)
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}
	return &result, nil
}

// LoadWallet loads an existing wallet by name into the Bitcoin node.
// The wallet must exist in the node's wallet directory and not already be loaded.
//
// Parameters:
//   - walletName: Name of the wallet to load (must exist on disk)
//
// Returns:
//   - *btcjson.LoadWalletResult: Result containing wallet loading details:
//   - Name: Name of the loaded wallet
//   - Warning: Any warnings from the loading process
//   - error: RPC error if wallet doesn't exist, is already loaded, or loading fails
//
// Example:
//
//	result, err := rt.LoadWallet("existing_wallet")
//	if err != nil {
//	    return fmt.Errorf("failed to load wallet: %w", err)
//	}
//	fmt.Printf("Loaded wallet: %s\n", result.Name)
func (r *Regtest) LoadWallet(walletName string) (*btcjson.LoadWalletResult, error) {
	return r.LoadWalletContext(context.Background(), walletName)
}

// LoadWalletContext is the context-aware variant of LoadWallet.
func (r *Regtest) LoadWalletContext(ctx context.Context, walletName string) (*btcjson.LoadWalletResult, error) {
	result, err := call[btcjson.LoadWalletResult](ctx, r, "loadwallet", walletName)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}
	return &result, nil
}

// UnloadWallet unloads a currently loaded wallet from the Bitcoin node.
// The wallet remains on disk but is no longer accessible for operations.
// This is useful for switching between wallets or cleaning up resources.
//
// Parameters:
//   - walletName: Name of the wallet to unload (must be currently loaded)
//
// Returns:
//   - error: RPC error if wallet is not loaded or unloading fails
//
// Example:
//
//	err := rt.UnloadWallet("my_wallet")
//	if err != nil {
//	    return fmt.Errorf("failed to unload wallet: %w", err)
//	}
//	fmt.Println("Wallet unloaded successfully")
func (r *Regtest) UnloadWallet(walletName string) error {
	return r.UnloadWalletContext(context.Background(), walletName)
}

// UnloadWalletContext is the context-aware variant of UnloadWallet.
func (r *Regtest) UnloadWalletContext(ctx context.Context, walletName string) error {
	if _, err := r.rawRPC(ctx, "unloadwallet", walletName); err != nil {
		return fmt.Errorf("failed to unload wallet: %w", err)
	}
	return nil
}

// EnsureWallet ensures a wallet with the given name exists and is loaded.
// This is a convenience method that handles the common pattern of ensuring
// a wallet is available for operations, regardless of its current state.
//
// The method follows this logic:
//  1. Try to load the wallet (if it exists but isn't loaded)
//  2. If already loaded, return success
//  3. If loading fails, try to create a new wallet
//  4. If creation fails because wallet exists, try loading again
//
// Parameters:
//   - walletName: Name of the wallet to ensure is available
//
// Returns:
//   - error: Error if wallet cannot be created, loaded, or is in an invalid state
//
// This method is particularly useful for:
//   - Test setup where wallets may or may not exist
//   - Application initialization where wallet state is unknown
//   - Scripts that need to work with existing or new wallets
//
// Example:
//
//	err := rt.EnsureWallet("my_app_wallet")
//	if err != nil {
//	    return fmt.Errorf("failed to ensure wallet: %w", err)
//	}
//	// Wallet is now guaranteed to be loaded and ready for use
func (r *Regtest) EnsureWallet(walletName string) error {
	return r.EnsureWalletContext(context.Background(), walletName)
}

// EnsureWalletContext is the context-aware variant of EnsureWallet.
func (r *Regtest) EnsureWalletContext(ctx context.Context, walletName string) error {
	// First, try to load the wallet (in case it already exists).
	_, err := r.LoadWalletContext(ctx, walletName)
	if err == nil {
		return nil
	}
	if e := asRPCError(err); e != nil && e.Code == rpcWalletAlreadyLoaded {
		return nil
	}

	_, err = r.CreateWalletContext(ctx, walletName)
	if err != nil {
		// Core reports an existing wallet as the generic RPC_WALLET_ERROR
		// (-4, "... Database already exists."), so the message is the only
		// distinguishing signal.
		if e := asRPCError(err); e != nil && e.Code == btcjson.ErrRPCWallet &&
			strings.Contains(e.Message, "already exists") {
			_, loadErr := r.LoadWalletContext(ctx, walletName)
			if loadErr != nil {
				return fmt.Errorf("wallet exists but failed to load: %w", loadErr)
			}
			return nil
		}
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	return nil
}

// Wallet is a handle to one wallet on the node. Its methods send wallet RPCs
// to bitcoind's /wallet/<name> endpoint, so they work when several wallets
// are loaded. (The wallet methods on Regtest itself use the default endpoint,
// which bitcoind rejects once more than one wallet is loaded.)
//
// The wallet must already be loaded, for example with EnsureWallet. A Wallet
// stays valid across Stop and Start of its Regtest.
//
// Example:
//
//	_ = rt.EnsureWallet("alice")
//	_ = rt.EnsureWallet("bob")
//	alice, bob := rt.Wallet("alice"), rt.Wallet("bob")
//	addr, _ := alice.NewAddress("", regtest.AddressBech32)
//	_ = rt.Warp(101, addr)
//	bobAddr, _ := bob.NewAddress("", regtest.AddressBech32m)
//	txid, _ := alice.SendToAddress(bobAddr, 50_000_000)
type Wallet struct {
	rt   *Regtest
	name string
}

// Wallet returns a handle for the named wallet. It doesn't create or load
// the wallet and makes no RPC call.
func (r *Regtest) Wallet(name string) *Wallet {
	return &Wallet{rt: r, name: name}
}

// Name returns the wallet's name.
func (w *Wallet) Name() string { return w.name }

func (w *Wallet) rawRPC(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	client, err := w.rt.walletClient(w.name)
	if err != nil {
		return nil, err
	}
	raw, err := rawRequest(ctx, client, method, args...)
	if err != nil {
		return nil, fmt.Errorf("wallet %q: %w", w.name, err)
	}
	return raw, nil
}

// NewAddress returns a new address of the given type from this wallet.
func (w *Wallet) NewAddress(label string, addrType AddressType) (string, error) {
	return w.NewAddressContext(context.Background(), label, addrType)
}

// NewAddressContext is the context-aware variant of NewAddress.
func (w *Wallet) NewAddressContext(ctx context.Context, label string, addrType AddressType) (string, error) {
	return newAddress(ctx, w, label, addrType)
}

// SendToAddress sends sats to address from this wallet and returns the txid.
func (w *Wallet) SendToAddress(address string, sats int64) (*chainhash.Hash, error) {
	return w.SendToAddressContext(context.Background(), address, sats)
}

// SendToAddressContext is the context-aware variant of SendToAddress.
func (w *Wallet) SendToAddressContext(ctx context.Context, address string, sats int64) (*chainhash.Hash, error) {
	return sendToAddress(ctx, w, address, sats)
}

// Balance returns the wallet's trusted balance: confirmed funds plus its own
// unconfirmed change (getbalance).
func (w *Wallet) Balance() (btcutil.Amount, error) {
	return w.BalanceContext(context.Background())
}

// BalanceContext is the context-aware variant of Balance.
func (w *Wallet) BalanceContext(ctx context.Context) (btcutil.Amount, error) {
	btc, err := call[float64](ctx, w, "getbalance")
	if err != nil {
		return 0, fmt.Errorf("getbalance: %w", err)
	}
	amt, err := btcutil.NewAmount(btc)
	if err != nil {
		return 0, fmt.Errorf("getbalance: %w", err)
	}
	return amt, nil
}

// ListUnspent returns the wallet's unspent outputs with at least minConf
// confirmations. Use 0 to include unconfirmed outputs.
func (w *Wallet) ListUnspent(minConf int) ([]btcjson.ListUnspentResult, error) {
	return w.ListUnspentContext(context.Background(), minConf)
}

// ListUnspentContext is the context-aware variant of ListUnspent.
func (w *Wallet) ListUnspentContext(ctx context.Context, minConf int) ([]btcjson.ListUnspentResult, error) {
	if minConf < 0 {
		return nil, fmt.Errorf("minConf must be >= 0, got %d", minConf)
	}
	utxos, err := call[[]btcjson.ListUnspentResult](ctx, w, "listunspent", minConf)
	if err != nil {
		return nil, fmt.Errorf("listunspent: %w", err)
	}
	return utxos, nil
}

// Info returns the wallet's getwalletinfo result.
func (w *Wallet) Info() (*btcjson.GetWalletInfoResult, error) {
	return w.InfoContext(context.Background())
}

// InfoContext is the context-aware variant of Info.
func (w *Wallet) InfoContext(ctx context.Context) (*btcjson.GetWalletInfoResult, error) {
	return walletInfo(ctx, w)
}

// SignRawTransaction signs tx with this wallet's keys. It fails if the
// wallet can't sign every input.
func (w *Wallet) SignRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, error) {
	return w.SignRawTransactionContext(context.Background(), tx)
}

// SignRawTransactionContext is the context-aware variant of SignRawTransaction.
func (w *Wallet) SignRawTransactionContext(ctx context.Context, tx *wire.MsgTx) (*wire.MsgTx, error) {
	return signRawTx(ctx, w, tx)
}

// FundRawTransaction adds inputs from this wallet, and a change output, to
// tx. opts may be nil.
func (w *Wallet) FundRawTransaction(tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return w.FundRawTransactionContext(context.Background(), tx, opts)
}

// FundRawTransactionContext is the context-aware variant of FundRawTransaction.
func (w *Wallet) FundRawTransactionContext(ctx context.Context, tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return fundRawTx(ctx, w, tx, opts)
}

// faucetWallet is the wallet FundAddress mines to and spends from.
const faucetWallet = "go-regtest-faucet"

// FundAddress sends sats to address in a confirmed transaction and returns
// the new output. It needs no wallet setup: the coins come from an internal
// wallet ("go-regtest-faucet") that FundAddress loads, mines to as needed
// and unloads again, so the node is left with the wallets it had. Each call
// mines at least one block; the first one mines 101 or more.
//
// While FundAddress runs a second wallet may be loaded, so don't call the
// default-wallet methods on Regtest (GenerateBech32, SendToAddress, ...)
// from other goroutines at the same time. Wallet handles are unaffected.
//
// Example:
//
//	op, err := rt.FundAddress(addr, 1_000_000)
//	// op is a confirmed, spendable outpoint paying addr 0.01 BTC.
func (r *Regtest) FundAddress(address string, sats int64) (wire.OutPoint, error) {
	return r.FundAddressContext(context.Background(), address, sats)
}

// FundAddressContext is the context-aware variant of FundAddress.
func (r *Regtest) FundAddressContext(ctx context.Context, address string, sats int64) (wire.OutPoint, error) {
	if sats <= 0 {
		return wire.OutPoint{}, fmt.Errorf("sats must be > 0, got %d", sats)
	}
	dest, err := btcutil.DecodeAddress(address, &chaincfg.RegressionNetParams)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("decode address: %w", err)
	}
	script, err := txscript.PayToAddrScript(dest)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("address script: %w", err)
	}

	r.faucetMu.Lock()
	defer r.faucetMu.Unlock()
	if err := r.EnsureWalletContext(ctx, faucetWallet); err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
	}
	defer func() {
		// A fresh context: unload even if ctx has expired.
		unloadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.UnloadWalletContext(unloadCtx, faucetWallet)
	}()

	faucet := r.Wallet(faucetWallet)
	minerAddr, err := faucet.NewAddressContext(ctx, "", AddressBech32)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
	}
	if err := r.fillFaucet(ctx, faucet, minerAddr, btcutil.Amount(sats)); err != nil {
		return wire.OutPoint{}, err
	}

	txid, err := faucet.SendToAddressContext(ctx, address, sats)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
	}
	// Read the tx while it's in the mempool: right after the confirming
	// block, the -txindex lookup can lag behind the tip.
	tx, err := r.GetRawTransactionContext(ctx, txid)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
	}
	if err := r.WarpContext(ctx, 1, minerAddr); err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: confirm: %w", err)
	}
	for i, out := range tx.TxOut {
		if out.Value == sats && bytes.Equal(out.PkScript, script) {
			return wire.OutPoint{Hash: *txid, Index: uint32(i)}, nil
		}
	}
	return wire.OutPoint{}, fmt.Errorf("FundAddress: tx %s has no output paying %s", txid, address)
}

// fillFaucet mines to the faucet until its balance covers want plus a fee
// margin. Mining 101 blocks matures at least one coinbase; regtest halves
// the subsidy every 150 blocks, so a few rounds may be needed on long chains.
func (r *Regtest) fillFaucet(ctx context.Context, faucet *Wallet, minerAddr string, want btcutil.Amount) error {
	const feeMargin = btcutil.Amount(1_000_000)
	for round := 0; ; round++ {
		bal, err := faucet.BalanceContext(ctx)
		if err != nil {
			return fmt.Errorf("FundAddress: %w", err)
		}
		if bal >= want+feeMargin {
			return nil
		}
		if round == 4 {
			return fmt.Errorf("FundAddress: faucet holds %v after mining, need %v", bal, want+feeMargin)
		}
		if err := r.WarpContext(ctx, 101, minerAddr); err != nil {
			return fmt.Errorf("FundAddress: mine: %w", err)
		}
	}
}
