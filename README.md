# go-regtest

Run a real `bitcoind` regtest node from Go tests: wallets, raw transactions, mining, multi-node reorgs, and upcoming soft forks (CTV, ANYPREVOUT, OP_CAT, CSFS, INTERNALKEY) through Bitcoin Inquisition.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26.10%2B-blue.svg)](https://golang.org)
[![CI](https://github.com/neverDefined/go-regtest/workflows/CI/badge.svg)](https://github.com/neverDefined/go-regtest/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/neverDefined/go-regtest.svg)](https://pkg.go.dev/github.com/neverDefined/go-regtest)

## Install

```bash
go get github.com/neverDefined/go-regtest
```

You also need `bitcoind` on your PATH: `brew install bitcoin` on macOS, or a [release tarball](https://bitcoincore.org/en/download/) on Linux. Bitcoin Core 24 or newer; CI tests 30, 31 and 32.

## Quick start

```go
func TestPayment(t *testing.T) {
    t.Parallel()
    rt := regtest.StartT(t, nil) // free ports, temp data dir, stopped when the test ends

    if err := rt.EnsureWallet("alice"); err != nil {
        t.Fatal(err)
    }
    addr, err := rt.Wallet("alice").NewAddress("", regtest.AddressBech32)
    if err != nil {
        t.Fatal(err)
    }

    op, err := rt.FundAddress(addr, 1_000_000) // a confirmed 0.01 BTC output, no setup needed
    if err != nil {
        t.Fatal(err)
    }
    t.Logf("funded %s", op)
}
```

`StartT` skips the test when `bitcoind` isn't installed, and fails it instead when the `CI` environment variable is set. Each node gets its own ports and data directory, so tests can run in parallel. Outside tests, use `regtest.New` with `Start` and `Stop`.

## What you can do

- **Wallets.** `rt.Wallet(name)` returns a handle whose calls go to that wallet, so several wallets can be loaded at once. It covers addresses of every type, sends, balance, UTXOs, signing and funding.
- **Transactions.** Create, fund, sign, decode and broadcast. `TestMempoolAccept` tells you why a tx would be rejected. `GenerateBlock` mines a block with exactly the transactions you give it, skipping mempool policy.
- **Chain and mempool.** Blocks, headers, chain tips, raw transactions, mempool entries, and merkle proofs (`GetTxOutProof` / `VerifyTxOutProof`).
- **Time.** `SetMockTime`, `MineWithTimestamp`, and `WarpTime`, which moves the chain's median time past forward.
- **Several nodes.** `Connect`, `Disconnect`, `SyncBlocks`, and the reorg primitives `InvalidateBlock`, `ReconsiderBlock`, `PreciousBlock`.
- **Soft forks.** See below.

Every RPC method has a `Context` variant. The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/neverDefined/go-regtest).

## Testing soft forks

Stock Bitcoin Core doesn't know about proposed soft forks; [Bitcoin Inquisition](https://github.com/bitcoin-inquisition/bitcoin) does. The library works with either binary. It picks `bitcoind-inquisition` from your PATH if it's there, or you can set `Config.BinaryPath`.

```go
func TestCTVSpend(t *testing.T) {
    rt := regtest.StartT(t, nil)
    if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
        t.Skip("needs bitcoind-inquisition")
    }
    _ = rt.EnsureWallet("miner")
    miner, _ := rt.Wallet("miner").NewAddress("", regtest.AddressBech32m)
    if _, err := rt.MineUntilActiveBIP(regtest.BIP119, miner, 2000); err != nil {
        t.Fatal(err)
    }
    // Build a CTV spend, probe it with TestMempoolAccept, then broadcast
    // it, or confirm it directly with GenerateBlock.
}
```

- **Typed BIPs.** `BIPTestdummy`, `BIPTaproot`, `BIP54`, `BIP118`, `BIP119`, `BIP347`, `BIP348` and `BIP349` are typed constants. `SupportsBIP` checks the running node, so on Core it reports false for the Inquisition-only ones.
- **BIP9 deployments.** `Config.VBParams` sets them up, `DeploymentStatus` reads their state, and `ListDeployments` lists every deployment along with its BIP metadata.
- **Non-standard transactions.** Set `Config.AcceptNonstdTxn` to send consensus-valid but non-standard transactions through the mempool.

Worked examples, as runnable tests:
- [`TestExampleActivateTestdummy`](examples_test.go): DEFINED → STARTED → LOCKED_IN → ACTIVE on Core.
- [`TestExampleTimeoutWithoutLockin`](examples_test.go): the FAILED path, using `WarpTime`.
- [`TestExampleActivateBIP119`](examples_inquisition_test.go): skip-when-missing and activation on Inquisition.
- [`TestExampleReorg`](examples_reorg_test.go): a two-node fork resolved by the longest-chain rule.

### Building Inquisition

```bash
git clone https://github.com/bitcoin-inquisition/bitcoin.git ~/btc/bitcoin-inquisition
cd ~/btc/bitcoin-inquisition
cmake -B build -DBUILD_TESTS=OFF -DBUILD_GUI=OFF
cmake --build build -j"$(nproc)"   # macOS: -j"$(sysctl -n hw.ncpu)"
```

Then either set `Config.BinaryPath` to the binary's absolute path, such as `/home/you/btc/bitcoin-inquisition/build/bin/bitcoind` (Go doesn't expand `~`), or symlink that binary onto your PATH as `bitcoind-inquisition`. `rt.Variant()` reports which one is running.

## Troubleshooting

- **`Not enough file descriptors available` on macOS.** Run `ulimit -n 4096` before `go test` (`make gate` does it for you). bitcoind opens many sockets, and macOS's default limit is 256.
- **A BIP9 deployment locks in instead of failing at its timeout.** Core checks the signaling threshold before the timeout, and regtest blocks signal for every started deployment. Pass `ExtraArgs: []string{"-blockversion=536870912"}` to stop signaling. See `TestExampleTimeoutWithoutLockin`.
- **`bad-cb-locktime` or `Version bits parameters malformed` on Inquisition.** BIP54 is active there by default, and Inquisition only accepts the three-field `-vbparams` form (the library writes that form when `MinActivationHeight` is 0). Skip Core-only tests with `Variant()` or `SupportsBIP()`.
- **`Variant()` returns `VariantUnknown`.** With an error, the `getnetworkinfo` call failed. With a nil error, the node reported an empty subversion; any other subversion without "inquisition" counts as Core.

## Development

```bash
make gate       # what CI runs: gofmt and go.mod checks, vet, golangci-lint, tests with -race, coverage floor
make test-race
make coverage
```

Releases are cut by pushing a `v*` tag. The release workflow runs the gate, then goreleaser. Coding agents read `AGENTS.md` for the conventions (`CLAUDE.md` imports it).

For development and testing only. Not for production use.
