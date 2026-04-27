package regtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestExampleActivateBIP119 is the worked example for soft-fork testing
// against Bitcoin Inquisition. It mirrors TestExampleActivateTestdummy's
// shape — skip-when-missing, EnsureWallet, GenerateBech32m, MineUntilActive
// — but uses the typed BIPID surface (BIP119 / OP_CHECKTEMPLATEVERIFY) so
// downstream tests don't have to remember Inquisition's deployment-key
// strings ("checktemplateverify").
//
// Unlike testdummy on Core (which transitions DEFINED → STARTED →
// LOCKED_IN → ACTIVE under VBParams), Inquisition activates BIP119 at
// genesis via its "heretical" deployment scheme. So MineUntilActiveBIP
// returns immediately with mined=0 — the API call still validates the
// pathway end-to-end (SupportsBIP → translate BIPID → DeploymentStatus →
// SoftForkActive) without spending blocks.
//
// Prerequisites:
//   - bitcoind-inquisition on PATH (auto-detected) OR Config.BinaryPath
//     pointing at a built Inquisition binary. See the README for build
//     instructions.
//
// On a Core-only machine the test t.Skip's cleanly.
func TestExampleActivateBIP119(t *testing.T) {
	// 1) Start a node. Default Config relies on the PATH auto-detect chain
	//    (bitcoind-inquisition first, fallback to bitcoind). Use a
	//    dedicated port so this example doesn't collide with other tests.
	rt, err := New(&Config{
		Host:    "127.0.0.1:19850",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "regtest"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 2) Skip-when-missing. Core's getdeploymentinfo doesn't list
	//    "checktemplateverify"; SupportsBIP(BIP119) returns false there
	//    and the test skips with a clear reason. This is the canonical
	//    pattern downstream consumers should copy for any test that needs
	//    an Inquisition-only deployment.
	supports, err := rt.SupportsBIPContext(ctx, BIP119)
	if err != nil {
		t.Fatalf("SupportsBIP(BIP119): %v", err)
	}
	if !supports {
		v, _ := rt.VariantContext(ctx)
		t.Skipf("BIP119 not advertised by this bitcoind variant (%s); see README for Inquisition install", v)
	}

	// 3) Set up a miner wallet. Coinbase rewards land here; for the
	//    activation path on Inquisition we don't actually need them, but
	//    keeping the wallet step makes this example a drop-in template
	//    for tests that DO need to broadcast CTV-spending transactions.
	if err := rt.EnsureWallet("miner"); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt.GenerateBech32m("miner")
	if err != nil {
		t.Fatalf("GenerateBech32m: %v", err)
	}

	// 4) Inspect the initial state. On Inquisition, BIP119's deployment
	//    record is type="heretical" with active=true since genesis. The
	//    convenience accessor reports SoftForkActive on either Inquisition
	//    or Core (if Core ever ships CTV-as-buried, this still works).
	initial, err := rt.DeploymentStatusContext(ctx, "checktemplateverify")
	if err != nil {
		t.Fatalf("DeploymentStatus(initial): %v", err)
	}
	t.Logf("BIP119 initial status: %s", initial)

	// 5) Drive activation. On Inquisition this short-circuits with mined=0
	//    because the deployment is already SoftForkActive at genesis. On a
	//    hypothetical Core build that exposes BIP119 as BIP9, the same
	//    call would mine through the activation windows. The single line
	//    is the whole point of the typed API:
	mined, err := rt.MineUntilActiveBIPContext(ctx, BIP119, miner, 2000)
	if err != nil {
		t.Fatalf("MineUntilActiveBIP(BIP119): %v", err)
	}
	t.Logf("MineUntilActiveBIP(BIP119) finished after %d blocks", mined)

	// 6) Final assertion. The post-condition is the same regardless of
	//    how activation happened.
	final, err := rt.DeploymentStatusContext(ctx, "checktemplateverify")
	if err != nil {
		t.Fatalf("DeploymentStatus(final): %v", err)
	}
	if final != SoftForkActive {
		t.Fatalf("BIP119 final status = %s, want SoftForkActive", final)
	}

	// 7) Bonus diagnostic — log what variant we ran against and the BIP119
	//    metadata from the registry. Useful in CI logs when triaging which
	//    binary the suite picked up.
	v, _ := rt.VariantContext(ctx)
	deps, _ := rt.ListDeploymentsContext(ctx)
	for _, d := range deps {
		if d.BIP == BIP119 {
			t.Logf("variant=%s BIP119 type=%s active=%v doc=%s",
				v, d.Type, d.Active, d.DocURL)
			return
		}
	}
	// Should not reach here — SupportsBIP confirmed BIP119 was advertised.
	t.Errorf("BIP119 missing from ListDeployments output (impossible after SupportsBIP=true)")
}

// TestVariantDetection is the smoke test for Variant(): start a node, assert
// the variant resolves to Core or Inquisition (not Unknown — Unknown means
// the subversion parser regressed), and log the value so CI output records
// which binary the suite ran against.
//
// Whichever bitcoind happens to be on PATH (or whatever Config.BinaryPath
// points at) is a valid answer; the test pins only that the parse path
// succeeds and the variant is one of the known good values.
func TestVariantDetection(t *testing.T) {
	rt, err := New(&Config{
		Host:    "127.0.0.1:19852",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "regtest"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	v, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant: %v", err)
	}
	t.Logf("running against variant: %s (binary: %s)", v, rt.bitcoindPath)

	switch v {
	case VariantCore, VariantInquisition:
		// Either is a valid runtime answer.
	case VariantUnknown:
		t.Errorf("Variant() returned VariantUnknown — getnetworkinfo subversion parser regressed?")
	default:
		t.Errorf("Variant() returned unrecognized value: %d", v)
	}
}
