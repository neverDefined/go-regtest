// Package regtest provides a lightweight Go library for managing Bitcoin Core
// regtest environments. See doc.go for detailed documentation.
package regtest

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
)

// errNotConnected is returned by RPC methods called before Start() or after Stop().
var errNotConnected = errors.New("RPC client not connected")

//go:embed scripts/bitcoind_manager.sh
var bitcoindManagerScript string

// =============================================================================
// TYPE DEFINITIONS
// =============================================================================

// Config holds the configuration for the Bitcoin regtest environment.
// It allows customization of RPC connection parameters and bitcoind settings.
type Config struct {
	// RPC connection settings
	Host string // RPC host:port (default: "127.0.0.1:18443")
	User string // RPC username (default: "user")
	Pass string // RPC password (default: "pass")

	// Bitcoin Core settings
	DataDir string // Data directory for bitcoind (default: "./bitcoind_regtest")

	// Additional bitcoind arguments (optional)
	// Example: []string{"-txindex=1", "-fallbackfee=0.0001"}
	ExtraArgs []string
}

// Regtest manages a Bitcoin regtest node instance.
// Each instance can run independently with its own configuration.
// This design allows multiple regtest nodes to run simultaneously
// on different ports with different configurations.
type Regtest struct {
	config       *Config
	scriptPath   string
	scriptTmpDir string // Directory containing the temporary script file
	mu           sync.Mutex
	client       *rpcclient.Client
	clientMu     sync.RWMutex
}

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

// =============================================================================
// CONSTRUCTOR
// =============================================================================

// New creates a new Regtest instance with the provided configuration.
// If config is nil, default configuration values are used.
//
// The initialization process:
//  1. Checks if bitcoind is installed and available in PATH
//  2. Gets the current working directory
//  3. Walks up the directory tree looking for go.mod
//  4. Constructs the script path as scripts/bitcoind_manager.sh
//  5. Verifies the script exists and is accessible
//
// Parameters:
//   - config: Configuration for the regtest node (nil for defaults)
//
// Returns:
//   - *Regtest: A new Regtest instance
//   - error: Detailed error if initialization fails
//
// Example:
//
//	rt, err := regtest.New(nil) // Use defaults
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer rt.Stop()
//	err = rt.Start()
func New(config *Config) (*Regtest, error) {
	rt := &Regtest{}

	// Use default config if none provided
	if config == nil {
		rt.config = DefaultConfig()
	} else {
		// Store a copy to prevent external modifications
		rt.config = &Config{
			Host:      config.Host,
			User:      config.User,
			Pass:      config.Pass,
			DataDir:   config.DataDir,
			ExtraArgs: append([]string(nil), config.ExtraArgs...),
		}
	}

	// Initialize immediately
	if err := rt.initialize(); err != nil {
		return nil, err
	}

	return rt, nil
}

// =============================================================================
// CONFIGURATION
// =============================================================================

// DefaultConfig returns a new Config with default regtest settings.
// These are the standard settings for running a local Bitcoin regtest node.
//
// Returns:
//   - *Config: A new config with default values
//
// Default values:
//   - Host: "127.0.0.1:18443" (standard regtest RPC port)
//   - User: "user" (default RPC username)
//   - Pass: "pass" (default RPC password)
//   - DataDir: "./bitcoind_regtest" (local data directory)
//   - ExtraArgs: nil (no additional arguments)
func DefaultConfig() *Config {
	return &Config{
		Host:      "127.0.0.1:18443",
		User:      "user",
		Pass:      "pass",
		DataDir:   "./bitcoind_regtest",
		ExtraArgs: nil,
	}
}

// Config returns a copy of this instance's configuration.
// This prevents external modifications to the internal config.
//
// Returns:
//   - *Config: A copy of the configuration
func (r *Regtest) Config() *Config {
	return &Config{
		Host:      r.config.Host,
		User:      r.config.User,
		Pass:      r.config.Pass,
		DataDir:   r.config.DataDir,
		ExtraArgs: append([]string(nil), r.config.ExtraArgs...),
	}
}

// RPCConfig returns an RPC client configuration for connecting to this regtest node.
// This uses the configuration provided when creating the Regtest instance.
//
// Returns:
//   - *rpcclient.ConnConfig: Connection configuration for this regtest node
//
// Example:
//
//	rt, _ := regtest.New(nil)
//	rt.Start()
//	client, _ := rpcclient.New(rt.RPCConfig(), nil)
func (r *Regtest) RPCConfig() *rpcclient.ConnConfig {
	return &rpcclient.ConnConfig{
		Host:         r.config.Host,
		User:         r.config.User,
		Pass:         r.config.Pass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}
}

