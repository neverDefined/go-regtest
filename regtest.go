// Package regtest provides a lightweight Go library for managing Bitcoin Core
// regtest environments. See doc.go for detailed documentation.
package regtest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
)

// errNotConnected is returned by RPC methods called before Start() or after Stop().
var errNotConnected = errors.New("RPC client not connected")

//go:embed scripts/bitcoind_manager.sh
var bitcoindManagerScript string

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

	// VBParams configures named BIP9 deployments. Each entry renders to one
	// -vbparams=<name>:<start>:<timeout>:<min_activation_height> flag. See
	// VBParam, VBAlwaysActive, and VBNeverActive in softfork.go.
	VBParams []VBParam

	// AcceptNonstdTxn maps to -acceptnonstdtxn=1 when true. Pre-standardness
	// soft-fork transactions (APO sigs, CTV-committed outputs, etc.) are
	// consensus-valid but mempool-rejected by default; flip this on for any
	// test that needs to broadcast such a tx through the mempool. Default false.
	AcceptNonstdTxn bool

	// BinaryPath overrides the bitcoind binary used by Start/Stop.
	//
	// When empty (the default), the harness searches PATH for
	// bitcoind-inquisition first, then falls back to bitcoind. Set this to
	// run against a non-standard build (e.g. /opt/bitcoin/bin/bitcoind)
	// without modifying PATH.
	//
	// Accepts an absolute path, a relative path, or a bare name resolved via
	// PATH (e.g. "bitcoind-inquisition"). The bitcoin-cli companion is
	// derived from the same directory, falling back to bitcoin-cli on PATH.
	BinaryPath string
}

// Regtest manages a Bitcoin regtest node instance.
// Each instance can run independently with its own configuration.
// This design allows multiple regtest nodes to run simultaneously
// on different ports with different configurations.
type Regtest struct {
	config         *Config
	scriptPath     string
	scriptTmpDir   string // Directory containing the temporary script file
	bitcoindPath   string // Resolved absolute path to bitcoind
	bitcoinCliPath string // Resolved absolute path to bitcoin-cli
	mu             sync.Mutex
	client         *rpcclient.Client
	clientMu       sync.RWMutex

	// variantMu guards variantCached / variant. The first VariantContext
	// call hits getnetworkinfo; subsequent calls return the cached value.
	variantMu     sync.Mutex
	variantCached bool
	variant       Variant
}

// New creates a new Regtest instance with the provided configuration.
// If config is nil, default configuration values are used.
//
// The initialization process:
//  1. Resolves the bitcoind binary — Config.BinaryPath if set, otherwise
//     bitcoind-inquisition then bitcoind on PATH.
//  2. Resolves the bitcoin-cli companion alongside bitcoind, falling back
//     to bitcoin-cli on PATH.
//  3. Writes the embedded bitcoind manager script to a temp directory.
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
			Host:            config.Host,
			User:            config.User,
			Pass:            config.Pass,
			DataDir:         config.DataDir,
			ExtraArgs:       append([]string(nil), config.ExtraArgs...),
			VBParams:        append([]VBParam(nil), config.VBParams...),
			AcceptNonstdTxn: config.AcceptNonstdTxn,
			BinaryPath:      config.BinaryPath,
		}
	}

	// Validate VBParams: empty Deployment is a configuration mistake we
	// catch eagerly rather than letting bitcoind silently ignore the flag.
	for i, vb := range rt.config.VBParams {
		if vb.Deployment == "" {
			return nil, fmt.Errorf("VBParams[%d].Deployment must not be empty", i)
		}
	}

	// Initialize immediately
	if err := rt.initialize(); err != nil {
		return nil, err
	}

	return rt, nil
}

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
		Host:            r.config.Host,
		User:            r.config.User,
		Pass:            r.config.Pass,
		DataDir:         r.config.DataDir,
		ExtraArgs:       append([]string(nil), r.config.ExtraArgs...),
		VBParams:        append([]VBParam(nil), r.config.VBParams...),
		AcceptNonstdTxn: r.config.AcceptNonstdTxn,
		BinaryPath:      r.config.BinaryPath,
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

	// Pass config parameters to script: start datadir port user pass [extra-args...].
	// renderExtraArgs combines Config.ExtraArgs with rendered VBParams and
	// -acceptnonstdtxn; the script forwards them verbatim to bitcoind (see
	// scripts/bitcoind_manager.sh).
	scriptArgs := append([]string{r.scriptPath, "start", r.config.DataDir, port, r.config.User, r.config.Pass}, r.config.renderExtraArgs()...)
	cmd := exec.CommandContext(ctx, "bash", scriptArgs...)
	cmd.Env = append(os.Environ(), "BITCOIND_BIN="+r.bitcoindPath, "BITCOIN_CLI_BIN="+r.bitcoinCliPath)
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
	cmd.Env = append(os.Environ(), "BITCOIND_BIN="+r.bitcoindPath, "BITCOIN_CLI_BIN="+r.bitcoinCliPath)
	output, err := cmd.CombinedOutput()

	// Note: The temporary script dir is cleaned up by Cleanup().

	if err != nil {
		return fmt.Errorf("failed to stop bitcoind: %s", string(output))
	}

	return nil
}

