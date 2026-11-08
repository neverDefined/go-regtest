package regtest

import (
	"context"
	"fmt"
	"strings"
)

// Variant is the bitcoind implementation a node runs, as reported in
// getnetworkinfo's subversion. Tests use it to skip fork-specific cases, such
// as a BIP119 test on Bitcoin Core.
type Variant int

const (
	// VariantUnknown is the zero value. Variant returns it with an error, or
	// when the node reports an empty subversion.
	VariantUnknown Variant = iota
	// VariantCore identifies a stock Bitcoin Core node.
	VariantCore
	// VariantInquisition is Bitcoin Inquisition, Core's experimental fork that
	// activates BIP54, BIP118, BIP119, BIP347, BIP348 and BIP349.
	VariantInquisition
)

// String returns "unknown", "core" or "inquisition".
func (v Variant) String() string {
	switch v {
	case VariantCore:
		return "core"
	case VariantInquisition:
		return "inquisition"
	default:
		return "unknown"
	}
}

// Variant reports whether the node is Bitcoin Core or Bitcoin Inquisition.
// The first successful call asks getnetworkinfo; later calls return the
// cached result.
func (r *Regtest) Variant() (Variant, error) {
	return r.VariantContext(context.Background())
}

// VariantContext is like Variant but takes a context.
func (r *Regtest) VariantContext(ctx context.Context) (Variant, error) {
	r.variantMu.Lock()
	if r.variantCached {
		v := r.variant
		r.variantMu.Unlock()
		return v, nil
	}
	r.variantMu.Unlock()

	type networkInfo struct {
		SubVersion string `json:"subversion"`
	}
	info, err := call[networkInfo](ctx, r, "getnetworkinfo")
	if err != nil {
		return VariantUnknown, fmt.Errorf("Variant: getnetworkinfo: %w", err)
	}

	v := parseVariant(info.SubVersion)

	r.variantMu.Lock()
	r.variant = v
	r.variantCached = true
	r.variantMu.Unlock()
	return v, nil
}

// parseVariant maps a getnetworkinfo subversion to a Variant.
//
// Bitcoin Inquisition reports a subversion like /Satoshi:29.2.0(inquisition)/;
// Bitcoin Core reports /Satoshi:29.0.0/. The match on "inquisition" ignores
// case so a future change in capitalization or version format still works.
// An empty subversion maps to VariantUnknown.
func parseVariant(subversion string) Variant {
	if subversion == "" {
		return VariantUnknown
	}
	if strings.Contains(strings.ToLower(subversion), "inquisition") {
		return VariantInquisition
	}
	return VariantCore
}