// =============================================================================
// LIFECYCLE MANAGEMENT
// =============================================================================

// Start starts the Bitcoin regtest node using the bitcoind manager script.
// This is a convenience wrapper around StartContext that uses context.Background().
// For cancellable operations, use StartContext instead.
//
// Returns:
//   - error: Detailed error if startup fails
//
// Example:
//
//	rt, _ := regtest.New(nil)
//	err := rt.Start()
//	if err != nil {
//	    log.Fatalf("Failed to start Bitcoin node: %v", err)
//	}
//	defer rt.Stop() // Always clean up
func (r *Regtest) Start() error {
	return r.StartContext(context.Background())
}

// StartContext starts the Bitcoin regtest node using the bitcoind manager script.
// This method is thread-safe and will prevent multiple simultaneous start attempts.
// The operation can be cancelled using the provided context.
//
// The function:
//   - Executes the bitcoind manager script with the "start" command
//   - Returns detailed error information if startup fails
//   - Uses mutex locking to prevent race conditions
//   - Respects context cancellation
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//
// Returns:
//   - error: Detailed error if startup fails or context is cancelled
//
// The started node will:
//   - Run on the regtest network
//   - Use this instance's configuration
//   - Be accessible via RPC on the configured port
//   - Create necessary data directories
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	err := rt.StartContext(ctx)
//	if err != nil {
//	    log.Fatalf("Failed to start Bitcoin node: %v", err)
//	}
//	defer rt.Stop()
func (r *Regtest) StartContext(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	port := r.extractPort()

	// Pass config parameters to script: start datadir port user pass
	cmd := exec.CommandContext(ctx, "bash", r.scriptPath, "start", r.config.DataDir, port, r.config.User, r.config.Pass)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("start cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("failed to start bitcoind (script: %s): %s", r.scriptPath, string(output))
	}

	// Now that node is started, create RPC client
	return r.connectClient()
}

// Stop stops the Bitcoin regtest node and performs cleanup.
// This method is thread-safe and should be called to properly shut down
// the Bitcoin node and clean up resources.
//
// The function:
//   - Sends a stop signal to the running bitcoind process
//   - Waits for the process to terminate gracefully
//   - Cleans up data directories and temporary files
//   - Removes temporary script directory
//   - Uses mutex locking to prevent race conditions
//
// Returns:
//   - error: Detailed error if the stop process fails
//
// It's recommended to always call this method in defer statements
// to ensure proper cleanup, even if the program exits unexpectedly.
//
// Example:
//
//	rt, _ := regtest.New(nil)
//	err := rt.Start()
//	if err != nil {
//	    return err
//	}
//	defer rt.Stop() // Ensures cleanup
func (r *Regtest) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Shutdown RPC client if it exists
	r.clientMu.Lock()
	if r.client != nil {
		r.client.Shutdown()
		r.client = nil
	}
	r.clientMu.Unlock()

	port := r.extractPort()

	// Pass config parameters to script: stop datadir port user pass
	cmd := exec.Command("bash", r.scriptPath, "stop", r.config.DataDir, port, r.config.User, r.config.Pass)
	output, err := cmd.CombinedOutput()

	// Note: The temporary script dir is cleaned up by Cleanup().

	if err != nil {
		return fmt.Errorf("failed to stop bitcoind: %s", string(output))
	}

	return nil
}

// Cleanup removes temporary files and directories created by this Regtest instance.
// This should be called when you're completely done with the instance and won't
// need to check its status anymore. It's safe to call this multiple times.
//
// Note: Stop() does not automatically call Cleanup() to allow status checks after
// stopping. You should call Cleanup() explicitly when you're done with the instance.
//
// Example:
//
//	rt, _ := regtest.New(nil)
//	rt.Start()
//	// ... use regtest ...
//	rt.Stop()
//	rt.Cleanup() // Clean up temp files
func (r *Regtest) Cleanup() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.scriptTmpDir != "" {
		if err := os.RemoveAll(r.scriptTmpDir); err != nil {
			return fmt.Errorf("failed to clean up temp directory: %w", err)
		}
		r.scriptTmpDir = ""
		r.scriptPath = ""
	}
	return nil
}