// Cleanup removes temporary files and directories created by this Regtest instance.
// It is safe to call multiple times. Stop() does not invoke Cleanup() automatically;
// call it explicitly when you are completely done with the instance.
//
// As of Phase 1.3, IsRunning() probes the RPC port directly and remains valid
// after Cleanup().
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

// initialize performs one-time initialization of the Regtest instance.
// It resolves the bitcoind / bitcoin-cli binaries (honoring Config.BinaryPath
// or auto-detecting on PATH) and writes the embedded bitcoind manager script
// to a temporary file.
func (r *Regtest) initialize() error {
	// Resolve the bitcoind binary (Config.BinaryPath if set, else PATH chain).
	bitcoindPath, bitcoinCliPath, err := resolveBinary(r.config.BinaryPath)
	if err != nil {
		return err
	}
	r.bitcoindPath = bitcoindPath
	r.bitcoinCliPath = bitcoinCliPath

	// Create a temporary directory for the script
	tmpDir, err := os.MkdirTemp("", "go-regtest-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory for script: %w", err)
	}
	r.scriptTmpDir = tmpDir

	// Write the embedded script to the temp directory. The script is invoked
	// as `bash <scriptPath> ...` so it doesn't need the executable bit; 0600
	// (owner read/write only) is sufficient and avoids gosec G306.
	scriptPath := filepath.Join(tmpDir, "bitcoind_manager.sh")
	if err := os.WriteFile(scriptPath, []byte(bitcoindManagerScript), 0600); err != nil {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			return fmt.Errorf("failed to write bitcoind manager script (%w) and failed to clean up temp dir (%w)", err, rmErr)
		}
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

// resolveBinary resolves the bitcoind path (honoring an explicit override or
// the PATH auto-detect chain bitcoind-inquisition → bitcoind) and derives the
// bitcoin-cli companion alongside it, falling back to bitcoin-cli on PATH.
//
// Parameters:
//   - path: optional Config.BinaryPath. Empty means auto-detect; otherwise may
//     be an absolute path, relative path, or bare name resolved via PATH.
//
// Returns:
//   - bitcoind: absolute path to the bitcoind binary.
//   - bitcoinCli: absolute path to the matching bitcoin-cli.
//   - err: wrapped error if no candidate is executable.
func resolveBinary(path string) (bitcoind, bitcoinCli string, err error) {
	bitcoind, err = resolveBitcoind(path)
	if err != nil {
		return "", "", err
	}
	bitcoinCli, err = resolveBitcoinCli(bitcoind)
	if err != nil {
		return "", "", err
	}
	return bitcoind, bitcoinCli, nil
}

// resolveBitcoind picks the bitcoind binary. When path is non-empty it is
// resolved via exec.LookPath so absolute, relative, and bare names all work
// (LookPath bypasses PATH if the name contains a separator). When path is
// empty the auto-detect chain prefers bitcoind-inquisition, then falls back
// to bitcoind.
func resolveBitcoind(path string) (string, error) {
	if path != "" {
		p, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("Config.BinaryPath %q: %w", path, err)
		}
		return p, nil
	}
	if p, err := exec.LookPath("bitcoind-inquisition"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("bitcoind"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("bitcoind not found in PATH (tried bitcoind-inquisition, bitcoind) — install Bitcoin Core or set Config.BinaryPath")
}

// resolveBitcoinCli looks for bitcoin-cli alongside the resolved bitcoind
// binary, then falls back to whatever bitcoin-cli is on PATH. Sibling
// resolution lets bundled installs (Inquisition shipping its own bitcoin-cli
// in the same dir) work without further configuration; the PATH fallback
// covers the common case where bitcoin-cli is installed once globally.
func resolveBitcoinCli(bitcoind string) (string, error) {
	sibling := filepath.Join(filepath.Dir(bitcoind), "bitcoin-cli")
	if p, err := exec.LookPath(sibling); err == nil {
		return p, nil
	}
	p, err := exec.LookPath("bitcoin-cli")
	if err != nil {
		return "", fmt.Errorf("bitcoin-cli not found alongside %s or in PATH: %w", bitcoind, err)
	}
	return p, nil
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
