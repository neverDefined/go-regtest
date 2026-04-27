package regtest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestMineUntilActive_Testdummy is the streamlined version of
// TestExampleActivateTestdummy: same harness, same testdummy deployment,
// but the mining loop is replaced with a single MineUntilActive call.
// Pins that the helper composes correctly with VBParams + WarpContext +
// DeploymentStatusContext.
func TestMineUntilActive_Testdummy(t *testing.T) {
	rt, err := New(testdummyConfig(t, 19900))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// Inquisition exposes a testdummy entry but it isn't BIP9-overridable via
	// -vbparams the way Core's is, so MineUntilActive can't drive it to
	// Active. PR2's SupportsBIP will replace this Variant check with a
	// BIP-aware skip.
	if v, _ := rt.Variant(); v == VariantInquisition {
		t.Skip("testdummy not driveable via -vbparams on Inquisition")
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

	mined, err := rt.MineUntilActiveContext(ctx, "testdummy", miner, 2000)
	if err != nil {
		t.Fatalf("MineUntilActive: %v", err)
	}
	t.Logf("testdummy activated after %d blocks", mined)

	status, err := rt.DeploymentStatus("testdummy")
	if err != nil {
		t.Fatalf("DeploymentStatus: %v", err)
	}
	if status != SoftForkActive {
		t.Errorf("post-MineUntilActive status = %v, want SoftForkActive", status)
	}
}

// TestMineUntilActive_UnknownDeployment pins the early-exit contract:
// MineUntilActive returns ErrUnknownDeployment without mining a single
// block when the deployment name isn't known to bitcoind. This is the
// signal a soft-fork-specific test should use to t.Skip cleanly when
// run against mainline Core.
func TestMineUntilActive_UnknownDeployment(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet("miner"); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt.GenerateBech32("miner")
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	startHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}

	mined, err := rt.MineUntilActive("does-not-exist", miner, 1000)
	if err == nil {
		t.Fatal("expected ErrUnknownDeployment, got nil")
	}
	if !errors.Is(err, ErrUnknownDeployment) {
		t.Errorf("expected errors.Is(err, ErrUnknownDeployment), got %v", err)
	}
	if mined != 0 {
		t.Errorf("expected 0 blocks mined for unknown deployment, got %d", mined)
	}
	endHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if endHeight != startHeight {
		t.Errorf("unknown-deployment path mined blocks: %d -> %d", startHeight, endHeight)
	}
}

