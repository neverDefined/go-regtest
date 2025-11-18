# go-regtest

A lightweight Go library for managing Bitcoin Core regtest environments.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-blue.svg)](https://golang.org)
[![CI](https://github.com/neverDefined/go-regtest/workflows/CI/badge.svg)](https://github.com/neverDefined/go-regtest/actions)

## Features

- Simple API for starting/stopping regtest nodes
- Multiple independent instances on different ports
- Wallet management, address generation, transactions
- Thread-safe and well-tested

## Prerequisites

- Bitcoin Core: `brew install bitcoin` (macOS) or `sudo apt-get install bitcoind` (Linux)
- Go 1.23+

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
    rt, err := regtest.New(nil)
    if err != nil {
        log.Fatal(err)
    }
    
    if err := rt.Start(); err != nil {
        log.Fatal(err)
    }
    defer rt.Stop()
    
    rt.EnsureWallet("miner")
    addr, _ := rt.GenerateBech32("miner")
    rt.Warp(101, addr) // Mine to maturity
    
    blockCount, _ := rt.GetBlockCount()
    log.Printf("Block height: %d", blockCount)
}
```

## Usage

### Configuration

```go
// Default config
rt, _ := regtest.New(nil)

// Custom config
rt, _ := regtest.New(&regtest.Config{
    Host:    "127.0.0.1:19000",
    User:    "myuser",
    Pass:    "mypass",
    DataDir: "./my_regtest_data",
})
```

Default values: `Host: "127.0.0.1:18443"`, `User: "user"`, `Pass: "pass"`, `DataDir: "./bitcoind_regtest"`

### Lifecycle

```go
rt.Start()
rt.IsRunning()
rt.Stop()
```

### Multiple Instances

```go
rt1, _ := regtest.New(&regtest.Config{Host: "127.0.0.1:19000", DataDir: "./regtest_1"})
rt2, _ := regtest.New(&regtest.Config{Host: "127.0.0.1:19100", DataDir: "./regtest_2"})
rt1.Start()
rt2.Start()
defer rt1.Stop()
defer rt2.Stop()
```

### Common Operations

```go
// Wallets
rt.CreateWallet("wallet")
rt.LoadWallet("wallet")
rt.EnsureWallet("wallet") // Create or load
rt.UnloadWallet("wallet")

// Addresses
addr, _ := rt.GenerateBech32("wallet")   // P2WPKH
addr, _ := rt.GenerateBech32m("wallet")  // P2TR (Taproot)

// Mining
rt.Warp(101, addr) // Mine blocks to address

// Transactions
txid, _ := rt.SendToAddress(addr, 100_000_000) // Send satoshis
utxo, _ := rt.GetTxOut(txid, 0, true)
utxos, _ := rt.ScanTxOutSetForAddress(addr)

// Signing & Broadcasting
signedTx, _ := rt.SignRawTransactionWithWallet(unsignedTx)
txid, _ := rt.BroadcastTransaction(signedTx)

// Direct RPC access
client := rt.Client()
info, _ := client.GetBlockChainInfo()
```

## API Reference

### Types

```go
type Config struct {
    Host    string   // RPC host:port (default: "127.0.0.1:18443")
    User    string   // RPC username (default: "user")
    Pass    string   // RPC password (default: "pass")
    DataDir string   // Data directory (default: "./bitcoind_regtest")
    ExtraArgs []string
}
```

### Methods

**Lifecycle:** `Start()`, `Stop()`, `IsRunning()`

**Configuration:** `Config()`, `RPCConfig()`

**RPC:** `Client()`, `GetBlockCount()`, `HealthCheck()`

**Wallets:** `CreateWallet(name)`, `LoadWallet(name)`, `UnloadWallet(name)`, `EnsureWallet(name)`, `GetWalletInformation()`

**Addresses:** `GenerateBech32(label)`, `GenerateBech32m(label)`

**Mining:** `Warp(blocks, address)`

**Transactions:** `SendToAddress(address, sats)`, `GetTxOut(txid, vout, includeMempool)`, `ScanTxOutSetForAddress(address)`, `SignRawTransactionWithWallet(tx)`, `BroadcastTransaction(tx)`

See [godoc](https://pkg.go.dev/github.com/neverDefined/go-regtest) for detailed API documentation.

## Development

```bash
make test              # Run tests
make test-race         # Run with race detector
make test-coverage     # Generate coverage report
make check-all         # Format, vet, lint, test
```

## Troubleshooting

**Node fails to start:** Check `bitcoind` is installed (`which bitcoind`) and port is available.

**RPC connection fails:** Verify node is running and wait a few seconds after `Start()`.

**Port conflicts:** Use widely spaced ports (e.g., 19000, 19100) and different data directories.


---

**Note:** For development and testing only. Not for production use.
