package regtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// BIPID identifies a Bitcoin Improvement Proposal that this library tracks
// in its curated registry. BIP IDs are typed constants — prefer them over
// stringly-typed deployment names (e.g. "checktemplateverify") so callers
// avoid typos and pick up renames automatically when bitcoind changes a
// deployment key.
//
// New BIPs can be added without breaking the iota ordering: append to the
// const block and the registry. Reordering existing entries is a breaking
// change for callers that persist BIPIDs.
type BIPID int

const (
	// BIPUnknown is the zero value, returned when an EnrichedDeployment's
	// deployment key isn't tracked by this library's registry.
	BIPUnknown BIPID = iota
	// BIPTestdummy is Bitcoin Core's regtest-only "testdummy" deployment.
	// Used by activation tests in this library; not BIP-numbered.
	BIPTestdummy
	// BIPTaproot covers BIP340 (Schnorr signatures), BIP341 (Taproot), and
	// BIP342 (Tapscript). Buried as of Bitcoin Core 22.
	BIPTaproot
	// BIP54 is Consensus Cleanup. Active on Bitcoin Inquisition 29.2+.
	BIP54
	// BIP118 is SIGHASH_ANYPREVOUT (APO/eltoo). Active on Bitcoin
	// Inquisition.
	BIP118
	// BIP119 is OP_CHECKTEMPLATEVERIFY (CTV). Active on Bitcoin Inquisition.
	BIP119
	// BIP347 is OP_CAT. Active on Bitcoin Inquisition.
	BIP347
	// BIP348 is OP_CHECKSIGFROMSTACK (CSFS). Active on Bitcoin Inquisition.
	BIP348
	// BIP349 is OP_INTERNALKEY. Active on Bitcoin Inquisition.
	BIP349
)

// String returns a compact, human-readable name for the BIPID — "BIP119" for
// BIP-numbered entries, the registry name for others ("testdummy",
// "Taproot"), and "BIPUnknown" for unregistered values.
func (b BIPID) String() string {
	m, ok := metaByBIP(b)
	if !ok {
		return "BIPUnknown"
	}
	if m.bipNumber > 0 {
		return fmt.Sprintf("BIP%d", m.bipNumber)
	}
	return m.name
}

// ErrUnknownBIP is returned by registry-aware methods (SupportsBIP,
// MineUntilActiveBIP) when the supplied BIPID isn't in the curated registry.
// Use errors.Is to detect it; do not string-match.
var ErrUnknownBIP = errors.New("unknown BIP")

// bipMeta is the curated metadata for a BIP tracked by this library.
type bipMeta struct {
	id              BIPID
	deployment      string  // matches getdeploymentinfo key
	bipNumber       int     // 0 for non-numbered deployments (testdummy)
	name            string  // human-readable feature name
	docURL          string  // link to the BIP text or upstream tracking issue
	expectedVariant Variant // VariantCore for testdummy/taproot, VariantInquisition for the rest
}

// bipRegistry is the package-private source of truth mapping each BIPID to
// its bitcoind deployment key, BIP number, name, and doc URL. Deployment key
// strings were verified against Bitcoin Inquisition 29.2's getdeploymentinfo
// output (2026-04); Core's testdummy/taproot keys are stable across 24+.
var bipRegistry = []bipMeta{
	{
		id:              BIPTestdummy,
		deployment:      "testdummy",
		bipNumber:       0,
		name:            "testdummy",
		docURL:          "https://github.com/bitcoin/bitcoin/blob/master/src/kernel/chainparams.cpp",
		expectedVariant: VariantCore,
	},
	{
		id:              BIPTaproot,
		deployment:      "taproot",
		bipNumber:       341,
		name:            "Taproot",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0341.mediawiki",
		expectedVariant: VariantCore,
	},
	{
		id:              BIP54,
		deployment:      "consensuscleanup",
		bipNumber:       54,
		name:            "Consensus Cleanup",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0054.mediawiki",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP118,
		deployment:      "anyprevout",
		bipNumber:       118,
		name:            "ANYPREVOUT",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0118.mediawiki",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP119,
		deployment:      "checktemplateverify",
		bipNumber:       119,
		name:            "OP_CHECKTEMPLATEVERIFY",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0119.mediawiki",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP347,
		deployment:      "op_cat",
		bipNumber:       347,
		name:            "OP_CAT",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0347.mediawiki",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP348,
		deployment:      "checksigfromstack",
		bipNumber:       348,
		name:            "OP_CHECKSIGFROMSTACK",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0348.mediawiki",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP349,
		deployment:      "internalkey",
		bipNumber:       349,
		name:            "OP_INTERNALKEY",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0349.mediawiki",
		expectedVariant: VariantInquisition,
	},
}