// TestExampleActivateTestdummy is a narrated end-to-end example of
// activating Bitcoin Core's "testdummy" BIP9 soft-fork deployment from
// scratch. testdummy is the deployment Core ships specifically for testing
// the activation machinery — it requires no new consensus code, just
// exercises the version-bit signaling state machine end-to-end.
//
// Once this test passes, the same template (with a different deployment
// name and a patched bitcoind binary in $PATH) is the recipe for testing
// real future soft-forks: SIGHASH_ANYPREVOUT (eltoo, see #25), CTV
// (LNHANCE), CSFS, and so on. See the Phase 5 roadmap (#83) for the full
// picture.
//
// The BIP9 state machine has four interesting transitions:
//
//	DEFINED → STARTED:    once the median-time-past >= start_time, at the
//	                       next retarget boundary (regtest period is 144
//	                       blocks).
//	STARTED → LOCKED_IN:  if at least 108 of the previous 144 blocks
//	                       signaled the deployment's bit, at the next
//	                       retarget boundary.
//	LOCKED_IN → ACTIVE:   automatically at the next retarget boundary
//	                       after lock-in.
//	any → FAILED:         if the timeout passes without lock-in.
//
// On regtest with our VBParams (start_time=0, timeout=far-future) and
// Bitcoin Core's default block-version policy that signals for all
// currently-STARTED deployments, activation completes in ~3 retarget
// windows (~432 blocks).
func TestExampleActivateTestdummy(t *testing.T) {
	// 1) Start a node with testdummy configured for fast activation and
	//    -acceptnonstdtxn=1. testdummyConfig produces:
	//
	//      VBParams: [{testdummy, start=0, timeout=9999999999, min=0}]
	//      AcceptNonstdTxn: true
	//
	//    See the testdummyConfig helper in regtest_test.go.
	rt, err := New(testdummyConfig(t, 19800))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	// Inquisition exposes a testdummy entry but it isn't BIP9-overridable via
	// -vbparams the way Core's is. PR2's SupportsBIP will replace this
	// Variant check with a BIP-aware skip.
	if v, _ := rt.Variant(); v == VariantInquisition {
		t.Skip("testdummy not driveable via -vbparams on Inquisition")
	}

	// 2) Set up a wallet so coinbase rewards have somewhere to land while
	//    we mine activation blocks.
	if err := rt.EnsureWallet("miner"); err != nil {
		t.Fatalf("EnsureWallet: %v", err)
	}
	miner, err := rt.GenerateBech32("miner")
	if err != nil {
		t.Fatalf("GenerateBech32: %v", err)
	}

	// 3) Initial status check. On a fresh regtest node testdummy is
	//    typically reported as DEFINED until the first retarget boundary,
	//    or STARTED if Core has already evaluated the start time. Either
	//    is acceptable as a starting point — Active would mean the
	//    deployment was somehow already activated, which would defeat the
	//    purpose of this test.
	initial, err := rt.DeploymentStatus("testdummy")
	if err != nil {
		t.Fatalf("DeploymentStatus initial: %v", err)
	}
	t.Logf("initial status: %s", initial)
	if initial == SoftForkActive {
		t.Fatalf("testdummy is unexpectedly active on a fresh node (status=%s)", initial)
	}

	// 4) Mine in 144-block (one retarget window) chunks, observing the
	//    state machine progress after each. GenerateToAddress (under
	//    Warp) signals for all currently-STARTED BIP9 deployments by
	//    default on regtest, so every block counts toward the threshold.
	//    Pre-allocate `observed` so unparam doesn't trip on the append.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const window = 144
	const maxWindows = 10
	observed := []SoftForkStatus{initial}

	for w := 1; w <= maxWindows; w++ {
		if err := rt.WarpContext(ctx, window, miner); err != nil {
			t.Fatalf("Warp window %d: %v", w, err)
		}
		status, err := rt.DeploymentStatusContext(ctx, "testdummy")
		if err != nil {
			t.Fatalf("DeploymentStatus window %d: %v", w, err)
		}
		t.Logf("window %d (block %d): %s", w, w*window, status)
		if observed[len(observed)-1] != status {
			observed = append(observed, status)
		}
		if status == SoftForkActive {
			break
		}
		if status == SoftForkFailed {
			t.Fatalf("testdummy reached SoftForkFailed unexpectedly")
		}
	}

	// 5) Final assertion. If we hit maxWindows without activating, the
	//    bitcoind binary either doesn't signal by default or doesn't
	//    know testdummy.
	final, err := rt.DeploymentStatus("testdummy")
	if err != nil {
		t.Fatalf("DeploymentStatus final: %v", err)
	}
	if final != SoftForkActive {
		t.Fatalf("testdummy did not reach SoftForkActive within %d windows (final=%s, observed=%v)",
			maxWindows, final, observed)
	}
	t.Logf("testdummy activated successfully — observed transitions: %v", observed)
}

