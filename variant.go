package regtest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Variant identifies which bitcoind implementation the running node belongs to.
//
// Inspected via getnetworkinfo's subversion field. Used by tests to gate
// fork-specific behavior — e.g. skip a BIP119 activation test when running
// against Core, or assert variant-specific deployments are present.
type Variant int

const (
	// VariantUnknown is the zero value, returned when subversion parsing
	// fails or the node has not been started. Should not appear in practice
	// on a healthy node.
	VariantUnknown Variant = iota
	// VariantCore identifies a stock Bitcoin Core node.
	VariantCore
	// VariantInquisition identifies a Bitcoin Inquisition node — Core's
	// experimental fork that activates BIP54/118/119/347/348/349.
	VariantInquisition
)

// String returns a stable, human-readable name for the Variant
// ("unknown", "core", "inquisition"). Useful for logging in tests.
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

// Variant returns which bitcoind implementation the running node belongs to.
// This is a convenience wrapper around VariantContext that uses
// context.Background().
//
// Returns:
//   - Variant: VariantCore or VariantInquisition on success.
//   - error: errNotConnected if Start has not been called; otherwise the
//     wrapped getnetworkinfo failure.
//
// Example:
//
//	v, err := rt.Variant()
//	if err != nil {
//	    return err
//	}
//	if v != regtest.VariantInquisition {
//	    t.Skip("requires bitcoind-inquisition")
//	}
func (r *Regtest) Variant() (Variant, error) {
	return r.VariantContext(context.Background())
}

// VariantContext is the context-aware variant of Variant. The first call
// hits getnetworkinfo and parses the subversion field; the result is cached
// and subsequent calls return it without further RPC traffic.
//
// Parameters:
//   - ctx: context for cancellation and timeout control. Pre-cancelled
//     context returns ctx.Err() before any work.
//
// Returns:
//   - Variant: VariantCore or VariantInquisition on success.
//   - error: errNotConnected if Start has not been called; otherwise the
//     wrapped getnetworkinfo failure.
func (r *Regtest) VariantContext(ctx context.Context) (Variant, error) {
	r.variantMu.Lock()
	if r.variantCached {
		v := r.variant
		r.variantMu.Unlock()
		return v, nil
	}
	r.variantMu.Unlock()

	raw, err := r.rawRPC(ctx, "getnetworkinfo")
	if err != nil {
		return VariantUnknown, fmt.Errorf("Variant: getnetworkinfo: %w", err)
	}

	var info struct {
		SubVersion string `json:"subversion"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return VariantUnknown, fmt.Errorf("Variant: parse getnetworkinfo: %w", err)
	}

	v := parseVariant(info.SubVersion)

	r.variantMu.Lock()
	r.variant = v
	r.variantCached = true
	r.variantMu.Unlock()
	return v, nil
}

// parseVariant maps a getnetworkinfo subversion string to a Variant.
//
// Bitcoin Inquisition reports a subversion like /Satoshi:29.2.0(inquisition)/
// (lowercase, parenthesized). Stock Bitcoin Core reports /Satoshi:29.0.0/.
// The check is case-insensitive on the substring "inquisition" so that any
// future capitalization or version-format change still resolves correctly.
//
// An empty subversion (cannot happen in practice on a healthy node) maps to
// VariantUnknown so callers can detect parse failures.
func parseVariant(subversion string) Variant {
	if subversion == "" {
		return VariantUnknown
	}
	if strings.Contains(strings.ToLower(subversion), "inquisition") {
		return VariantInquisition
	}
	return VariantCore
}
