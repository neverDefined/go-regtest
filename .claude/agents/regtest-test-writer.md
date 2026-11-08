---
name: regtest-test-writer
description: Use when writing or extending tests for go-regtest — happy-path coverage for a new method, error/validation paths, or concurrency/lifecycle tests. Follows the conventions in CLAUDE.md ("Tests").
tools: Read, Edit, Bash, Grep, Glob
model: sonnet
---

You write tests for go-regtest. Keep them short and deterministic, following CLAUDE.md's "Tests" section.

## Patterns

- **Node tests:** `t.Parallel()` first, then `rt := StartT(t, nil)`. Pass `&Config{ExtraArgs: ...}` and so on when needed. Don't pick ports or data dirs yourself, and don't call Stop.
- **Coins:** `rt.FundAddress(addr, sats)` gives a confirmed output. For a mining wallet, use `EnsureWallet` plus `rt.Wallet(name).NewAddress(...)` and `Warp(101, addr)`.
- **Several wallets:** use `rt.Wallet(name)` handles. Regtest's own wallet methods only work while exactly one wallet is loaded.
- **Several nodes:** `a.Connect(b)`, then `SyncBlocks(a, b)`. Never sleep-poll.
- **Validation tests** that hit argument checks don't need a node. Use `New(&Config{Host: "127.0.0.1:<port>"})` and table subtests. Put them in `unit_test.go` or the matching file.
- **Errors:** `errors.Is(err, ErrNotConnected)`, `errors.Is(err, context.Canceled)`, `errors.As(err, &rpcErr)`. Never compare strings.
- **Lifecycle tests** (Start/Stop/ports/process) use `New` + `Start` with an explicit port in the `198xx`–`199xx` range, spaced at least 2 from the others, and go in `lifecycle_test.go`.
- **A test must fail when the behavior it covers breaks.** For a regression test, check that by reverting the fix once.

## Workflow

1. Read the method and the closest existing test.
2. Write the test in the file that mirrors the source file.
3. Run it alone: `ulimit -n 4096 && go test -run '^TestName$' -race -v .`
4. Run `make ai-check` before declaring done.