// TestExampleTimeoutWithoutLockin is the worked example for the BIP9 failure
// path: a deployment whose timeout passes before the signaling threshold is
// met transitions to SoftForkFailed. The Phase 6 time API (WarpTime + sticky
// mocktime) makes this observable in seconds rather than waiting through
// real-world time.
//
// On Bitcoin Core's BIP9 state machine, the transitions are:
//
//	at boundary 144:  DEFINED → STARTED   (MTP >= start_time)
//	at boundary 288:  STARTED → FAILED    (MTP >= timeout, no lock-in)
//
// So we need to mine to height 288 with MTP past the configured timeout.
// This test sets a far-future timeout, uses WarpTime to push MTP past it,
// then mines through the second retarget boundary.
//
// Inquisition replaces BIP9 with its "heretical" state machine which doesn't
// honor -vbparams overrides for testdummy, so the test skips there. PR2's
// SupportsBIP could substitute once we add a BIP for testdummy, but the
// Variant check is sufficient and self-documenting.
func TestExampleTimeoutWithoutLockin(t *testing.T) {
	const timeout = 4_000_000_000 // year ~2096; under the uint32 block-timestamp cap

	cfg := &Config{
		Host:    "127.0.0.1:19851",
		User:    "user",
		Pass:    "pass",
		DataDir: filepath.Join(t.TempDir(), "regtest"),
		// -blockversion=0x20000000 (536870912) is the BIP9 baseline with no
		// deployment bits set. Without it, regtest's default block-version
		// policy signals for every STARTED deployment, so testdummy would
		// LOCKED_IN before the timeout check (Bitcoin Core's BIP9 evaluates
		// signaling threshold before the timeout). Suppressing signaling is
		// the only way to demonstrate the FAILED transition.
		ExtraArgs: []string{"-blockversion=536870912"},
		VBParams: []VBParam{{
			Deployment:          "testdummy",
			StartTime:           0,
			Timeout:             timeout,
			MinActivationHeight: 0,
		}},
		AcceptNonstdTxn: true,
	}
	rt, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Stop()

	if v, _ := rt.Variant(); v == VariantInquisition {
		t.Skip("testdummy not driveable via -vbparams on Inquisition (heretical state machine)")
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

	// Pre-fill 11 blocks so MTP has a populated window before we warp.
	if err := rt.WarpContext(ctx, 11, miner); err != nil {
		t.Fatalf("Warp pre-fill: %v", err)
	}

	initial, err := rt.DeploymentStatusContext(ctx, "testdummy")
	if err != nil {
		t.Fatalf("DeploymentStatus initial: %v", err)
	}
	t.Logf("initial status: %s", initial)
	if initial == SoftForkFailed {
		t.Fatalf("testdummy started in SoftForkFailed; expected DEFINED/STARTED on a pre-warp chain")
	}

	// Compute a duration that pushes MTP comfortably past timeout regardless
	// of system time. WarpTime sets mocktime sticky, so subsequent Warp /
	// MineToHeight calls also stamp blocks at the warped time — keeping the
	// MTP window populated past timeout for the BIP9 evaluation at 288.
	preInfo, err := rt.GetBlockChainInfoContext(ctx)
	if err != nil {
		t.Fatalf("GetBlockChainInfo: %v", err)
	}
	need := time.Duration(timeout-preInfo.MedianTime+86400) * time.Second // +1 day cushion
	newMTP, err := rt.WarpTimeContext(ctx, need, miner)
	if err != nil {
		t.Fatalf("WarpTime: %v", err)
	}
	t.Logf("warped MTP from %d to %d (timeout=%d)", preInfo.MedianTime, newMTP, timeout)
	if newMTP < timeout {
		t.Fatalf("WarpTime produced MTP=%d, still below timeout=%d", newMTP, timeout)
	}

	// Mine to the second retarget boundary so BIP9 evaluates STARTED → FAILED.
	// Mocktime is still set from WarpTime, so all new blocks stamp at the
	// warped time — MTP at 288 stays past timeout.
	if err := rt.MineToHeightContext(ctx, 288, miner); err != nil {
		t.Fatalf("MineToHeight 288: %v", err)
	}

	final, err := rt.DeploymentStatusContext(ctx, "testdummy")
	if err != nil {
		t.Fatalf("DeploymentStatus final: %v", err)
	}
	t.Logf("final status at height 288: %s", final)
	if final != SoftForkFailed {
		t.Fatalf("testdummy final status = %s, want SoftForkFailed", final)
	}
}
