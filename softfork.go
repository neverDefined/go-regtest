// Package regtest soft-fork support.
//
// The types and methods in this file expose Bitcoin Core's BIP9 deployment
// state machine to Go callers — the underpinnings for testing future soft
// forks (APO/eltoo, CTV/LNHANCE, CSFS, etc.) deterministically against a
// regtest node.
//
// Minimum bitcoind version: Bitcoin Core 24. The getdeploymentinfo RPC was
// introduced in v24; against older bitcoind builds these methods will surface
// the underlying RPC error from rawRPC and tests can skip cleanly.
package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SoftForkStatus is the typed BIP9 deployment state. It collapses Bitcoin
// Core's getdeploymentinfo "status" string into an enum so callers can match
// without comparing strings.
type SoftForkStatus int

const (
	// SoftForkUnknown is the zero value, returned when the BIP9 status string
	// from bitcoind doesn't match a known state. Should not appear in
	// practice on a healthy node.
	SoftForkUnknown SoftForkStatus = iota
	// SoftForkDefined is the initial BIP9 state — the deployment exists but
	// signaling has not begun.
	SoftForkDefined
	// SoftForkStarted means the deployment has reached its start time and
	// blocks may now signal support.
	SoftForkStarted
	// SoftForkLockedIn means the signaling threshold was met within a
	// retarget window. Activation follows after the lock-in window.
	SoftForkLockedIn
	// SoftForkActive means the deployment's consensus rules are enforced
	// from the activation height onwards. Buried deployments report this
	// status as well (since they are always-active).
	SoftForkActive
	// SoftForkFailed means the deployment did not reach the signaling
	// threshold before its timeout. Terminal — the state machine does not
	// recover from this.
	SoftForkFailed
)

