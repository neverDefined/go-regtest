---
description: Scaffold a new RPC wrapper following the project's conventions (Foo + FooContext via call[T], plain godoc, tests).
allowed-tools: Read, Edit, Bash, Grep, Glob
---

You're adding an RPC wrapper to go-regtest. The user invoked: `/add-rpc-method $ARGUMENTS`.

Arguments: `<GoMethodName> <rpcMethodName> [target-file]`, e.g. `/add-rpc-method GetMempoolInfo getmempoolinfo tx.go`.

## Steps

1. **Pick the file.** Use the third argument, or infer it from the concern: `wallet.go`, `tx.go`, `chain.go`, `block.go`, `mining.go`, `peer.go`, `reorg.go`, `time.go`. Read it, `rpc.go`, and CLAUDE.md.
2. **Check the RPC's real shape** on the running Core version (`bitcoin-cli help <rpc>`, or call it), in particular the parameter order and the result JSON.
3. **Write both variants.** `Foo(args)` is a one-line wrapper around `FooContext(context.Background(), args)`. `FooContext` validates its arguments, then calls `call[T](ctx, r, "rpcname", args...)` (or `callHash`, or `rawRPC` when the result is null) and wraps errors as `fmt.Errorf("rpcname: %w", err)`. Return a btcjson/wire type if it matches the JSON; otherwise define a small struct with only stable fields. Use the hex helpers in `encode.go`.
4. **Wallet RPCs:** write the body as a helper taking a `caller`, so both `Regtest` and `Wallet` can expose it.
5. **godoc:** follow CLAUDE.md's style. One sentence for what it does, plus only the non-obvious facts. The Context variant gets one line.
6. **Tests:** a happy-path test with `t.Parallel()` and `StartT`, plus validation and not-connected cases in a table. Put them in the test file that mirrors the source file.
7. **Verify:** `go build ./... && go vet ./... && make lint`, then `ulimit -n 4096 && go test -run '^TestYourTest' -race -v .`
8. **Report** the files and symbols you added and any decisions. Don't commit or open a PR.
