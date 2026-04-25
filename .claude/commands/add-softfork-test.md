---
description: Scaffold a BIP9 soft-fork activation test for a named deployment, modeled on TestExampleActivateTestdummy.
allowed-tools: Read, Edit, Bash, Grep, Glob
---

You're adding a new BIP9 soft-fork activation test to go-regtest. The user invoked: `/add-softfork-test $ARGUMENTS`.

Expected arguments: `<deployment-name> [port]`

Examples:
- `/add-softfork-test anyprevout` (port auto-picked from 196xx+ range)
- `/add-softfork-test checktemplateverify 19850`
- `/add-softfork-test csfs`

The deployment name is the lowercase identifier bitcoind uses (matches the BIP9 deployment name in `getdeploymentinfo`'s `deployments` map). For real soft-forks this means pointing `bitcoind` in `$PATH` at a patched binary that knows the deployment (e.g. bitcoin-inquisition for APO/CTV/CSFS).

## Steps

1. **Parse the arguments.** First token is the deployment name — must be lowercase ASCII, no spaces. Reject anything else with a clear error message. Second token (optional) is the RPC port. If omitted, pick the next unused port in the 196xx+ range by grepping existing tests for `127.0.0.1:19[6-9]` and `127.0.0.1:20[0-9]` patterns.

2. **Read the template.** Open `examples_test.go` and read `TestExampleActivateTestdummy` plus its narrated comments — that test is the canonical template. Also read `examples_test.go`'s `TestMineUntilActive_Testdummy` for the streamlined pattern that uses `MineUntilActiveContext`. Read `CLAUDE.md` for repo conventions.

3. **Pick the right helper.**
   - If the test only needs to verify activation reaches `SoftForkActive`, use the streamlined `MineUntilActiveContext` shape from `TestMineUntilActive_Testdummy`.
   - If the test should narrate the BIP9 state-machine transitions (DEFINED → STARTED → LOCKED_IN → ACTIVE), use the windowed-loop shape from `TestExampleActivateTestdummy`.

   Default to the streamlined shape unless the user explicitly asks for the narrated version.

4. **Append to `regtest_rpc_test.go`.** Tests for the in-tree `regtest` package — no `regtest.` prefix on types. Use the snippet below as a starting point, substituting `<deployment>` for the deployment name (use the literal lowercase string everywhere) and `<Deployment>` for a PascalCase Go-identifier-safe form (strip non-alphanumerics, capitalize words). Also substitute `<PORT>` with the chosen port.

```go
// TestExampleActivate<Deployment> is the BIP9 activation test for the
// "<deployment>" soft-fork deployment. Skips cleanly if the bitcoind
// binary in $PATH doesn't know the deployment — point at a patched
// binary (e.g. bitcoin-inquisition) to see real activation.
func TestExampleActivate<Deployment>(t *testing.T) {
	rt, err := New(&Config{
		Host:            "127.0.0.1:<PORT>",
		User:            "user",
		Pass:            "pass",
		DataDir:         filepath.Join(t.TempDir(), "regtest"),
		AcceptNonstdTxn: true,
		VBParams:        []VBParam{VBAlwaysActive("<deployment>")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// Skip-if-unknown: clean degradation when run against a stock
	// bitcoind that doesn't know this deployment.
	if _, err := rt.DeploymentStatus("<deployment>"); errors.Is(err, ErrUnknownDeployment) {
		t.Skipf("bitcoind doesn't expose '<deployment>': %v", err)
	} else if err != nil {
		t.Fatalf("DeploymentStatus: %v", err)
	}

	if err := rt.EnsureWallet("miner"); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt.GenerateBech32("miner")
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mined, err := rt.MineUntilActiveContext(ctx, "<deployment>", miner, 2000)
	if err != nil {
		t.Fatalf("MineUntilActive: %v", err)
	}
	t.Logf("'<deployment>' activated after %d blocks", mined)

	status, err := rt.DeploymentStatus("<deployment>")
	if err != nil {
		t.Fatalf("DeploymentStatus final: %v", err)
	}
	if status != SoftForkActive {
		t.Errorf("post-MineUntilActive status = %v, want SoftForkActive", status)
	}
}
```

5. **Imports.** Confirm `context`, `errors`, `path/filepath`, `testing`, `time` are present in `regtest_rpc_test.go`. Add any that are missing.

6. **Verify.** Run:
   - `go build ./...`
   - `go vet ./...`
   - `make lint`
   - `ulimit -n 4096 && go test -run TestExampleActivate<Deployment> -race -v -timeout 90s .`

   The first run typically prints `--- SKIP: ... bitcoind doesn't expose ...` against a stock bitcoind. That is the success signal that the test compiles and the skip-guard works. To see real activation, the user needs to put a patched bitcoind in `$PATH`.

7. **Report what you added** (file path + new symbol name + chosen port + skip-vs-active result) and any decisions you made. Do not commit or open a PR — the maintainer reviews changes manually.
