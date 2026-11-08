package regtest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// rpcWalletAlreadyLoaded is Bitcoin Core's RPC_WALLET_ALREADY_LOADED, returned
// by loadwallet for a wallet that is already loaded. btcjson doesn't define it.
const rpcWalletAlreadyLoaded btcjson.RPCErrorCode = -35

// GetWalletInformation returns getwalletinfo for the default wallet (see
// Wallet).
func (r *Regtest) GetWalletInformation() (*btcjson.GetWalletInfoResult, error) {
	return r.GetWalletInformationContext(context.Background())
}

// GetWalletInformationContext is like GetWalletInformation but takes a context.
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

// CreateWallet creates a wallet with the given name and loads it. It fails if
// the wallet already exists; EnsureWallet doesn't.
func (r *Regtest) CreateWallet(walletName string) (*btcjson.CreateWalletResult, error) {
	return r.CreateWalletContext(context.Background(), walletName)
}

// CreateWalletContext is like CreateWallet but takes a context.
func (r *Regtest) CreateWalletContext(ctx context.Context, walletName string) (*btcjson.CreateWalletResult, error) {
	result, err := call[btcjson.CreateWalletResult](ctx, r, "createwallet", walletName)
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}
	return &result, nil
}

// LoadWallet loads an existing wallet from the node's wallet directory. It
// fails if the wallet is already loaded.
func (r *Regtest) LoadWallet(walletName string) (*btcjson.LoadWalletResult, error) {
	return r.LoadWalletContext(context.Background(), walletName)
}

// LoadWalletContext is like LoadWallet but takes a context.
func (r *Regtest) LoadWalletContext(ctx context.Context, walletName string) (*btcjson.LoadWalletResult, error) {
	result, err := call[btcjson.LoadWalletResult](ctx, r, "loadwallet", walletName)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}
	return &result, nil
}

// UnloadWallet unloads a loaded wallet. The wallet stays on disk.
func (r *Regtest) UnloadWallet(walletName string) error {
	return r.UnloadWalletContext(context.Background(), walletName)
}

// UnloadWalletContext is like UnloadWallet but takes a context.
func (r *Regtest) UnloadWalletContext(ctx context.Context, walletName string) error {
	if _, err := r.rawRPC(ctx, "unloadwallet", walletName); err != nil {
		return fmt.Errorf("failed to unload wallet: %w", err)
	}
	return nil
}

// EnsureWallet makes sure the named wallet exists and is loaded: it loads the
// wallet, creating it first if it doesn't exist. A wallet that is already
// loaded is not an error.
func (r *Regtest) EnsureWallet(walletName string) error {
	return r.EnsureWalletContext(context.Background(), walletName)
}

// EnsureWalletContext is like EnsureWallet but takes a context.
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
// are loaded. The wallet methods on Regtest itself use the node's default
// wallet endpoint, which bitcoind rejects once more than one wallet is loaded.
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
//	_, _ = alice.SendToAddress(bobAddr, 50_000_000)
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

// NewAddressContext is like NewAddress but takes a context.
func (w *Wallet) NewAddressContext(ctx context.Context, label string, addrType AddressType) (string, error) {
	return newAddress(ctx, w, label, addrType)
}

// SendToAddress sends sats to address from this wallet and returns the txid.
func (w *Wallet) SendToAddress(address string, sats int64) (*chainhash.Hash, error) {
	return w.SendToAddressContext(context.Background(), address, sats)
}

// SendToAddressContext is like SendToAddress but takes a context.
func (w *Wallet) SendToAddressContext(ctx context.Context, address string, sats int64) (*chainhash.Hash, error) {
	return sendToAddress(ctx, w, address, sats)
}

// Balance returns the wallet's trusted balance: confirmed funds plus its own
// unconfirmed change (getbalance).
func (w *Wallet) Balance() (btcutil.Amount, error) {
	return w.BalanceContext(context.Background())
}

// BalanceContext is like Balance but takes a context.
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

// ListUnspentContext is like ListUnspent but takes a context.
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

// InfoContext is like Info but takes a context.
func (w *Wallet) InfoContext(ctx context.Context) (*btcjson.GetWalletInfoResult, error) {
	return walletInfo(ctx, w)
}

// SignRawTransaction signs tx with this wallet's keys. It fails if the
// wallet can't sign every input.
func (w *Wallet) SignRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, error) {
	return w.SignRawTransactionContext(context.Background(), tx)
}

// SignRawTransactionContext is like SignRawTransaction but takes a context.
func (w *Wallet) SignRawTransactionContext(ctx context.Context, tx *wire.MsgTx) (*wire.MsgTx, error) {
	return signRawTx(ctx, w, tx)
}

// FundRawTransaction funds a copy of tx from this wallet, adding inputs and,
// if needed, a change output. The copy is the result's Transaction; tx itself
// is not modified. A nil opts uses Bitcoin Core's defaults.
func (w *Wallet) FundRawTransaction(tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return w.FundRawTransactionContext(context.Background(), tx, opts)
}

// FundRawTransactionContext is like FundRawTransaction but takes a context.
func (w *Wallet) FundRawTransactionContext(ctx context.Context, tx *wire.MsgTx, opts *btcjson.FundRawTransactionOpts) (*btcjson.FundRawTransactionResult, error) {
	return fundRawTx(ctx, w, tx, opts)
}
