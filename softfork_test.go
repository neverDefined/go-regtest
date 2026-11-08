package regtest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func deploymentNames(m map[string]Deployment) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}

// TestDeploymentStatus_Taproot verifies taproot reports active on every
// supported Core: listed as buried up to Core 31, absent from
// getdeploymentinfo (but enforced) from Core 32.
func TestDeploymentStatus_Taproot(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	status, err := rt.DeploymentStatus("taproot")
	if err != nil {
		t.Fatalf("DeploymentStatus(taproot): %v", err)
	}
	if status != SoftForkActive {
		t.Errorf("taproot status = %v (%q), want SoftForkActive", status, status)
	}
	if ok, err := rt.SupportsBIP(BIPTaproot); err != nil || !ok {
		t.Errorf("SupportsBIP(BIPTaproot) = %v, %v; want true", ok, err)
	}
}

// TestDeploymentStatus_Unknown pins the contract that an unrecognized
// deployment name returns ErrUnknownDeployment via errors.Is. Tests that
// target a not-yet-mainline soft-fork (APO, CTV, CSFS) rely on this signal
// to skip cleanly when run against mainline Core.
func TestDeploymentStatus_Unknown(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	_, err := rt.DeploymentStatus("definitely-not-a-real-deployment")
	if err == nil {
		t.Fatal("expected error for unknown deployment, got nil")
	}
	if !errors.Is(err, ErrUnknownDeployment) {
		t.Errorf("expected errors.Is(err, ErrUnknownDeployment), got %v", err)
	}
}

// TestWaitForDeployment_AlreadyActive exercises the unexported polling
// helper waitForDeployment with a target the deployment has already reached
// (taproot=active on a fresh regtest). The helper should return on the
// first poll without sleeping.
func TestWaitForDeployment_AlreadyActive(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rt.waitForDeployment(ctx, "taproot", SoftForkActive); err != nil {
		t.Fatalf("waitForDeployment: %v", err)
	}
}

// TestWaitForDeployment_Cancellation pins that ctx cancellation surfaces
// rather than spinning forever when the target status will not be reached.
func TestWaitForDeployment_Cancellation(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	// Wait for taproot=Defined — taproot is buried/active so this status
	// will never be reported. Cancel after a short wait and confirm the
	// helper surfaces ctx.Err().
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := rt.waitForDeployment(ctx, "taproot", SoftForkDefined)
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx error, got %v", err)
	}
}

// TestGetDeploymentInfo exercises the typed getdeploymentinfo wrapper.
// On a default regtest node we expect entries for the well-known buried
// deployments (taproot, segwit, csv) — taproot is active from block 0 on
// modern Core so its Active flag must be true.
//
// This test requires Bitcoin Core 24+ for the underlying RPC; on older
// builds it will report a clear failure pointing at that minimum version.
func TestGetDeploymentInfo(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	info, err := rt.GetDeploymentInfo()
	if err != nil {
		t.Fatalf("GetDeploymentInfo (requires Bitcoin Core 24+): %v", err)
	}
	if info.Hash == "" {
		t.Error("Hash empty")
	}
	if info.Deployments == nil {
		t.Fatal("Deployments map nil")
	}

	// taproot is not listed: Core 32 dropped it from getdeploymentinfo.
	for _, name := range []string{"segwit", "csv"} {
		d, ok := info.Deployments[name]
		if !ok {
			t.Errorf("missing deployment %q in %v", name, deploymentNames(info.Deployments))
			continue
		}
		if d.Type != "buried" && d.Type != "bip9" {
			t.Errorf("%s: unexpected Type %q", name, d.Type)
		}
	}

	if d, ok := info.Deployments["taproot"]; ok {
		if !d.Active {
			t.Errorf("taproot expected Active=true on modern regtest, got %+v", d)
		}
	}
}

// TestSoftForkStatus_String verifies that SoftForkStatus.String() returns
// the BIP9 status strings bitcoind uses, and that parseSoftForkStatus is its
// inverse — pinning the round-trip contract for the typed enum.
func TestSoftForkStatus_String(t *testing.T) {
	cases := []struct {
		status SoftForkStatus
		s      string
	}{
		{SoftForkDefined, "defined"},
		{SoftForkStarted, "started"},
		{SoftForkLockedIn, "locked_in"},
		{SoftForkActive, "active"},
		{SoftForkFailed, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			if got := tc.status.String(); got != tc.s {
				t.Errorf("String() = %q, want %q", got, tc.s)
			}
			if got := parseSoftForkStatus(tc.s); got != tc.status {
				t.Errorf("parseSoftForkStatus(%q) = %v, want %v", tc.s, got, tc.status)
			}
		})
	}
	if got := SoftForkUnknown.String(); got != "unknown" {
		t.Errorf("SoftForkUnknown.String() = %q, want unknown", got)
	}
	if got := parseSoftForkStatus("garbage"); got != SoftForkUnknown {
		t.Errorf("parseSoftForkStatus(garbage) = %v, want SoftForkUnknown", got)
	}
}
