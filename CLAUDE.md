# go-regtest: notes for Claude

A Go library that runs a local `bitcoind` regtest node as a test fixture for other Go projects. Single package, MIT, pre-1.0, solo-maintained. The code is the source of truth; this file covers what isn't obvious from it.

## Layout

One package at the repo root (`package regtest`), split by concern:

- `regtest.go`: `Config`, `Regtest`, lifecycle (`Start`/`Stop`/`IsRunning`), binary and port resolution, data-dir safety, the readiness probe.
- `process.go` (+ `process_linux.go`, `process_other.go`): the bitcoind child process (launch, output tail, terminate/kill).
- `testing.go`: `StartT`, free-port reservation.
- `rpc.go`: `Client`, `rawRPC`, `call[T]`, `callHash`, the `caller` interface, per-wallet clients, `runWithContext`.
- `encode.go`: tx/block/header hex helpers.
- `wallet.go`: wallet lifecycle and the `Wallet` handle.
- `faucet.go`: `FundAddress`, funded from an in-memory key (no wallet), confirmed with `GenerateBlock`.
- `address.go`: `AddressType`, `GenerateBech32[m]`.
- `tx.go`: raw transactions, mempool, merkle proofs, `TestMempoolAccept`.
- `chain.go`, `block.go`, `reorg.go`, `mining.go`, `time.go`, `peer.go`: chain reads, block templates and `GenerateBlock`, reorg primitives, mining, mocktime, peers and `SyncBlocks`.
- `softfork.go`, `deployments.go`, `variant.go`: BIP9 state, the BIP registry, Core vs Inquisition.

## How the node runs

- `StartContext` launches bitcoind as a **foreground child** (no `-daemon`, no shell script) and polls readiness over RPC.
- Each Start adds a one-time `-rpcauth` probe credential, and readiness only passes through it, so a different node that grabbed the port can't pass.
- `StopContext` sends SIGTERM to our own PID and kills it on timeout.
- On Linux the child gets `Pdeathsig`. It is launched from a locked OS thread because the signal follows the launching thread.
- The node listens on 127.0.0.1 only: RPC on `Config.Host`'s port and P2P on port+1 (`-bind`, `-listenonion=0`).
- Start wipes `DataDir`. Stop deletes it only if this instance's Start created it. `checkDataDir` refuses `/`, `$HOME`, and the working directory or its parents, compared after resolving symlinks.

## Invariants (discuss before changing)

1. **Two locks.** `mu` guards the lifecycle (`proc`, `ownsDataDir`); `clientMu` guards the client pointers (`client`, `walletClients`). Lock order is `mu` then `clientMu`. RPC code reads clients through `lockedClient()` / `walletClient()` and never holds a lock during an RPC. `FundAddress` serializes on its own context-aware semaphore (`faucetSem`), outside both.
2. **`Config` is immutable from outside.** `New` and `Config()` copy it (`clone`).
3. **No Docker and no shell scripts.** bitcoind is a child process managed from Go.
4. **`IsRunning()` probes the RPC port directly**, whether or not this instance started the node.
5. **Public behavior is preserved.** Pre-1.0 but in use: prefer additive changes (new methods, new options) and call out any behavior change in the PR.
6. **btcd stays at v0.25.x.** v0.26+ moves to `/v2` modules, which changes the types in our public API. Dependabot ignores it on purpose.

## Conventions

- **Errors:** wrap with `fmt.Errorf("context: %w", err)`. Sentinels: `ErrNotConnected`, `ErrBinaryNotFound`, `ErrUnknownDeployment`, `ErrUnknownBIP`. Test them with `errors.Is`, never by string.
- **Context variants:** every public RPC method has `Foo` (a one-line `context.Background()` wrapper) and `FooContext`.
- **RPC calls:** use `call[T](ctx, r, "method", args...)` (or `callHash`, or `rawRPC` for null results), not btcd's typed methods. Their result shapes break across Core versions. Return btcjson/wire types where they fit the JSON; otherwise define a small struct with only stable fields.
- **Wallet RPCs** take a `caller`, so the same helper serves `Regtest` (default wallet) and `Wallet` (a named one).
- **godoc:** follow the style below. Short, every sentence earns its place.

### godoc style

1. The first sentence says what it does and starts with the name.
2. Add a sentence only for what the signature doesn't say: the minimum Core version, sentinel errors, side effects (mines, sets mocktime, loads wallets, deletes dirs), what nil means, units, enforced ranges.
3. No `Parameters:`/`Returns:` blocks, no lists of result-struct fields, no filler ("useful for", "simply", "powerful"), no history ("as of", issue numbers), few em dashes.
4. Context variants: `// FooContext is like Foo but takes a context.`
5. Put examples in `example_test.go` as `ExampleXxx` functions (compiled, shown on pkg.go.dev). Use an inline example only where it is the clearest way to show a pattern.
6. Keep comments that explain a non-obvious *why* (Core quirks, btcd pitfalls, races). Delete comments that restate code.

## Tests

- **Node tests** use `rt := StartT(t, nil)` plus `t.Parallel()`: free ports, a temp data dir, cleanup. They're skipped without bitcoind, or fail when `CI` is set.
- **Lifecycle tests** (`lifecycle_test.go`, `process_linux_test.go`, Start/Stop tests) use `New` + `Start` with explicit ports in the `198xx`–`199xx` range, spaced at least 2 apart.
- Tests that don't need bitcoind (parsers, registry, fake-RPC tests) go in `unit_test.go`.
- Test files mirror source files (`chain_test.go`, `tx_test.go`, ...). Prefer table tests for validation cases.
- Every new method gets a happy-path test, plus at least one validation or error case.

## Workflow

- `make ai-check` is the gate: fmt, vet, golangci-lint (v2, see `.golangci.yml`), tests with `-race`, govulncheck. Run it before any commit you intend to ship.
- macOS: run `ulimit -n 4096` before the tests.
- CI tests pinned Bitcoin Core releases (currently 30.4, 31.2 and 32.1 on Linux; 32.1 on macOS) and the minimum Go from `go.mod`.
- The maintainer reviews and merges all PRs. Open them, but don't merge.

## Project tools

- Subagents in `.claude/agents/`: `bitcoin-rpc-expert`, `regtest-test-writer`, `godoc-polisher`.
- Slash commands: `/add-rpc-method <GoName> <rpcName>`, `/add-softfork-test`, `/coverage`.
