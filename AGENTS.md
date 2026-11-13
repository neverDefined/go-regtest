# go-regtest

A Go library that wraps a local `bitcoind` regtest node as a test fixture.
One package at the repo root (`package regtest`), split into files by
concern. The code is the source of truth; this file holds only what it
cannot tell you.

## Gate

`make gate` must pass before anything merges. CI runs the same command.
It checks formatting and `go.mod` tidiness, runs vet and golangci-lint,
then the race-enabled test suite with a 70% coverage floor.

- It needs `bitcoind` in `PATH` and golangci-lint v2 (`make install-tools`).
- The gate sets `REGTEST_REQUIRE_BITCOIND=1`, so a missing `bitcoind`
  fails instead of skipping.
- Tests for Inquisition-only deployments skip on Bitcoin Core, and a few
  Core-only tests skip on Inquisition. Report those as not run.
- While iterating: `go test -race -run <TestName> .`
- `make vuln` is separate and informational. govulncheck flags standard
  library CVEs of whichever Go runs it, which this repo cannot fix.

Open PRs; do not merge them. The maintainer reviews and merges.

## Invariants

Do not change these without discussion.

1. **Two mutexes.** `mu` guards lifecycle operations. `clientMu` guards the
   RPC client pointer: reads take `RLock`, `Start` and `Stop` take `Lock`.
   Use `lockedClient()`; never read `r.client` directly.
2. **`Config` is immutable from outside.** `New()` and `Config()` return
   copies. Do not expose `r.config`.
3. **No Docker.** bitcoind is a subprocess run by the embedded
   `scripts/bitcoind_manager.sh`.
4. **`IsRunning()` works after `Cleanup()`.** It probes the RPC port
   directly and must not depend on the script.
5. **Public behaviour is preserved.** Pre-1.0 but in use: add methods and
   options, do not break signatures.

## Conventions

- Wrap errors with `fmt.Errorf("context: %w", err)`, always `%w`.
- RPC methods called before `Start()` return `errNotConnected`. Test for it
  with `errors.Is`, never by string.
- Every public RPC method `Foo` has a `FooContext(ctx, ...)` that does the
  work; `Foo` is a one-line `context.Background()` wrapper.
- Call JSON-RPC through `r.rawRPC(ctx, "method", args...)`. Wrap a typed
  btcd `rpcclient` call in `runWithContext`, because btcd's calls block and
  take no `ctx`. Prefer `rawRPC` where Bitcoin Core 26+ changed a result
  shape that btcd's typed call still expects.
- Every exported symbol has godoc in the `Parameters: / Returns: /
  Example:` shape of its neighbours.
- A new RPC wrapper comes with a happy-path test and at least one
  validation or error-path test.
- Tests that start their own node pick an unused port from 19200 up and a
  `DataDir` under `t.TempDir()`. RPC functional tests share the default
  `18443`.
