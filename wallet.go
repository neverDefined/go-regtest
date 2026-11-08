package regtest

import (
	"context"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcjson"
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
	info, err := call[btcjson.GetWalletInfoResult](ctx, r, "getwalletinfo")
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
