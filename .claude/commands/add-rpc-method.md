---
description: Scaffold a new RPC wrapper following the project's conventions (typed Foo + FooContext, godoc, test stub).
allowed-tools: Read, Edit, Bash, Grep, Glob
---

You're adding a new RPC wrapper to go-regtest. The user invoked: `/add-rpc-method $ARGUMENTS`.

Expected arguments: `<GoMethodName> <rpcMethodName> [target-file]`

Examples:
- `/add-rpc-method GetBlockHash getblockhash` (target file inferred from category)
- `/add-rpc-method ListUnspent listunspent tx.go`

## Steps

1. **Parse the arguments.** First token is the Go method name (PascalCase). Second is the Bitcoin Core JSON-RPC method (lowercase, no spaces). Third (optional) is the target file. If not provided, infer from category: `wallet.go` for wallet ops, `tx.go` for transactions, `address.go` for addresses, `mining.go` for mining, `rpc.go` for general queries.

2. **Read** the chosen target file and one existing wrapper in it to copy the shape. Read `rpc.go` for `rawRPC` / `runWithContext` semantics. Read `CLAUDE.md` for conventions.

3. **Decide call style.**
   - If Bitcoin Core's response is small JSON that maps cleanly to a known struct, use `rawRPC` and `json.Unmarshal` (see `BroadcastTransactionContext` in `tx.go`).
   - If btcsuite has a typed wrapper that doesn't suffer from Bitcoin Core 26+ field-shape issues, use it via `runWithContext` (see `GetBlockCountContext` in `rpc.go`).

4. **Write both variants.** `Foo()` is a one-line `context.Background()` wrapper around `FooContext(ctx, ...)`. The Context variant does the work.

5. **godoc.** Match the surrounding voice (see `EnsureWallet` for a longer example, `GetBlockCount` for a short one).

6. **Add a test.** Append to the appropriate `_test.go`:
   - If validation guards exist (e.g., empty string check), add a `Test_..._ValidationErrors` table test that doesn't need bitcoind.
   - Add a `TestRPC_...` happy-path test that needs a running node, mirrored on `TestRPC_SendToAddress` or similar.

7. **Verify.** Run:
   - `go build ./...`
   - `go vet ./...`
   - `make lint`
   - `ulimit -n 4096 && go test -run <NewTestName> -race -v -timeout 60s .`

8. **Report what you added** (file paths + new symbol names) and any decisions you made (call style, target file). Do not commit or open a PR — the maintainer reviews changes manually.
