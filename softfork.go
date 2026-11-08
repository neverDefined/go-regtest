package regtest

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SoftForkStatus is a deployment's BIP9 state, parsed from the "status"
// string in getdeploymentinfo.
type SoftForkStatus int

const (
	// SoftForkUnknown is the zero value, used when bitcoind reports a status
	// this package doesn't recognize, or no BIP9 status for an inactive
	// deployment.
	SoftForkUnknown SoftForkStatus = iota
	// SoftForkDefined is the initial state: the deployment exists but
	// signaling has not started.
	SoftForkDefined
	// SoftForkStarted means the start time has passed and blocks may signal
	// for the deployment.
	SoftForkStarted
	// SoftForkLockedIn means a retarget window met the signaling threshold.
	// The deployment activates after one more window, or later if
	// MinActivationHeight requires it.
	SoftForkLockedIn
	// SoftForkActive means the deployment's rules are enforced. Buried
	// deployments also report it.
	SoftForkActive
	// SoftForkFailed means the timeout passed without lock-in. It is terminal.
	SoftForkFailed
)

// String returns the status as getdeploymentinfo spells it ("defined",
// "started", "locked_in", "active", "failed"), or "unknown".
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

// parseSoftForkStatus maps a getdeploymentinfo status string to a
// SoftForkStatus, or SoftForkUnknown if it isn't recognized.
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

// enforcedWithoutEntry lists deployments that every node with
// getdeploymentinfo enforces but that newer versions leave out of the
// response: Core 32 dropped "taproot". A missing entry means active.
var enforcedWithoutEntry = map[string]bool{"taproot": true}

// ErrUnknownDeployment is returned by DeploymentStatus and MineUntilActive
// when getdeploymentinfo has no deployment with the given name. Tests can
// skip on it when the binary lacks the deployment, for example an APO test
// run against Bitcoin Core.
var ErrUnknownDeployment = errors.New("unknown deployment")

// DeploymentInfo is bitcoind's getdeploymentinfo result: the state of every
// deployment as of one block.
type DeploymentInfo struct {
	// Hash is the hash of the block the deployments were evaluated at.
	Hash string `json:"hash"`
	// Height is the height of that block.
	Height int64 `json:"height"`
	// Deployments maps each deployment name ("testdummy", "taproot",
	// "anyprevout", ...) to its state. The names depend on the bitcoind
	// binary.
	Deployments map[string]Deployment `json:"deployments"`
}

// Deployment is one soft-fork deployment's state. Buried deployments are
// hard-coded as active; BIP9 deployments move through the BIP9 states.
type Deployment struct {
	// Type is "buried" for a deployment hard-coded as active, "bip9" for a
	// BIP9 deployment in any state, or "heretical" on Bitcoin Inquisition.
	Type string `json:"type"`
	// Active reports whether the deployment is enforced as of
	// DeploymentInfo.Hash.
	Active bool `json:"active"`
	// Height is the activation height for buried deployments and active
	// BIP9 deployments; 0 for BIP9 deployments that aren't active yet.
	Height int64 `json:"height"`
	// BIP9 holds the BIP9 state for Type "bip9"; nil for buried
	// deployments.
	BIP9 *BIP9Info `json:"bip9,omitempty"`
}

// BIP9Info is the "bip9" object of a getdeploymentinfo deployment.
type BIP9Info struct {
	// Status is the BIP9 state: "defined", "started", "locked_in", "active"
	// or "failed". DeploymentStatus parses it into a SoftForkStatus.
	Status string `json:"status"`
	// StatusNext is the status the next block will have.
	StatusNext string `json:"status_next,omitempty"`
	// Bit is the version bit blocks set to signal for this deployment.
	Bit int `json:"bit"`
	// StartTime is the Unix time at which signaling may begin.
	StartTime int64 `json:"start_time"`
	// Timeout is the Unix time after which the deployment fails if it hasn't
	// locked in.
	Timeout int64 `json:"timeout"`
	// MinActivationHeight is the BIP9 minimum-activation-height field.
	MinActivationHeight int32 `json:"min_activation_height"`
	// Since is the height at which the current Status was reached.
	Since int64 `json:"since"`
	// Statistics holds signaling progress while Status is "started" or
	// "locked_in"; nil otherwise.
	Statistics *BIP9Statistics `json:"statistics,omitempty"`
}

// BIP9Statistics is signaling progress in the current retarget window.
type BIP9Statistics struct {
	// Period is the length of the retarget window in blocks.
	Period int `json:"period"`
	// Threshold is the number of signaling blocks required to lock in.
	Threshold int `json:"threshold"`
	// Elapsed is the number of blocks evaluated so far in the window.
	Elapsed int `json:"elapsed"`
	// Count is the number of signaling blocks observed so far.
	Count int `json:"count"`
	// Possible reports whether Threshold can still be reached in this window.
	Possible bool `json:"possible"`
}