// String returns the BIP9 status string ("defined", "started", "locked_in",
// "active", "failed", or "unknown") matching bitcoind's getdeploymentinfo
// output.
func (s SoftForkStatus) String() string {
	switch s {
	case SoftForkDefined:
		return "defined"
	case SoftForkStarted:
		return "started"
	case SoftForkLockedIn:
		return "locked_in"
	case SoftForkActive:
		return "active"
	case SoftForkFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// parseSoftForkStatus maps a getdeploymentinfo status string onto the typed
// enum, returning SoftForkUnknown for unrecognized values.
func parseSoftForkStatus(s string) SoftForkStatus {
	switch s {
	case "defined":
		return SoftForkDefined
	case "started":
		return SoftForkStarted
	case "locked_in":
		return SoftForkLockedIn
	case "active":
		return SoftForkActive
	case "failed":
		return SoftForkFailed
	default:
		return SoftForkUnknown
	}
}

// ErrUnknownDeployment is returned by DeploymentStatus when the named
// deployment is not present in bitcoind's getdeploymentinfo response.
// This is the signal a test should use to t.Skip when a soft-fork specific
// test is run against a bitcoind binary that doesn't know that deployment
// (e.g. running an APO test against mainline Core).
var ErrUnknownDeployment = errors.New("unknown deployment")

// DeploymentInfo is the typed shape of bitcoind's getdeploymentinfo response.
// It describes the BIP9 deployment state machine evaluated as of a specific
// block hash and height.
type DeploymentInfo struct {
	// Hash is the block hash the deployments were evaluated against.
	Hash string `json:"hash"`
	// Height is the block height the deployments were evaluated against.
	Height int64 `json:"height"`
	// Deployments maps deployment name (e.g. "testdummy", "taproot",
	// "anyprevout") to its current state. The set of names depends on the
	// bitcoind binary in use.
	Deployments map[string]Deployment `json:"deployments"`
}

// Deployment describes a single soft-fork deployment's state. Bitcoin Core
// distinguishes "buried" deployments (active for so long they're embedded in
// the consensus rules — Type=="buried") from "bip9" deployments (still
// signaling and capable of state transitions — Type=="bip9").
type Deployment struct {
	// Type is "bip9" for active BIP9 deployments or "buried" for deployments
	// hard-coded as always active.
	Type string `json:"type"`
	// Active reports whether the deployment is currently enforced as of Hash.
	Active bool `json:"active"`
	// Height is the activation height for buried deployments and the
	// activation block height for active BIP9 deployments. Zero for BIP9
	// deployments that have not yet activated.
	Height int64 `json:"height"`
	// BIP9 carries the BIP9 state machine details for Type=="bip9"; nil for
	// buried deployments.
	BIP9 *BIP9Info `json:"bip9,omitempty"`
}

// BIP9Info is the per-deployment BIP9 state, as reported under the "bip9"
// field of getdeploymentinfo.
type BIP9Info struct {
	// Status is the current BIP9 state: defined, started, locked_in, active,
	// or failed.
	Status string `json:"status"`
	// StatusNext is the projected status at the next retarget boundary
	// (Bitcoin Core 25+).
	StatusNext string `json:"status_next,omitempty"`
	// Bit is the version bit signaled for this deployment.
	Bit int `json:"bit"`
	// StartTime is the unix timestamp at which signaling may begin.
	StartTime int64 `json:"start_time"`
	// Timeout is the unix timestamp after which the deployment fails.
	Timeout int64 `json:"timeout"`
	// MinActivationHeight is the BIP9 minimum-activation-height field.
	MinActivationHeight int32 `json:"min_activation_height"`
	// Since is the height at which the current Status was reached.
	Since int64 `json:"since"`
	// Statistics carries signaling statistics for status=="started"
	// deployments; nil otherwise.
	Statistics *BIP9Statistics `json:"statistics,omitempty"`
}

// BIP9Statistics describes BIP9 signaling progress within the current
// retarget window.
type BIP9Statistics struct {
	// Period is the length of the retarget window in blocks.
	Period int `json:"period"`
	// Threshold is the number of signaling blocks required to lock in.
	Threshold int `json:"threshold"`
	// Elapsed is the number of blocks evaluated so far in the window.
	Elapsed int `json:"elapsed"`
	// Count is the number of signaling blocks observed so far.
	Count int `json:"count"`
	// Possible is true if Threshold is still reachable in the current window.
	Possible bool `json:"possible"`
}

// VBParam configures a single named BIP9 deployment for regtest. Each VBParam
// renders to one -vbparams=<name>:<start>:<timeout>:<min> flag passed to
// bitcoind on Start. Bitcoin Core treats StartTime values -1 and -2 as magic
// sentinels for ALWAYS_ACTIVE and NEVER_ACTIVE respectively (see
// VBAlwaysActive / VBNeverActive).
//
// For test workflows that want to exercise the BIP9 state machine end-to-end
// (DEFINED → STARTED → LOCKED_IN → ACTIVE), set explicit values:
// StartTime=0, Timeout=far-future, MinActivationHeight=0. Using
// VBAlwaysActive collapses the state machine so the deployment reports
// active immediately — useful when an application test only needs the new
// rules on, but it skips the activation observability path.
type VBParam struct {
	// Deployment is the deployment name as known to bitcoind (e.g. "testdummy",
	// "anyprevout", "checktemplateverify"). Must not be empty.
	Deployment string
	// StartTime is the BIP9 nStartTime field. Use 0 for "signaling may begin
	// immediately"; use -1 for ALWAYS_ACTIVE; use -2 for NEVER_ACTIVE.
	StartTime int64
	// Timeout is the BIP9 nTimeout field — a unix timestamp after which the
	// deployment fails. Use a far-future value (e.g. 9999999999) for tests
	// that want unlimited time.
	Timeout int64
	// MinActivationHeight is the BIP9 minimum-activation-height field.
	MinActivationHeight int32
}

// VBAlwaysActive returns a VBParam that tells bitcoind the named deployment
// is always active on regtest (Bitcoin Core's ALWAYS_ACTIVE sentinel,
// StartTime = -1). The BIP9 state machine is collapsed: status reports active
// from block 0 with no signaling required.
func VBAlwaysActive(name string) VBParam {
	return VBParam{Deployment: name, StartTime: -1, Timeout: 0, MinActivationHeight: 0}
}

// VBNeverActive returns a VBParam that tells bitcoind the named deployment is
// never active on regtest (Bitcoin Core's NEVER_ACTIVE sentinel, StartTime =
// -2). Useful for tests that want to verify the soft-fork-disabled path.
func VBNeverActive(name string) VBParam {
	return VBParam{Deployment: name, StartTime: -2, Timeout: 0, MinActivationHeight: 0}
}

// DeploymentStatus returns the typed BIP9 status of a single named deployment.
// Buried deployments (always-active) report SoftForkActive.
//
// Parameters:
//   - name: deployment name as known to bitcoind (e.g. "testdummy", "taproot",
//     "anyprevout").
//
// Returns:
//   - SoftForkStatus: the typed status enum
//   - error: ErrUnknownDeployment (errors.Is compatible) when the deployment
//     is not in the response; errNotConnected before Start; otherwise the
//     wrapped RPC or unmarshal error.
//
// Example:
//
//	status, err := rt.DeploymentStatus("anyprevout")
//	if errors.Is(err, regtest.ErrUnknownDeployment) {
//	    t.Skip("bitcoind doesn't expose 'anyprevout'")
//	}
//	if status != regtest.SoftForkActive {
//	    t.Fatalf("expected APO active, got %v", status)
//	}
func (r *Regtest) DeploymentStatus(name string) (SoftForkStatus, error) {
	return r.DeploymentStatusContext(context.Background(), name)
}

// DeploymentStatusContext is the context-aware variant of DeploymentStatus.
func (r *Regtest) DeploymentStatusContext(ctx context.Context, name string) (SoftForkStatus, error) {
	info, err := r.GetDeploymentInfoContext(ctx)
	if err != nil {
		return SoftForkUnknown, err
	}
	d, ok := info.Deployments[name]
	if !ok {
		return SoftForkUnknown, fmt.Errorf("%w: %q", ErrUnknownDeployment, name)
	}
	// Buried deployments don't carry a BIP9 sub-object; they're hard-coded
	// active.
	if d.BIP9 == nil {
		if d.Active {
			return SoftForkActive, nil
		}
		return SoftForkUnknown, nil
	}
	return parseSoftForkStatus(d.BIP9.Status), nil
}

// waitForDeployment polls DeploymentStatus at ~100ms intervals until the
// named deployment reaches target, ctx expires, or the deployment terminates
// in SoftForkFailed (when target != SoftForkFailed).
//
// This is the polling primitive that MineUntilActive (#71) layers on top of.
// It does not mine blocks — callers are expected to drive chain progress in
// parallel (or rely on a deployment that activates without further input).
func (r *Regtest) waitForDeployment(ctx context.Context, name string, target SoftForkStatus) error {
	const interval = 100 * time.Millisecond
	for {
		status, err := r.DeploymentStatusContext(ctx, name)
		if err != nil {
			return err
		}
		if status == target {
			return nil
		}
		if status == SoftForkFailed && target != SoftForkFailed {
			return fmt.Errorf("deployment %q reached SoftForkFailed (cannot reach %v)", name, target)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// renderExtraArgs builds the slice of bitcoind flags to forward on Start.
// It composes Config.ExtraArgs with one -vbparams=... per VBParam and
// -acceptnonstdtxn=1 when AcceptNonstdTxn is true. The order is stable:
// ExtraArgs first, then VBParams in declaration order, then AcceptNonstdTxn.
func (c *Config) renderExtraArgs() []string {
	args := append([]string(nil), c.ExtraArgs...)
	for _, vb := range c.VBParams {
		args = append(args, fmt.Sprintf("-vbparams=%s:%d:%d:%d",
			vb.Deployment, vb.StartTime, vb.Timeout, vb.MinActivationHeight))
	}
	if c.AcceptNonstdTxn {
		args = append(args, "-acceptnonstdtxn=1")
	}
	return args
}

// GetDeploymentInfo returns the BIP9 soft-fork deployment state evaluated
// against the chain tip. This is the canonical way to inspect activation
// progress for both buried (e.g. "taproot", "segwit") and active BIP9
// deployments (e.g. "testdummy", or — against bitcoin-inquisition builds —
// "anyprevout", "checktemplateverify").
//
// Returns:
//   - *DeploymentInfo: typed deployment state map keyed by deployment name
//   - error: errNotConnected if Start has not been called; otherwise the
//     wrapped RPC or unmarshal error. On Bitcoin Core older than v24 the RPC
//     itself is absent; callers can skip on that error.
//
// Example:
//
//	info, err := rt.GetDeploymentInfo()
//	if err != nil {
//	    return err
//	}
//	if d, ok := info.Deployments["taproot"]; ok && d.Active {
//	    fmt.Println("taproot active at height", d.Height)
//	}
func (r *Regtest) GetDeploymentInfo() (*DeploymentInfo, error) {
	return r.GetDeploymentInfoContext(context.Background())
}

// GetDeploymentInfoContext is the context-aware variant of GetDeploymentInfo.
func (r *Regtest) GetDeploymentInfoContext(ctx context.Context) (*DeploymentInfo, error) {
	raw, err := r.rawRPC(ctx, "getdeploymentinfo")
	if err != nil {
		return nil, fmt.Errorf("getdeploymentinfo: %w", err)
	}
	var info DeploymentInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("unmarshal getdeploymentinfo: %w", err)
	}
	return &info, nil
}
