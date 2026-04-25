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
	"fmt"
)

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
