package regtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/btcsuite/btcd/rpcclient"
)

// ---------------------------------------------------------------
//  Bitcoin Core Node Management
// ---------------------------------------------------------------

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
	config     *Config
	scriptPath string
	mu         sync.Mutex
	initOnce   sync.Once
	initError  error
}

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

// initialize performs one-time initialization of the Regtest instance.
// It discovers the bitcoind manager script path and validates dependencies.
func (r *Regtest) initialize() error {
	// Check if bitcoind is installed
	if _, err := exec.LookPath("bitcoind"); err != nil {
		return fmt.Errorf("bitcoind not found in PATH - please install Bitcoin Core (brew install bitcoin / apt-get install bitcoind)")
	}

	// Get the current working directory
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Walk up the directory tree to find go.mod
	projectRoot := workDir
	found := false
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			found = true
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			// Reached filesystem root without finding go.mod
			break
		}
		projectRoot = parent
	}

	if !found {
		return fmt.Errorf("could not find project root (go.mod) - searched from %s up to filesystem root", workDir)
	}

	// Construct and verify script path
	r.scriptPath = filepath.Join(projectRoot, "scripts", "bitcoind_manager.sh")
	if _, err := os.Stat(r.scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("bitcoind manager script not found at: %s", r.scriptPath)
	}

	return nil
}

// ---------------------------------------------------------------
//  Configuration Management
// ---------------------------------------------------------------

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

// Start starts the Bitcoin regtest node using the bitcoind manager script.
// This method is thread-safe and will prevent multiple simultaneous start attempts.
//
// The function:
//   - Executes the bitcoind manager script with the "start" command
//   - Returns detailed error information if startup fails
//   - Uses mutex locking to prevent race conditions
//
// Returns:
//   - error: Detailed error if startup fails
//
// The started node will:
//   - Run on the regtest network
//   - Use this instance's configuration
//   - Be accessible via RPC on the configured port
//   - Create necessary data directories
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
	r.mu.Lock()
	defer r.mu.Unlock()

	// Extract port from Host (format: "host:port")
	hostParts := strings.Split(r.config.Host, ":")
	port := "18443" // default
	if len(hostParts) == 2 {
		port = hostParts[1]
	}

	// Pass config parameters to script: start datadir port user pass
	cmd := exec.Command("bash", r.scriptPath, "start", r.config.DataDir, port, r.config.User, r.config.Pass)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start bitcoind (script: %s): %s", r.scriptPath, string(output))
	}

	return nil
}

// Stop stops the Bitcoin regtest node and performs cleanup.
// This method is thread-safe and should be called to properly shut down
// the Bitcoin node and clean up resources.
//
// The function:
//   - Sends a stop signal to the running bitcoind process
//   - Waits for the process to terminate gracefully
//   - Cleans up data directories and temporary files
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

	// Extract port from Host (format: "host:port")
	hostParts := strings.Split(r.config.Host, ":")
	port := "18443" // default
	if len(hostParts) == 2 {
		port = hostParts[1]
	}

	// Pass config parameters to script: stop datadir port user pass
	cmd := exec.Command("bash", r.scriptPath, "stop", r.config.DataDir, port, r.config.User, r.config.Pass)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop bitcoind: %s", string(output))
	}

	return nil
}

// IsRunning checks if the Bitcoin regtest node is currently running.
// This method queries the node status without affecting its state.
// Uses mutex locking to allow concurrent access.
//
// The function:
//   - Executes the bitcoind manager script with the "status" command
//   - Parses the output to determine if the node is running
//   - Returns a boolean indicating the current state
//
// Returns:
//   - bool: true if bitcoind is running, false otherwise
//   - error: Error if the status check fails or script execution fails
//
// This method is useful for:
//   - Checking node state before performing operations
//   - Implementing health checks in applications
//   - Avoiding duplicate start attempts
//   - Monitoring node status in long-running processes
//
// Example:
//
//	rt, _ := regtest.New(nil)
//	running, err := rt.IsRunning()
//	if err != nil {
//	    return fmt.Errorf("failed to check node status: %w", err)
//	}
//	if !running {
//	    err := rt.Start()
//	    if err != nil {
//	        return fmt.Errorf("failed to start node: %w", err)
//	    }
//	}
func (r *Regtest) IsRunning() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Extract port from Host (format: "host:port")
	hostParts := strings.Split(r.config.Host, ":")
	port := "18443" // default
	if len(hostParts) == 2 {
		port = hostParts[1]
	}

	// Pass config parameters to script: status datadir port user pass
	cmd := exec.Command("bash", r.scriptPath, "status", r.config.DataDir, port, r.config.User, r.config.Pass)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to check bitcoind status: %s", string(output))
	}

	return strings.Contains(string(output), "is running"), nil
}
