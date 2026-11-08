package regtest

import (
	"errors"
	"testing"
)

func TestVariant_Returns(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	v, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant: %v", err)
	}
	if v == VariantUnknown {
		t.Errorf("Variant should resolve to Core or Inquisition, got Unknown")
	}
	t.Logf("running against variant: %s", v)
}

// TestRPC_Variant_Cached confirms repeat calls return the same value without
// surfacing transient errors. Functional check — does not directly count RPC
// hits, but a cache miss on the second call would re-issue getnetworkinfo
// against an already-stopped node and fail.

func TestVariant_Cached(t *testing.T) {
	t.Parallel()
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	v1, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant (first): %v", err)
	}

	// Stop the node; a cache miss on the next call would fail.
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	v2, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant (cached): %v", err)
	}
	if v1 != v2 {
		t.Errorf("cached Variant changed: first=%s second=%s", v1, v2)
	}
}

// TestRPC_Variant_PreStart confirms Variant() returns ErrNotConnected before
// Start has been called.

func TestVariantValidation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	if _, err := rt.Variant(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("pre-Start Variant: want ErrNotConnected, got %v", err)
	}
}

// TestRPC_Variant_StringRoundTrip pins the human-readable enum strings.

func TestVariant_StringRoundTrip(t *testing.T) {
	cases := []struct {
		v    Variant
		want string
	}{
		{VariantUnknown, "unknown"},
		{VariantCore, "core"},
		{VariantInquisition, "inquisition"},
	}
	for _, tc := range cases {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("Variant(%d).String() = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// Test_ParseVariant pins the subversion → Variant mapping against the actual
// strings that Bitcoin Core and Bitcoin Inquisition 29.2 report. Lets the
// Inquisition path be exercised without a live Inquisition binary on PATH.

func TestParseVariant(t *testing.T) {
	cases := []struct {
		name       string
		subversion string
		want       Variant
	}{
		{"empty", "", VariantUnknown},
		{"core-29", "/Satoshi:29.0.0/", VariantCore},
		{"core-25", "/Satoshi:25.0.0/", VariantCore},
		{"inquisition-29.2-lowercase", "/Satoshi:29.2.0(inquisition)/", VariantInquisition},
		{"inquisition-titlecase", "/Satoshi:29.2.0(Inquisition)/", VariantInquisition},
		{"inquisition-uppercase", "/Satoshi:29.2.0(INQUISITION)/", VariantInquisition},
		{"plain-core-no-marker", "/Satoshi:29.0.0(custom)/", VariantCore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseVariant(tc.subversion); got != tc.want {
				t.Errorf("parseVariant(%q) = %s, want %s", tc.subversion, got, tc.want)
			}
		})
	}
}

// Test_BIPRegistry_Shape pins the integrity of bipRegistry: every BIPID
// constant has exactly one entry; deployment keys, BIPIDs, and BIP numbers
// are unique; no zero/empty leaks. Pure unit test — no node spawned.

func TestBIPRegistry_Shape(t *testing.T) {
	wantIDs := []BIPID{
		BIPTestdummy, BIPTaproot,
		BIP54, BIP118, BIP119, BIP347, BIP348, BIP349,
	}

	seenID := make(map[BIPID]bool)
	seenDeployment := make(map[string]bool)
	seenBIPNumber := make(map[int]bool)

	for _, m := range bipRegistry {
		if m.id == BIPUnknown {
			t.Errorf("bipRegistry must not contain BIPUnknown")
		}
		if seenID[m.id] {
			t.Errorf("duplicate BIPID in registry: %d", m.id)
		}
		seenID[m.id] = true

		if m.deployment == "" {
			t.Errorf("registry entry %d has empty deployment key", m.id)
		}
		if seenDeployment[m.deployment] {
			t.Errorf("duplicate deployment key in registry: %q", m.deployment)
		}
		seenDeployment[m.deployment] = true

		if m.bipNumber > 0 {
			if seenBIPNumber[m.bipNumber] {
				t.Errorf("duplicate BIP number in registry: %d", m.bipNumber)
			}
			seenBIPNumber[m.bipNumber] = true
		}
		if m.name == "" {
			t.Errorf("registry entry %d has empty name", m.id)
		}
		if m.docURL == "" {
			t.Errorf("registry entry %d has empty docURL", m.id)
		}
	}

	for _, want := range wantIDs {
		if !seenID[want] {
			t.Errorf("BIPID %d (%s) missing from registry", want, want)
		}
	}
}

// Test_BIPID_String pins the human-readable name format for logging.

func TestBIPID_String(t *testing.T) {
	cases := []struct {
		bip  BIPID
		want string
	}{
		{BIPUnknown, "BIPUnknown"},
		{BIPTestdummy, "testdummy"},
		{BIPTaproot, "BIP341"},
		{BIP54, "BIP54"},
		{BIP119, "BIP119"},
		{BIP347, "BIP347"},
		{BIPID(9999), "BIPUnknown"},
	}
	for _, tc := range cases {
		if got := tc.bip.String(); got != tc.want {
			t.Errorf("BIPID(%d).String() = %q, want %q", tc.bip, got, tc.want)
		}
	}
}

// Test_MetaLookups confirms metaByBIP and metaByDeployment round-trip for
// every registry entry, and return false for unknown lookups.

func TestMetaLookups(t *testing.T) {
	for _, m := range bipRegistry {
		got, ok := metaByBIP(m.id)
		if !ok || got.deployment != m.deployment {
			t.Errorf("metaByBIP(%d) = (%v, %v), want (%+v, true)", m.id, got, ok, m)
		}
		got, ok = metaByDeployment(m.deployment)
		if !ok || got.id != m.id {
			t.Errorf("metaByDeployment(%q) = (%v, %v), want (%+v, true)", m.deployment, got, ok, m)
		}
	}
	if _, ok := metaByBIP(BIPUnknown); ok {
		t.Error("metaByBIP(BIPUnknown) should return false")
	}
	if _, ok := metaByDeployment("nonexistent-deployment"); ok {
		t.Error("metaByDeployment(nonexistent) should return false")
	}
}

// TestRPC_ListDeployments confirms the registry-joined view returns at least
// taproot (buried, both variants) plus the variant-specific deployments, and
// that registry metadata is populated for known keys.
