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

var (
	// bitcoindMutex ensures thread-safe access to Bitcoin node operations.
	// This prevents race conditions when multiple goroutines try to start/stop
	// the Bitcoin node simultaneously.
	bitcoindMutex sync.Mutex

	// scriptPath holds the absolute path to the bitcoind_manager.sh script.
	// This is discovered lazily on first use by walking up the directory tree
	// to find the project root (go.mod).
	scriptPath string

	// initOnce ensures initialization happens only once
	initOnce sync.Once

	// initError stores any error that occurred during initialization
	initError error
)

// initialize performs one-time initialization of the package.
// It discovers the bitcoind manager script path and validates dependencies.
//
// The initialization process:
//  1. Checks if bitcoind is installed and available in PATH
//  2. Gets the current working directory
//  3. Walks up the directory tree looking for go.mod
//  4. Constructs the script path as scripts/bitcoind_manager.sh
//  5. Verifies the script exists and is accessible
//
// Returns:
//   - error: Detailed error if initialization fails
func initialize() error {
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
	scriptPath = filepath.Join(projectRoot, "scripts", "bitcoind_manager.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("bitcoind manager script not found at: %s", scriptPath)
	}

	return nil
}

// ensureInitialized ensures the package has been initialized.
// This function is called before any operation that requires initialization.
// It uses sync.Once to guarantee initialization happens exactly once.
//
// Returns:
//   - error: Error from initialization if it failed
func ensureInitialized() error {
	initOnce.Do(func() {
		initError = initialize()
	})
	return initError
}

// DefaultRegtestConfig returns a pre-configured RPC connection config for Bitcoin regtest.
// This configuration connects to a local Bitcoin node running on the standard regtest port
// with basic authentication credentials.
//
// Returns:
//   - *rpcclient.ConnConfig: Connection configuration for regtest network
//
// Configuration details:
//   - Host: 127.0.0.1:18443 (standard regtest RPC port)
//   - Authentication: user/pass (default regtest credentials)
//   - HTTP POST mode enabled for JSON-RPC communication
//   - TLS disabled for local development
func DefaultRegtestConfig() *rpcclient.ConnConfig {
	return &rpcclient.ConnConfig{
		Host:         "127.0.0.1:18443",
		User:         "user",
		Pass:         "pass",
		HTTPPostMode: true,
		DisableTLS:   true,
	}

}

// StartBitcoinRegtest starts a Bitcoin regtest node using the bitcoind manager script.
// This function is thread-safe and will prevent multiple simultaneous start attempts.
//
// The function:
//   - Validates that the bitcoind manager script exists
//   - Executes the script with the "start" command
//   - Returns detailed error information if startup fails
//   - Uses mutex locking to prevent race conditions
//
// Returns:
//   - error: Detailed error if script is missing or startup fails
//
// The started node will:
//   - Run on the regtest network
//   - Use the default regtest configuration
//   - Be accessible via RPC on the configured port
//   - Create necessary data directories
//
// Example:
//
//	err := StartBitcoinRegtest()
//	if err != nil {
//	    log.Fatalf("Failed to start Bitcoin node: %v", err)
//	}
//	defer StopBitcoinRegtest() // Always clean up
func StartBitcoinRegtest() error {
	// Ensure package is initialized
	if err := ensureInitialized(); err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	bitcoindMutex.Lock()
	defer bitcoindMutex.Unlock()

	cmd := exec.Command("bash", scriptPath, "start")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start bitcoind (script: %s): %s", scriptPath, string(output))
	}

	return nil
}

// StopBitcoinRegtest stops the Bitcoin regtest node and performs cleanup.
// This function is thread-safe and should be called to properly shut down
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
// It's recommended to always call this function in defer statements
// to ensure proper cleanup, even if the program exits unexpectedly.
//
// Example:
//
//	err := StartBitcoinRegtest()
//	if err != nil {
//	    return err
//	}
//	defer StopBitcoinRegtest() // Ensures cleanup
func StopBitcoinRegtest() error {
	// Ensure package is initialized
	if err := ensureInitialized(); err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	bitcoindMutex.Lock()
	defer bitcoindMutex.Unlock()

	cmd := exec.Command("bash", scriptPath, "stop")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop bitcoind: %s", string(output))
	}

	return nil
}

// IsBitcoindRunning checks if the Bitcoin regtest node is currently running.
// This function queries the node status without affecting its state. Uses mutex locking to allow concurrent access.
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
// This function is useful for:
//   - Checking node state before performing operations
//   - Implementing health checks in applications
//   - Avoiding duplicate start attempts
//   - Monitoring node status in long-running processes
//
// Example:
//
//	running, err := IsBitcoindRunning()
//	if err != nil {
//	    return fmt.Errorf("failed to check node status: %w", err)
//	}
//	if !running {
//	    err := StartBitcoinRegtest()
//	    if err != nil {
//	        return fmt.Errorf("failed to start node: %w", err)
//	    }
//	}
func IsBitcoindRunning() (bool, error) {
	// Ensure package is initialized
	if err := ensureInitialized(); err != nil {
		return false, fmt.Errorf("initialization failed: %w", err)
	}

	bitcoindMutex.Lock()
	defer bitcoindMutex.Unlock()

	cmd := exec.Command("bash", scriptPath, "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to check bitcoind status: %s", string(output))
	}

	return strings.Contains(string(output), "is running"), nil
}
