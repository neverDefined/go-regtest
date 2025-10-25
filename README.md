# go-regtest

A lightweight Go library for managing Bitcoin Core regtest environments with minimal dependencies. Perfect for integration testing, development, and prototyping Bitcoin applications.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-blue.svg)](https://golang.org)
[![CI](https://github.com/neverDefined/go-regtest/workflows/CI/badge.svg)](https://github.com/neverDefined/go-regtest/actions)

## Features

- **Simple API**: Start and stop Bitcoin Core regtest nodes with a single function call
- **Multiple Instances**: Run multiple independent regtest nodes simultaneously on different ports
- **Built-in RPC Methods**: Wallet management, address generation, transaction handling, and more
- **Configurable**: Customize ports, credentials, data directories, and more
- **Thread-Safe**: Safe for concurrent use with proper mutex protection
- **Pure Go**: 100% Go implementation with a minimal bash script for process management
- **Well Tested**: 74%+ test coverage with comprehensive integration tests

## Prerequisites

- **Bitcoin Core**: Install via your system's package manager
  - macOS: `brew install bitcoin`
  - Ubuntu/Debian: `sudo apt-get install bitcoind` or `sudo snap install bitcoin-core`
  - Arch: `sudo pacman -S bitcoin-core`
- **Go**: Version 1.23 or higher

## Installation

```bash
go get github.com/neverDefined/go-regtest
```

## Quick Start

```go
package main

import (
    "log"
    
    "github.com/neverDefined/go-regtest"
)

func main() {
    // Create a new regtest instance with default configuration
    rt, err := regtest.New(nil)
    if err != nil {
        log.Fatalf("Failed to create regtest: %v", err)
    }
    
    // Start the Bitcoin regtest node
    if err := rt.Start(); err != nil {
        log.Fatalf("Failed to start bitcoind: %v", err)
    }
    defer rt.Stop()
    
    // Create or load a wallet
    if err := rt.EnsureWallet("miner"); err != nil {
        log.Fatalf("Failed to ensure wallet: %v", err)
    }
    
    // Generate a new address
    addr, err := rt.GenerateBech32("miner")
    if err != nil {
        log.Fatalf("Failed to generate address: %v", err)
    }
    log.Printf("Generated address: %s", addr)
    
    // Mine some blocks to the address
    if err := rt.Warp(101, addr); err != nil {
        log.Fatalf("Failed to mine blocks: %v", err)
    }
    
    // Check block count
    blockCount, err := rt.GetBlockCount()
    if err != nil {
        log.Fatalf("Failed to get block count: %v", err)
    }
    log.Printf("Current block height: %d", blockCount)
}
```

## Usage

### Creating a Regtest Instance

**With default configuration:**
```go
rt, err := regtest.New(nil)
if err != nil {
    log.Fatal(err)
}
```

**With custom configuration:**
```go
config := &regtest.Config{
    Host:    "127.0.0.1:19000",
    User:    "myuser",
    Pass:    "mypass",
    DataDir: "./my_regtest_data",
}

rt, err := regtest.New(config)
if err != nil {
    log.Fatal(err)
}
```

**Default configuration values:**
- Host: `127.0.0.1:18443`
- User: `user`
- Pass: `pass`
- DataDir: `./bitcoind_regtest`

### Lifecycle Management

```go
// Start the node
err := rt.Start()

// Check if running
running, err := rt.IsRunning()

// Stop the node
err = rt.Stop()
```

### Multiple Instances

Run multiple independent regtest nodes simultaneously:

```go
// First instance on port 19000
rt1, _ := regtest.New(&regtest.Config{
    Host:    "127.0.0.1:19000",
    User:    "user1",
    Pass:    "pass1",
    DataDir: "./bitcoind_regtest_1",
})

// Second instance on port 19100
rt2, _ := regtest.New(&regtest.Config{
    Host:    "127.0.0.1:19100",
    User:    "user2",
    Pass:    "pass2",
    DataDir: "./bitcoind_regtest_2",
})

rt1.Start()
rt2.Start()
defer rt1.Stop()
defer rt2.Stop()
```

### Wallet Management

```go
// Create a new wallet
err := rt.CreateWallet("mywallet")

// Load an existing wallet
err := rt.LoadWallet("mywallet")

// Unload a wallet
err := rt.UnloadWallet("mywallet")

// Ensure wallet exists (create if not, load if exists)
err := rt.EnsureWallet("mywallet")

// Get wallet information
info, err := rt.GetWalletInformation("mywallet")
```

### Address Generation

```go
// Generate a bech32 address (P2WPKH, SegWit v0)
addr, err := rt.GenerateBech32("mywallet")

// Generate a bech32m address (P2TR, SegWit v1, Taproot)
addr, err := rt.GenerateBech32m("mywallet")
```

### Mining Blocks

```go
// Mine 10 blocks to an address
err := rt.Warp(10, "bcrt1qaddress...")

// Mine to maturity (101 blocks)
err := rt.Warp(101, "bcrt1qaddress...")
```

### Sending Transactions

```go
// Send 1 BTC to an address (amount in satoshis)
txHash, err := rt.SendToAddress("bcrt1qaddress...", 100_000_000)

// Check transaction output
utxo, err := rt.GetTxOut(txHash, 0)
```

### Scanning UTXOs

```go
// Find all UTXOs for an address
utxos, err := rt.ScanTxOutSetForAddress("bcrt1qaddress...")
for _, utxo := range utxos {
    log.Printf("UTXO: %s:%d with %.8f BTC", 
        utxo.TxID, utxo.Vout, utxo.Amount)
}
```

### Transaction Signing

```go
// Sign a raw transaction with wallet
signedTxHex, complete, err := rt.SignRawTransactionWithWallet(unsignedTxHex, "mywallet")
if !complete {
    log.Println("Transaction not fully signed")
}

// Broadcast the signed transaction
txHash, err := rt.BroadcastTransaction(signedTxHex)
```

### Direct RPC Client Access

For advanced operations, access the underlying RPC client:

```go
client := rt.Client()

// Use any btcd/rpcclient methods
info, err := client.GetBlockChainInfo()
mempool, err := client.GetRawMempool()
```

## API Reference

### Types

#### `Config`
Configuration for a regtest instance.

```go
type Config struct {
    Host    string  // RPC host:port (default: "127.0.0.1:18443")
    User    string  // RPC username (default: "user")
    Pass    string  // RPC password (default: "pass")
    DataDir string  // Data directory (default: "./bitcoind_regtest")
}
```

#### `Regtest`
Represents a Bitcoin Core regtest instance.

```go
type Regtest struct {
    // unexported fields
}
```

### Constructor

#### `New(config *Config) (*Regtest, error)`
Creates a new Regtest instance with the given configuration. Pass `nil` for default configuration.

### Lifecycle Methods

- `Start() error` - Start the Bitcoin node
- `Stop() error` - Stop the Bitcoin node
- `IsRunning() (bool, error)` - Check if the node is running

### Configuration Methods

- `Config() *Config` - Get a copy of the instance's configuration
- `RPCConfig() *rpcclient.ConnConfig` - Get RPC client configuration

### RPC Client Methods

- `Client() *rpcclient.Client` - Get the underlying RPC client
- `GetBlockCount() (int64, error)` - Get current block height
- `HealthCheck() error` - Verify node is responding

### Wallet Methods

- `CreateWallet(name string) error` - Create a new wallet
- `LoadWallet(name string) error` - Load an existing wallet
- `UnloadWallet(name string) error` - Unload a wallet
- `EnsureWallet(name string) error` - Ensure wallet exists (create or load)
- `GetWalletInformation(wallet string) (*btcjson.GetWalletInfoResult, error)` - Get wallet info

### Address Methods

- `GenerateBech32(wallet string) (string, error)` - Generate P2WPKH address
- `GenerateBech32m(wallet string) (string, error)` - Generate P2TR address (Taproot)

### Mining Methods

- `Warp(blocks int, address string) error` - Mine blocks to an address

### Transaction Methods

- `SendToAddress(address string, amount int64) (string, error)` - Send amount (satoshis) to address
- `GetTxOut(txHash string, vout uint32) (*btcjson.GetTxOutResult, error)` - Get transaction output
- `ScanTxOutSetForAddress(address string) ([]UTXOResult, error)` - Scan UTXO set for address
- `SignRawTransactionWithWallet(txHex, wallet string) (string, bool, error)` - Sign transaction
- `BroadcastTransaction(txHex string) (string, error)` - Broadcast raw transaction

## Development

### Prerequisites

```bash
# Install development tools
make install-tools
```

### Available Make Targets

```bash
# Building and testing
make build              # Build the project
make test               # Run tests
make test-race          # Run tests with race detector
make test-coverage      # Generate coverage report
make bench              # Run benchmarks

# Code quality
make fmt                # Format code
make vet                # Run go vet
make lint               # Run golangci-lint

# Combined checks
make check-all          # Run all checks
make ci                 # Run CI pipeline checks
make pre-commit         # Quick pre-commit checks

# Dependencies
make deps               # Download dependencies
make tidy               # Tidy go.mod
make verify             # Verify dependencies

# Cleanup
make clean              # Clean build artifacts
make clean-all          # Clean everything including cache

# Regtest operations
make run-regtest        # Start a regtest node manually
make stop-regtest       # Stop the regtest node
make status-regtest     # Check regtest status
```

### Running Tests

```bash
# All tests
make test

# With race detector (recommended before commit)
make test-race

# With coverage
make test-coverage
open coverage.html
```

### Code Quality Checks

```bash
# Format, vet, lint, and test
make check-all

# Before committing
make pre-commit
```

## Project Structure

```
go-regtest/
├── regtest.go              # Main library implementation
├── regtest_test.go         # Basic tests
├── regtest_rpc_test.go     # RPC functionality tests
├── scripts/
│   └── bitcoind_manager.sh # Bitcoin Core process manager
├── Makefile                # Development automation
├── .github/
│   ├── workflows/
│   │   └── ci.yml          # GitHub Actions CI
│   └── dependabot.yml      # Dependency updates
├── .gitignore
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

## Testing

The library includes comprehensive tests covering:

- Basic lifecycle (start/stop/status)
- Configuration management
- Multiple simultaneous instances
- RPC connectivity
- Wallet operations
- Address generation (bech32 and bech32m)
- Block mining
- Transaction creation and signing
- UTXO scanning

Run the test suite:
```bash
make test
```

Run with race detector:
```bash
make test-race
```

## Use Cases

- **Integration Testing**: Test Bitcoin applications against a real local node
- **Development**: Rapid prototyping without testnet/mainnet setup
- **CI/CD**: Automated testing in continuous integration pipelines
- **Education**: Learn Bitcoin development in a safe environment
- **Multi-node Testing**: Test scenarios requiring multiple independent nodes

## Troubleshooting

### Node Fails to Start

- Verify Bitcoin Core is installed: `which bitcoind`
- Check if port is available: `lsof -i :18443`
- Ensure the `scripts/bitcoind_manager.sh` script has execute permissions
- Check for existing `bitcoind_regtest` directory conflicts

### RPC Connection Fails

- Verify the node is running: `rt.IsRunning()`
- Check credentials match your configuration
- Ensure no firewall is blocking the port
- Wait a few seconds after `Start()` for node to initialize

### Multiple Instances Port Conflict

- Use widely spaced ports (e.g., 19000, 19100) as Bitcoin uses both RPC and P2P ports
- Bitcoin Core uses RPC port and RPC+1 for P2P by default
- Ensure data directories are different for each instance

### Dependency Check Fails

The library checks for `bitcoind` on initialization. If you get "bitcoind not found":
```bash
# macOS
brew install bitcoin

# Ubuntu
sudo snap install bitcoin-core

# Or build from source
```


---

**Note**: This library is intended for development and testing purposes only. Do not use regtest nodes for production applications.