// metaByBIP returns the registry entry for the given BIPID, or false when
// the ID isn't registered. Linear scan is fine — the registry is short and
// lookups happen only on test setup paths.
func metaByBIP(b BIPID) (bipMeta, bool) {
	for _, m := range bipRegistry {
		if m.id == b {
			return m, true
		}
	}
	return bipMeta{}, false
}

// metaByDeployment returns the registry entry whose deployment key matches d,
// or false when the key isn't tracked.
func metaByDeployment(d string) (bipMeta, bool) {
	for _, m := range bipRegistry {
		if m.deployment == d {
			return m, true
		}
	}
	return bipMeta{}, false
}

// EnrichedDeployment is a single soft-fork deployment's live state joined with
// curated registry metadata. Returned by ListDeployments. Deployments that
// aren't in the registry are still returned (with BIP=BIPUnknown and zero
// metadata) so callers can see future forks bitcoind reports before this
// library is updated.
type EnrichedDeployment struct {
	// BIP is the typed BIPID from the registry, or BIPUnknown when the live
	// deployment key isn't tracked by this library.
	BIP BIPID
	// BIPNumber is the canonical BIP number (e.g. 119 for BIP119), or 0 for
	// deployments without one (testdummy, unknown).
	BIPNumber int
	// Name is the human-readable feature name from the registry, or empty
	// for unknown deployments.
	Name string
	// DocURL points at the BIP text (or upstream tracking issue), or empty
	// for unknown deployments.
	DocURL string
	// Deployment is the raw key bitcoind reports under getdeploymentinfo.
	Deployment string
	// Type is the deployment kind reported by bitcoind: "buried", "bip9",
	// or "heretical" (Inquisition's signaling-without-version-bits scheme).
	Type string
	// Active reports whether the deployment is enforced as of the chain tip.
	Active bool
	// Status is the typed BIP9 status. SoftForkActive for active buried/
	// heretical deployments; the BIP9 state-machine value for type=="bip9";
	// SoftForkUnknown when bitcoind doesn't carry a recognizable status.
	Status SoftForkStatus
	// Height is the activation height for buried deployments and the
	// activation block for active BIP9/heretical entries; 0 otherwise.
	Height int64
}

// ListDeployments returns every soft-fork deployment the running node knows
// about, joined with curated registry metadata where available.
//
// This is a convenience wrapper around ListDeploymentsContext that uses
// context.Background().
//
// Returns:
//   - []EnrichedDeployment: sorted alphabetically by Deployment for stable
//     output across runs.
//   - error: errNotConnected before Start; otherwise the wrapped
//     getdeploymentinfo failure.
//
// Example:
//
//	deps, err := rt.ListDeployments()
//	if err != nil { return err }
//	for _, d := range deps {
//	    fmt.Printf("%-22s %s (active=%v) %s\n",
//	        d.Deployment, d.Status, d.Active, d.DocURL)
//	}
func (r *Regtest) ListDeployments() ([]EnrichedDeployment, error) {
	return r.ListDeploymentsContext(context.Background())
}