// IsRunning checks if the Bitcoin regtest node is currently running by
// attempting a short-timeout RPC call against the configured host. It does not
// depend on the embedded manager script, so it remains valid after Cleanup().
//
// Returns true if the node responds to a getblockcount call within ~2 seconds.
// Returns false (with nil error) if the connection is refused or times out —
// the typical "not running" signals. Other RPC errors (auth failures, malformed
// responses) are propagated.
//
// Example:
//
//	running, err := rt.IsRunning()
//	if err != nil {
//	    return fmt.Errorf("failed to check node status: %w", err)
//	}
//	if !running {
//	    if err := rt.Start(); err != nil {
//	        return fmt.Errorf("failed to start node: %w", err)
//	    }
//	}
func (r *Regtest) IsRunning() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.IsRunningContext(ctx)
}

// IsRunningContext is the context-aware variant of IsRunning. The supplied ctx
// bounds how long this call will wait for the node to respond.
func (r *Regtest) IsRunningContext(ctx context.Context) (bool, error) {
	// Use the live client if Start() has been called; otherwise build an
	// ephemeral one so callers can probe the node before / after lifecycle calls.
	client, err := r.lockedClient()
	if err != nil {
		ephemeral, newErr := rpcclient.New(r.RPCConfig(), nil)
		if newErr != nil {
			return false, fmt.Errorf("failed to create RPC client for status check: %w", newErr)
		}
		defer ephemeral.Shutdown()
		client = ephemeral
	}

	_, err = runWithContext(ctx, func() (int64, error) {
		return client.GetBlockCount()
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// Treat "no response in time" as not running, but only when the caller
		// did not pass a pre-cancelled context. If the parent context was
		// cancelled before we started, surface that to the caller.
		if ctx.Err() != nil && ctx.Err() == context.Canceled {
			return false, ctx.Err()
		}
		return false, nil
	}
	if isConnRefusedErr(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check bitcoind status: %w", err)
}

// isConnRefusedErr returns true when err looks like a TCP connection refusal
// or similar "nobody is listening" condition, which we treat as "not running".
func isConnRefusedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF")
}

// =============================================================================
// RPC CLIENT HELPERS
// =============================================================================

// Client returns the RPC client for the Regtest instance.
// For advanced users that want to use the RPC client directly.
//
// Returns:
//   - *rpcclient.Client: The RPC client instance, or nil if not connected
func (r *Regtest) Client() *rpcclient.Client {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

// ---------------------------------------------------------------
//  Basic Operations
// ---------------------------------------------------------------

// GetBlockCount returns the current block count.
func (r *Regtest) GetBlockCount() (int64, error) {
	return r.GetBlockCountContext(context.Background())
}

// GetBlockCountContext is the context-aware variant of GetBlockCount.
func (r *Regtest) GetBlockCountContext(ctx context.Context) (int64, error) {
	client, err := r.lockedClient()
	if err != nil {
		return 0, err
	}
	return runWithContext(ctx, func() (int64, error) {
		return client.GetBlockCount()
	})
}

// HealthCheck performs a health check by getting the block count.
func (r *Regtest) HealthCheck() error {
	return r.HealthCheckContext(context.Background())
}

// HealthCheckContext is the context-aware variant of HealthCheck.
func (r *Regtest) HealthCheckContext(ctx context.Context) error {
	if _, err := r.GetBlockCountContext(ctx); err != nil {
		return fmt.Errorf("failed to get block count (health check): %w", err)
	}
	return nil
}

// ---------------------------------------------------------------
//  Wallet Management
// ---------------------------------------------------------------

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
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	info, err := runWithContext(ctx, func() (*btcjson.GetWalletInfoResult, error) {
		return client.GetWalletInfo()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet info: %w", err)
	}
	return info, nil
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
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	result, err := runWithContext(ctx, func() (*btcjson.CreateWalletResult, error) {
		return client.CreateWallet(walletName)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}
	return result, nil
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
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	result, err := runWithContext(ctx, func() (*btcjson.LoadWalletResult, error) {
		return client.LoadWallet(walletName)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}
	return result, nil
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
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.UnloadWallet(&walletName)
	})
	if err != nil {
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

	if strings.Contains(err.Error(), "already loaded") ||
		strings.Contains(err.Error(), "already exists") {
		return nil
	}

	_, err = r.CreateWalletContext(ctx, walletName)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
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

// ---------------------------------------------------------------
// Address Management
// ---------------------------------------------------------------

// GenerateBech32 generates a new Bech32 (native SegWit) address for the given label.
// Bech32 addresses start with "bc1" on mainnet or "bcrt1" on regtest and provide
// better error detection and lower transaction fees compared to legacy addresses.
//
// Parameters:
//   - labelStr: Human-readable label for the address (used for organization)
//
// Returns:
//   - string: A new Bech32 address (e.g., "bcrt1q...")
//   - error: RPC error if address generation fails or no wallet is loaded
//
// The generated address:
//   - Is derived from the wallet's HD seed
//   - Can be used for receiving Bitcoin payments
//   - Supports SegWit transactions with lower fees
//   - Has better error detection than legacy addresses
//
// Example:
//
//	address, err := rt.GenerateBech32("my_receiving_address")
//	if err != nil {
//	    return fmt.Errorf("failed to generate address: %w", err)
//	}
//	fmt.Printf("Generated Bech32 address: %s\n", address)
func (r *Regtest) GenerateBech32(labelStr string) (string, error) {
	return r.GenerateBech32Context(context.Background(), labelStr)
}

// GenerateBech32Context is the context-aware variant of GenerateBech32.
func (r *Regtest) GenerateBech32Context(ctx context.Context, labelStr string) (string, error) {
	return r.generateAddress(ctx, labelStr, "bech32")
}

// GenerateBech32m generates a new Bech32m (Taproot) address for the given label.
// Bech32m addresses are used for Taproot outputs and provide enhanced privacy
// and efficiency through the Taproot upgrade. They start with "bc1p" on mainnet
// or "bcrt1p" on regtest.
//
// Parameters:
//   - labelStr: Human-readable label for the address (used for organization)
//
// Returns:
//   - string: A new Bech32m Taproot address (e.g., "bcrt1p...")
//   - error: RPC error if address generation fails or no wallet is loaded
//
// The generated address:
//   - Is a Taproot address supporting advanced scripting
//   - Provides better privacy through key aggregation
//   - Enables complex smart contract functionality
//   - Has the same error detection as Bech32 but with different checksum
//
// Example:
//
//	address, err := rt.GenerateBech32m("my_taproot_address")
//	if err != nil {
//	    return fmt.Errorf("failed to generate Taproot address: %w", err)
//	}
//	fmt.Printf("Generated Bech32m address: %s\n", address)
func (r *Regtest) GenerateBech32m(labelStr string) (string, error) {
	return r.GenerateBech32mContext(context.Background(), labelStr)
}

// GenerateBech32mContext is the context-aware variant of GenerateBech32m.
func (r *Regtest) GenerateBech32mContext(ctx context.Context, labelStr string) (string, error) {
	return r.generateAddress(ctx, labelStr, "bech32m")
}

// generateAddress is the shared implementation behind GenerateBech32 and
// GenerateBech32m. addrType is forwarded as the second argument to bitcoind's
// getnewaddress RPC.
func (r *Regtest) generateAddress(ctx context.Context, label, addrType string) (string, error) {
	resp, err := r.rawRPC(ctx, "getnewaddress", label, addrType)
	if err != nil {
		return "", fmt.Errorf("failed to get new address (%s): %w", addrType, err)
	}
	var address string
	if err := json.Unmarshal(resp, &address); err != nil {
		return "", fmt.Errorf("failed to unmarshal address response: %w", err)
	}
	return address, nil
}

// ---------------------------------------------------------------
//  Mining
// ---------------------------------------------------------------

// Warp advances the blockchain by mining the specified number of blocks.
// This is a regtest-specific function that instantly mines blocks and
// sends the block rewards to the specified miner address.
//
// Parameters:
//   - blocks: Number of blocks to mine (must be > 0)
//   - miner: Bitcoin address to receive the block rewards (must be valid)
//
// Returns:
//   - error: Error if parameters are invalid or mining fails
//
// This function is useful for:
//   - Testing applications that depend on block confirmations
//   - Generating test funds by mining to a specific address
//   - Simulating time passage in regtest environments
//   - Creating UTXOs for testing transaction scenarios
//
// Example:
//
//	// Mine 100 blocks to generate test funds
//	err := rt.Warp(100, "bcrt1q...")
//	if err != nil {
//	    return fmt.Errorf("failed to mine blocks: %w", err)
//	}
//	fmt.Println("Mined 100 blocks successfully")
func (r *Regtest) Warp(blocks int64, miner string) error {
	return r.WarpContext(context.Background(), blocks, miner)
}

// WarpContext is the context-aware variant of Warp.
func (r *Regtest) WarpContext(ctx context.Context, blocks int64, miner string) error {
	if blocks <= 0 {
		return fmt.Errorf("blocks must be greater than 0, got %d", blocks)
	}
	if miner == "" {
		return fmt.Errorf("miner must be provided")
	}

	addr, err := btcutil.DecodeAddress(miner, &chaincfg.RegressionNetParams)
	if err != nil {
		return fmt.Errorf("failed to decode miner address: %w", err)
	}

	client, err := r.lockedClient()
	if err != nil {
		return err
	}

	_, err = runWithContext(ctx, func() ([]*chainhash.Hash, error) {
		return client.GenerateToAddress(blocks, addr, nil)
	})
	if err != nil {
		return fmt.Errorf("failed to generate blocks: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------
//  Transaction Operations
// ---------------------------------------------------------------

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

// =============================================================================
// INTERNAL HELPERS
// =============================================================================

// initialize performs one-time initialization of the Regtest instance.
// It writes the embedded bitcoind manager script to a temporary file and validates dependencies.
func (r *Regtest) initialize() error {
	// Check if bitcoind is installed
	if _, err := exec.LookPath("bitcoind"); err != nil {
		return fmt.Errorf("bitcoind not found in PATH - please install Bitcoin Core (brew install bitcoin / apt-get install bitcoind)")
	}

	// Create a temporary directory for the script
	tmpDir, err := os.MkdirTemp("", "go-regtest-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory for script: %w", err)
	}
	r.scriptTmpDir = tmpDir

	// Write the embedded script to the temp directory
	scriptPath := filepath.Join(tmpDir, "bitcoind_manager.sh")
	if err := os.WriteFile(scriptPath, []byte(bitcoindManagerScript), 0755); err != nil {
		os.RemoveAll(tmpDir) // Clean up on error
		return fmt.Errorf("failed to write bitcoind manager script: %w", err)
	}
	r.scriptPath = scriptPath

	return nil
}

// extractPort extracts the port number from the Host configuration.
// Returns the port as a string, defaulting to "18443" if extraction fails.
func (r *Regtest) extractPort() string {
	hostParts := strings.Split(r.config.Host, ":")
	if len(hostParts) == 2 {
		return hostParts[1]
	}
	return "18443" // default
}

// connectClient creates and stores the RPC client connection.
// This should be called after the node has started.
func (r *Regtest) connectClient() error {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()

	if r.client != nil {
		return nil // Already connected
	}

	client, err := rpcclient.New(r.RPCConfig(), nil)
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	r.client = client
	return nil
}

// lockedClient returns the current RPC client under read-lock, or errNotConnected
// if Start() has not been called (or Stop() cleared the client). The returned
// client is safe to use after the lock is released because *rpcclient.Client is
// internally synchronized; only the pointer slot needs lock protection.
func (r *Regtest) lockedClient() (*rpcclient.Client, error) {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	if r.client == nil {
		return nil, errNotConnected
	}
	return r.client, nil
}

// rawRPC issues a JSON-RPC call via the underlying btcd rpcclient and returns
// the raw response. Each arg is JSON-marshaled (json.RawMessage values pass
// through). The call respects ctx cancellation by returning ctx.Err() when the
// context is done, even though btcd's RawRequest is itself blocking.
func (r *Regtest) rawRPC(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}

	params := make([]json.RawMessage, len(args))
	for i, a := range args {
		if rm, ok := a.(json.RawMessage); ok {
			params[i] = rm
			continue
		}
		b, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("rawRPC %q: failed to marshal param %d: %w", method, i, err)
		}
		params[i] = b
	}

	return runWithContext(ctx, func() (json.RawMessage, error) {
		resp, err := client.RawRequest(method, params)
		if err != nil {
			return nil, fmt.Errorf("rawRPC %q failed: %w", method, err)
		}
		return resp, nil
	})
}

// runWithContext runs fn in a goroutine and returns its result, or ctx.Err()
// if the context is cancelled first. The fn continues running in the background
// after ctx cancellation; its result is discarded. This is the best the package
// can offer for cancellation given that btcd's rpcclient calls are blocking and
// don't accept a context.
func runWithContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case r := <-ch:
		return r.val, r.err
	}
}
