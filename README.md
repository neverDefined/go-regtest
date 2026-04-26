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

### Optional: Bitcoin Inquisition (for upcoming soft-fork testing)

[Bitcoin Inquisition](https://github.com/bitcoin-inquisition/bitcoin) is the experimental Core fork that activates the upcoming soft forks (BIP54 Consensus Cleanup, BIP118 ANYPREVOUT, BIP119 CTV, BIP347 OP_CAT, BIP348 CSFS, BIP349 INTERNALKEY). Build it once into its own directory — nothing escapes the clone, so it can't conflict with Homebrew/apt's `bitcoind`:

```bash
git clone https://github.com/bitcoin-inquisition/bitcoin.git ~/btc/bitcoin-inquisition
cd ~/btc/bitcoin-inquisition
cmake -B build -DBUILD_TESTS=OFF -DBUILD_GUI=OFF
cmake --build build -j$(sysctl -n hw.ncpu)   # use $(nproc) on Linux
```

Two ways to point this library at the resulting binary:

```go
// (a) Explicit BinaryPath — recommended while iterating on a single test
rt, _ := regtest.New(&regtest.Config{
    BinaryPath: "/Users/you/btc/bitcoin-inquisition/build/bin/bitcoind",
})

// (b) Auto-detect — symlink it as bitcoind-inquisition on PATH and the
//     library finds it ahead of stock bitcoind
//
//   ln -s ~/btc/bitcoin-inquisition/build/bin/bitcoind \
//         /usr/local/bin/bitcoind-inquisition
//
//   rt, _ := regtest.New(nil)  // picks Inquisition if present
```

`rt.Variant()` reports `VariantCore` or `VariantInquisition` after Start so tests can branch on which binary is running.

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

### Soft-fork testing

Configure a BIP9 deployment via `VBParams` and observe the activation state machine end-to-end. The `testdummy` deployment Bitcoin Core ships on regtest is the canonical no-consensus-code soft-fork test:

```go
rt, _ := regtest.New(&regtest.Config{
    AcceptNonstdTxn: true,
    VBParams: []regtest.VBParam{{
        Deployment: "testdummy", StartTime: 0, Timeout: 9999999999,
    }},
})
rt.Start(); defer rt.Stop()
status, _ := rt.DeploymentStatus("testdummy")  // SoftForkDefined / Started / ...

// Mine through retarget windows until ACTIVE.
miner, _ := rt.GenerateBech32("miner")
for status != regtest.SoftForkActive {
    rt.Warp(144, miner)
    status, _ = rt.DeploymentStatus("testdummy")
}
```

For a fully-narrated walkthrough, see [`TestExampleActivateTestdummy`](examples_test.go) — the same template applies to real future soft-forks (APO/eltoo, CTV, CSFS) once you point `bitcoind` in `$PATH` at a binary that knows the deployment.

#### Skip-when-missing pattern

For tests that depend on an Inquisition-only deployment, use `SupportsBIP` and `t.Skip` so the same suite stays green on a Core-only machine:

```go
import "github.com/neverDefined/go-regtest"

func TestMyCTVThing(t *testing.T) {
    rt, _ := regtest.New(nil)
    rt.Start(); defer rt.Stop()

    if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
        t.Skip("requires bitcoind-inquisition; see README")
    }

    miner, _ := rt.GenerateBech32m("miner")
    rt.MineUntilActiveBIP(regtest.BIP119, miner, 2000)
    // … now build/broadcast a CTV-spending tx …
}
```

`rt.ListDeployments()` returns the merged registry-and-live view (`BIPID`, `BIPNumber`, `Name`, `DocURL`, `Status`, `Type`, `Active`, `Height`) keyed by deployment string, useful for diagnostics. See [`TestExampleActivateBIP119`](examples_inquisition_test.go) for the full template.

### Multi-node and reorg testing

For a narrated two-node fork-resolution example — partition the network, mine divergent chains, reconnect, observe Bitcoin's longest-chain rule resolve the fork — see [`TestExampleReorg`](examples_reorg_test.go).

## API Reference

### Types

```go
type Config struct {
    Host            string   // RPC host:port (default: "127.0.0.1:18443")
    User            string   // RPC username (default: "user")
    Pass            string   // RPC password (default: "pass")
    DataDir         string   // Data directory (default: "./bitcoind_regtest")
    ExtraArgs       []string // Forwarded verbatim to bitcoind on Start
    VBParams        []VBParam // BIP9 deployment configuration
    AcceptNonstdTxn bool     // -acceptnonstdtxn=1 when true
    BinaryPath      string   // Override bitcoind binary; empty = PATH auto-detect
}
```

### Methods

**Lifecycle:** `Start()`, `Stop()`, `Cleanup()`, `IsRunning()`

**Configuration:** `DefaultConfig()`, `Config()`, `RPCConfig()`

**RPC:** `Client()`, `GetBlockCount()`, `HealthCheck()`

**Wallets:** `CreateWallet(name)`, `LoadWallet(name)`, `UnloadWallet(name)`, `EnsureWallet(name)`, `GetWalletInformation()`

**Addresses:** `GenerateBech32(label)`, `GenerateBech32m(label)`

**Mining:** `Warp(blocks, address)`, `MineToHeight(target, address)`, `MineUntilActive(deployment, address, maxBlocks)`, `MineUntilActiveBIP(BIPID, address, maxBlocks)`, `GetBlockTemplate(req)`, `SubmitBlock(block)`

**Soft-fork registry:** `Variant()`, `ListDeployments()`, `SupportsBIP(BIPID)`, `DeploymentStatus(name)`. Typed BIPID constants: `BIP54`, `BIP118`, `BIP119`, `BIP347`, `BIP348`, `BIP349`, `BIPTestdummy`, `BIPTaproot`.

**Transactions:** `SendToAddress(address, sats)`, `GetTxOut(txid, vout, includeMempool)`, `ScanTxOutSetForAddress(address)`, `SignRawTransactionWithWallet(tx)`, `BroadcastTransaction(tx)`, `CreateRawTransaction(inputs, amounts, lockTime)`, `DecodeRawTransaction(tx)`, `DecodeScript(scriptHex)`, `FundRawTransaction(tx, opts)`, `TestMempoolAccept(txs...)`

**Peers:** `Connect(other)`, `Disconnect(other)`, `AddNode(host)`, `GetConnectionCount()`

Every RPC-issuing method also has a `*Context` variant (`StartContext`, `GetBlockCountContext`, `WarpContext`, etc.) that accepts a `context.Context` for timeout and cancellation. The non-`Context` form is a thin `context.Background()` wrapper.

See [godoc](https://pkg.go.dev/github.com/neverDefined/go-regtest) for detailed API documentation.

## Development

```bash
make test              # Run tests
make test-race         # Run with race detector
make test-coverage     # Generate coverage report (HTML + summary)
make lint              # Run golangci-lint
make vuln              # Run govulncheck
make ai-check          # Full gate: fmt + vet + lint + test-race + vuln
make check-all         # Format, vet, lint, test
make release-dry-run   # Build a snapshot release locally (no publish)
```

On macOS, raise the file-descriptor limit before running tests so bitcoind has enough headroom:

```bash
ulimit -n 4096
```

If you use Claude Code, this repo ships with a tuned `.claude/` setup (subagents, slash commands, auto-format hook) and a `CLAUDE.md` that loads automatically. See `CLAUDE.md` for the conventions.

## Releases

Tagged releases (`v*`) are published by the `release.yml` workflow, which runs the test suite then `goreleaser` to attach a source archive and checksums to a GitHub Release. Notes are auto-generated from `git log`.

To cut a release:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

## Troubleshooting

**Node fails to start:** Check `bitcoind` is installed (`which bitcoind`) and port is available.

**RPC connection fails:** Verify node is running and wait a few seconds after `Start()`.

**Port conflicts:** Use widely spaced ports (e.g., 19000, 19100) and different data directories.


---

**Note:** For development and testing only. Not for production use.