// VBParam configures one BIP9 deployment for regtest and becomes one
// -vbparams flag on Start. StartTime -1 and -2 are Bitcoin Core's
// ALWAYS_ACTIVE and NEVER_ACTIVE values (see VBAlwaysActive and
// VBNeverActive).
//
// To exercise the whole state machine (defined, started, locked_in, active),
// use StartTime 0, a far-future Timeout and MinActivationHeight 0.
// VBAlwaysActive skips the state machine: the deployment is active from the
// start, which suits tests that only need the new rules on.
type VBParam struct {
	// Deployment is bitcoind's name for the deployment ("testdummy",
	// "anyprevout", "checktemplateverify", ...). New rejects an empty name.
	Deployment string
	// StartTime is the BIP9 start time, in Unix seconds. 0 lets signaling
	// begin at once, -1 means always active, -2 means never active.
	StartTime int64
	// Timeout is the Unix time after which the deployment fails if it hasn't
	// locked in. Use a far-future value such as 9999999999 for no limit.
	Timeout int64
	// MinActivationHeight is the earliest height at which the deployment can
	// activate. When it is non-zero the flag uses the four-field -vbparams
	// form, which Bitcoin Inquisition rejects.
	MinActivationHeight int32
}

// VBAlwaysActive returns a VBParam that makes the named deployment active
// from block 0, with no signaling (Bitcoin Core's ALWAYS_ACTIVE, StartTime
// -1).
func VBAlwaysActive(name string) VBParam {
	return VBParam{Deployment: name, StartTime: -1, Timeout: 0, MinActivationHeight: 0}
}

// VBNeverActive returns a VBParam that keeps the named deployment inactive
// (Bitcoin Core's NEVER_ACTIVE, StartTime -2), for testing the path where the
// soft fork is off.
func VBNeverActive(name string) VBParam {
	return VBParam{Deployment: name, StartTime: -2, Timeout: 0, MinActivationHeight: 0}
}

// DeploymentStatus returns the BIP9 status of the named deployment.
// Deployments without BIP9 state, such as buried ones, report SoftForkActive
// when active and SoftForkUnknown otherwise. It returns ErrUnknownDeployment
// if bitcoind doesn't know the name.
func (r *Regtest) DeploymentStatus(name string) (SoftForkStatus, error) {
	return r.DeploymentStatusContext(context.Background(), name)
}

// DeploymentStatusContext is like DeploymentStatus but takes a context.
func (r *Regtest) DeploymentStatusContext(ctx context.Context, name string) (SoftForkStatus, error) {
	info, err := r.GetDeploymentInfoContext(ctx)
	if err != nil {
		return SoftForkUnknown, err
	}
	d, ok := info.Deployments[name]
	if !ok {
		if enforcedWithoutEntry[name] {
			return SoftForkActive, nil
		}
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

// waitForDeployment polls DeploymentStatus every 100ms until the named
// deployment reaches target, ctx is done, or the deployment reaches
// SoftForkFailed (when target isn't SoftForkFailed). It doesn't mine: the
// caller must advance the chain, or the deployment must change state without
// new blocks.
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

// renderExtraArgs returns the bitcoind flags that come from the Config, in
// order: ExtraArgs, one -vbparams per VBParam, then -acceptnonstdtxn=1 if
// AcceptNonstdTxn is set.
//
// A VBParam renders in the three-field form (deployment:start:timeout) unless
// MinActivationHeight is non-zero, which needs the four-field form
// (deployment:start:timeout:min_activation_height). Bitcoin Core 24+ accepts
// both, but Bitcoin Inquisition only accepts three fields, so the three-field
// default works with either binary.
func (c *Config) renderExtraArgs() []string {
	args := append([]string(nil), c.ExtraArgs...)
	for _, vb := range c.VBParams {
		if vb.MinActivationHeight == 0 {
			args = append(args, fmt.Sprintf("-vbparams=%s:%d:%d",
				vb.Deployment, vb.StartTime, vb.Timeout))
		} else {
			args = append(args, fmt.Sprintf("-vbparams=%s:%d:%d:%d",
				vb.Deployment, vb.StartTime, vb.Timeout, vb.MinActivationHeight))
		}
	}
	if c.AcceptNonstdTxn {
		args = append(args, "-acceptnonstdtxn=1")
	}
	return args
}

// GetDeploymentInfo returns the state of every soft-fork deployment at the
// chain tip.
func (r *Regtest) GetDeploymentInfo() (*DeploymentInfo, error) {
	return r.GetDeploymentInfoContext(context.Background())
}

// GetDeploymentInfoContext is like GetDeploymentInfo but takes a context.
func (r *Regtest) GetDeploymentInfoContext(ctx context.Context) (*DeploymentInfo, error) {
	info, err := call[DeploymentInfo](ctx, r, "getdeploymentinfo")
	if err != nil {
		return nil, fmt.Errorf("getdeploymentinfo: %w", err)
	}
	return &info, nil
}
