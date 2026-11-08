package regtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// BIPID names a soft fork in this package's registry. Prefer the constants
// to bitcoind deployment names such as "checktemplateverify": a typo fails to
// compile, and a renamed deployment key only needs a registry update here.
//
// New BIPIDs are appended so existing values never change; reordering them
// would break callers that store BIPIDs.
type BIPID int

const (
	// BIPUnknown is the zero value, used in EnrichedDeployment for a
	// deployment the registry doesn't track.
	BIPUnknown BIPID = iota
	// BIPTestdummy is Bitcoin Core's regtest-only "testdummy" deployment,
	// which has no BIP number.
	BIPTestdummy
	// BIPTaproot covers BIP340 (Schnorr signatures), BIP341 (Taproot) and
	// BIP342 (Tapscript). Always active on regtest.
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

// String returns "BIP<n>" for BIP-numbered entries ("BIP119", "BIP341"),
// the registry name for testdummy, and "BIPUnknown" for unregistered values.
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

// ErrUnknownBIP is returned by SupportsBIP and MineUntilActiveBIP when the
// BIPID isn't in the registry. Test for it with errors.Is.
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

// bipRegistry maps each BIPID to its getdeploymentinfo key, BIP number, name
// and doc URL. The Inquisition keys were checked against Bitcoin Inquisition
// 29.2's getdeploymentinfo output (2026-04); Core's testdummy and taproot keys
// are stable across 24+.
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
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0054.md",
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
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0348.md",
		expectedVariant: VariantInquisition,
	},
	{
		id:              BIP349,
		deployment:      "internalkey",
		bipNumber:       349,
		name:            "OP_INTERNALKEY",
		docURL:          "https://github.com/bitcoin/bips/blob/master/bip-0349.md",
		expectedVariant: VariantInquisition,
	},
}

// metaByBIP returns the registry entry for b, or false if b isn't registered.
// A linear scan is fine: the registry is short and lookups happen only during
// test setup.
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

// EnrichedDeployment is one deployment's live state from getdeploymentinfo,
// joined with registry metadata. Deployments the registry doesn't track are
// still listed, with BIP set to BIPUnknown and empty metadata, so callers see
// new forks before this package knows about them.
type EnrichedDeployment struct {
	// BIP is the registry BIPID, or BIPUnknown for an untracked deployment.
	BIP BIPID
	// BIPNumber is the BIP number (119 for BIP119), or 0 for deployments
	// without one (testdummy, unknown).
	BIPNumber int
	// Name is the registry's feature name; empty for unknown deployments.
	Name string
	// DocURL links to the BIP text or upstream tracking issue; empty for
	// unknown deployments.
	DocURL string
	// Deployment is the key bitcoind reports in getdeploymentinfo.
	Deployment string
	// Type is the deployment kind bitcoind reports: "buried", "bip9", or
	// "heretical" (Inquisition's signaling-without-version-bits scheme).
	Type string
	// Active reports whether the deployment is enforced at the chain tip.
	Active bool
	// Status is the BIP9 status when bitcoind reports one. Otherwise it is
	// SoftForkActive for an active deployment and SoftForkUnknown for an
	// inactive one.
	Status SoftForkStatus
	// Height is the activation height for buried deployments and the
	// activation block for active BIP9 or heretical ones; 0 otherwise.
	Height int64
}

// ListDeployments returns every soft-fork deployment the node reports, with
// registry metadata where available, sorted by Deployment.
func (r *Regtest) ListDeployments() ([]EnrichedDeployment, error) {
	return r.ListDeploymentsContext(context.Background())
}

// ListDeploymentsContext is like ListDeployments but takes a context.
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

// SupportsBIP reports whether the running node has the deployment for bip.
// It checks getdeploymentinfo live, so a Core node returns false for
// Inquisition-only BIPs such as BIP119. It returns ErrUnknownBIP if bip isn't
// in the registry.
//
// Use it to skip tests the binary can't run:
//
//	if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
//	    t.Skip("requires bitcoind-inquisition")
//	}
func (r *Regtest) SupportsBIP(bip BIPID) (bool, error) {
	return r.SupportsBIPContext(context.Background(), bip)
}

// SupportsBIPContext is like SupportsBIP but takes a context.
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
	return present || enforcedWithoutEntry[m.deployment], nil
}

// MineUntilActiveBIP is MineUntilActive for a registry BIPID. It returns
// ErrUnknownBIP if bip isn't in the registry.
func (r *Regtest) MineUntilActiveBIP(bip BIPID, miner string, maxBlocks int64) (int64, error) {
	return r.MineUntilActiveBIPContext(context.Background(), bip, miner, maxBlocks)
}

// MineUntilActiveBIPContext is like MineUntilActiveBIP but takes a context.
func (r *Regtest) MineUntilActiveBIPContext(ctx context.Context, bip BIPID, miner string, maxBlocks int64) (int64, error) {
	m, ok := metaByBIP(bip)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrUnknownBIP, bip)
	}
	return r.MineUntilActiveContext(ctx, m.deployment, miner, maxBlocks)
}
