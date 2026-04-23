# go-regtest — Claude orientation

This file gives Claude Code the context it needs to make confident edits to this repo. Keep it short; rely on the code as the source of truth.

## What this is

A Go library that wraps a local `bitcoind` regtest node so other Go projects can use it as a test fixture. Single package, MIT licensed, pre-1.0, solo-maintained.

## Architecture

- One package at the repo root (`package regtest`).
- Code is split by concern:
  - `regtest.go` — `Config`, `Regtest`, lifecycle (`Start`/`Stop`/`Cleanup`/`IsRunning`), internal helpers
  - `rpc.go` — `Client`, `GetBlockCount`, `HealthCheck`, `rawRPC`, `lockedClient`, `runWithContext`
  - `wallet.go` — `CreateWallet`, `LoadWallet`, `UnloadWallet`, `EnsureWallet`, `GetWalletInformation`
  - `address.go` — `GenerateBech32`, `GenerateBech32m`, shared `generateAddress`
  - `mining.go` — `Warp`
  - `tx.go` — `SendToAddress`, `GetTxOut`, `ScanTxOutSetForAddress`, `SignRawTransactionWithWallet`, `BroadcastTransaction`, plus `ScantxoutsetUnspent` / `ScantxoutsetResult` types
- `scripts/bitcoind_manager.sh` is embedded via `//go:embed`, extracted to a temp dir at `New()` time, and invoked as `bash <path>`. It manages the bitcoind subprocess.
- The library talks to bitcoind via `btcsuite/btcd/rpcclient` over JSON-RPC. No Docker.

## Invariants — do not violate without discussion

1. **Thread safety via dual mutex.** `mu` (`sync.Mutex`) guards lifecycle ops. `clientMu` (`sync.RWMutex`) guards the RPC client pointer slot. Reads acquire `clientMu.RLock`; writes (`Start`, `Stop`) acquire `clientMu.Lock`. Use `lockedClient()` rather than reaching into `r.client` directly.
2. **`Config` is immutable from the outside.** `New()` and `Config()` return defensive copies. Don't expose direct access to `r.config`.
3. **No Docker.** bitcoind is a subprocess via the embedded shell script. Don't add container deps.
4. **`IsRunning()` works after `Cleanup()`.** It probes the RPC port directly (Phase 1.3). Don't reintroduce a script dependency in that path.
5. **Public method behavior is preserved.** Pre-1.0 but the library is in use; favor additive changes (new methods, new options) over breaking signatures.

## Conventions

- **Error wrapping:** always `fmt.Errorf("context: %w", err)`. The wrap (`%w`) is non-negotiable so `errors.Is` / `errors.As` work upstream.
- **`errNotConnected`:** the canonical sentinel returned by RPC methods when called before `Start()`. Use `errors.Is(err, errNotConnected)` to test for it; don't string-match.
- **`context.Context` variants:** every public RPC method has a `Context`-suffixed variant (`FooContext`). The non-ctx version is a thin `context.Background()` wrapper. New public RPC methods MUST follow this pattern.
- **Cancellation:** `runWithContext[T]` (in `rpc.go`) is the helper. btcd's `rpcclient.RawRequest` is blocking and doesn't accept `ctx` — `runWithContext` runs `fn` in a goroutine and selects on `ctx.Done()`. Pre-cancelled `ctx` is short-circuited with `ctx.Err()`.
- **godoc on every exported symbol.** Existing methods follow a `Parameters: / Returns: / Example:` shape. Match that voice on additions.
- **Test files:** `regtest_test.go` (lifecycle, validation, ctx tests) and `regtest_rpc_test.go` (RPC functional tests). Add to whichever matches the topic.
- **Port allocation in tests:** lifecycle tests that need their own port use `192xx` / `193xx` / `194xx` / `195xx` (see existing `Test_*` for examples). RPC functional tests share the default `18443`.

## How to add a new RPC wrapper

1. Add the typed wrapper in the file matching its concern (`wallet.go`, `tx.go`, etc.).
2. Add both `Foo()` and `FooContext(ctx, ...)`. The non-ctx form is a one-line `context.Background()` wrapper.
3. Use `r.rawRPC(ctx, "rpcMethodName", arg1, arg2, ...)` if calling JSON-RPC directly, or `runWithContext(ctx, func() { ... })` if calling a typed btcsuite method.
4. Wrap errors with `fmt.Errorf(": %w", err)`.
5. Add tests to the appropriate `*_test.go` — happy path + at least one validation/error path.
6. Run `make ai-check` before opening a PR.

## Workflow

- `make ai-check` is the green-gate (`fmt + vet + lint + test-race + vuln`). Run it before any commit you intend to ship.
- macOS: tests need `ulimit -n 4096` for bitcoind FDs. Set this in your shell.
- The maintainer reviews and merges all PRs. Open them; do not merge.
- `golangci-lint` is v2 (see `.golangci.yml`). `make install-tools` installs the right version via the official install script.

## Useful project-specific tools

- **Subagents in `.claude/agents/`** — `bitcoin-rpc-expert`, `regtest-test-writer`, `godoc-polisher`. Delegate to them when their description matches.
- **Slash commands** — `/add-rpc-method <GoName> <rpcName>`, `/coverage`.