// ListDeploymentsContext is the context-aware variant of ListDeployments.
//
// Parameters:
//   - ctx: context for cancellation and timeout control.
//
// Returns:
//   - []EnrichedDeployment: sorted alphabetically by Deployment.
//   - error: errNotConnected before Start; otherwise the wrapped
//     getdeploymentinfo failure.
func (r *Regtest) ListDeploymentsContext(ctx context.Context) ([]EnrichedDeployment, error) {
	info, err := r.GetDeploymentInfoContext(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]EnrichedDeployment, 0, len(info.Deployments))
	for name, d := range info.Deployments {
		ed := EnrichedDeployment{
			Deployment: name,
			Type:       d.Type,
			Active:     d.Active,
			Height:     d.Height,
		}
		switch {
		case d.BIP9 != nil:
			ed.Status = parseSoftForkStatus(d.BIP9.Status)
		case d.Active:
			ed.Status = SoftForkActive
		default:
			ed.Status = SoftForkUnknown
		}
		if m, ok := metaByDeployment(name); ok {
			ed.BIP = m.id
			ed.BIPNumber = m.bipNumber
			ed.Name = m.name
			ed.DocURL = m.docURL
		}
		out = append(out, ed)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Deployment < out[j].Deployment
	})
	return out, nil
}

// SupportsBIP reports whether the running bitcoind exposes the deployment
// keyed by this BIPID. The check is live — it queries getdeploymentinfo and
// looks for the registry's deployment key in the response — so a Core node
// correctly returns false for Inquisition-only BIPs (BIP119, BIP118, etc.)
// even though those BIPs are in the registry.
//
// This is the canonical "skip when missing" primitive:
//
//	if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
//	    t.Skip("requires bitcoind-inquisition")
//	}
//
// Returns:
//   - bool: true when the live response includes the BIPID's deployment key.
//   - error: ErrUnknownBIP when the BIPID isn't in the registry;
//     errNotConnected before Start; otherwise the wrapped
//     getdeploymentinfo failure.
func (r *Regtest) SupportsBIP(bip BIPID) (bool, error) {
	return r.SupportsBIPContext(context.Background(), bip)
}

// SupportsBIPContext is the context-aware variant of SupportsBIP.
func (r *Regtest) SupportsBIPContext(ctx context.Context, bip BIPID) (bool, error) {
	m, ok := metaByBIP(bip)
	if !ok {
		return false, fmt.Errorf("%w: %d", ErrUnknownBIP, bip)
	}
	info, err := r.GetDeploymentInfoContext(ctx)
	if err != nil {
		return false, err
	}
	_, present := info.Deployments[m.deployment]
	return present, nil
}

// MineUntilActiveBIP is the typed alternative to MineUntilActive — translate a
// BIPID to its registry deployment key, then drive the BIP9 state machine
// (or buried/heretical activation) to SoftForkActive.
//
// Parameters:
//   - bip: typed BIP identifier from the registry. ErrUnknownBIP otherwise.
//   - miner: address that receives coinbase rewards while mining activation
//     windows.
//   - maxBlocks: hard cap on blocks mined; >0.
//
// Returns:
//   - int64: blocks actually mined.
//   - error: ErrUnknownBIP for unregistered BIPs; same error semantics as
//     MineUntilActiveContext otherwise (validation, RPC, SoftForkFailed).
//
// Example:
//
//	mined, err := rt.MineUntilActiveBIP(regtest.BIP119, addr, 2000)
//	if errors.Is(err, regtest.ErrUnknownBIP) {
//	    t.Skipf("BIP119 not in registry: %v", err)
//	}
func (r *Regtest) MineUntilActiveBIP(bip BIPID, miner string, maxBlocks int64) (int64, error) {
	return r.MineUntilActiveBIPContext(context.Background(), bip, miner, maxBlocks)
}

// MineUntilActiveBIPContext is the context-aware variant of MineUntilActiveBIP.
func (r *Regtest) MineUntilActiveBIPContext(ctx context.Context, bip BIPID, miner string, maxBlocks int64) (int64, error) {
	m, ok := metaByBIP(bip)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrUnknownBIP, bip)
	}
	return r.MineUntilActiveContext(ctx, m.deployment, miner, maxBlocks)
}
