package regtest

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

func testdummyVBParams() []VBParam {
	return []VBParam{{
		Deployment:          "testdummy",
		StartTime:           0,
		Timeout:             9999999999,
		MinActivationHeight: 0,
	}}
}

func testdummyStartConfig() *Config {
	return &Config{
		AcceptNonstdTxn: true,
		VBParams:        testdummyVBParams(),
	}
}

func testdummyConfig(t *testing.T, port int) *Config {
	t.Helper()
	return &Config{
		Host:            fmt.Sprintf("127.0.0.1:%d", port),
		User:            "user",
		Pass:            "pass",
		DataDir:         filepath.Join(t.TempDir(), "regtest"),
		AcceptNonstdTxn: true,
		VBParams:        testdummyVBParams(),
	}
}

func TestListDeployments(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	deps, err := rt.ListDeployments()
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("expected at least one deployment, got 0")
	}

	// Sorted by Deployment for stable output.
	for i := 1; i < len(deps); i++ {
		if deps[i-1].Deployment > deps[i].Deployment {
			t.Errorf("deps not sorted: %q > %q", deps[i-1].Deployment, deps[i].Deployment)
		}
	}

	byKey := make(map[string]EnrichedDeployment, len(deps))
	for _, d := range deps {
		byKey[d.Deployment] = d
	}

	// testdummy is listed by every Core and Inquisition version.
	td, ok := byKey["testdummy"]
	if !ok {
		t.Fatal("expected 'testdummy' in ListDeployments output")
	}
	if td.BIP != BIPTestdummy || td.Name != "testdummy" || td.DocURL == "" {
		t.Errorf("testdummy not joined with registry metadata: %+v", td)
	}

	// Core 32 dropped taproot from getdeploymentinfo; check it where listed.
	if tap, ok := byKey["taproot"]; ok {
		if tap.BIP != BIPTaproot || tap.BIPNumber != 341 || tap.DocURL == "" {
			t.Errorf("taproot not joined with registry metadata: %+v", tap)
		}
		if !tap.Active {
			t.Error("taproot should be Active on regtest")
		}
	}
}

// TestRPC_SupportsBIP_Testdummy confirms SupportsBIP returns true for a
// deployment that's always in regtest's getdeploymentinfo (testdummy on
// Core, testdummy as heretical on Inquisition).

func TestSupportsBIP_Testdummy(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	ok, err := rt.SupportsBIP(BIPTestdummy)
	if err != nil {
		t.Fatalf("SupportsBIP: %v", err)
	}
	if !ok {
		t.Error("SupportsBIP(BIPTestdummy) should be true on a regtest node")
	}
}

// TestRPC_SupportsBIP_VariantSpecific confirms variant-specific BIPs resolve
// correctly: BIP119 should be present on Inquisition and absent on Core.
// The test asserts whichever direction the running variant dictates.

func TestSupportsBIP_VariantSpecific(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	v, err := rt.Variant()
	if err != nil {
		t.Fatalf("Variant: %v", err)
	}

	gotCTV, err := rt.SupportsBIP(BIP119)
	if err != nil {
		t.Fatalf("SupportsBIP(BIP119): %v", err)
	}
	switch v {
	case VariantInquisition:
		if !gotCTV {
			t.Error("BIP119 should be present on Inquisition")
		}
	case VariantCore:
		if gotCTV {
			t.Error("BIP119 should be absent on stock Core")
		}
	default:
		t.Fatalf("unexpected variant: %s", v)
	}
}

// TestRPC_SupportsBIP_UnknownBIP pins the validation contract: passing a
// BIPID outside the registry returns ErrUnknownBIP without touching the
// node.

func TestDeploymentsValidation(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	_, err = rt.SupportsBIP(BIPUnknown)
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("SupportsBIP(BIPUnknown): want ErrUnknownBIP, got %v", err)
	}
	_, err = rt.SupportsBIP(BIPID(9999))
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("SupportsBIP(BIPID(9999)): want ErrUnknownBIP, got %v", err)
	}
	_, err = rt.MineUntilActiveBIP(BIPID(9999), "addr", 100)
	if !errors.Is(err, ErrUnknownBIP) {
		t.Errorf("MineUntilActiveBIP(9999): want ErrUnknownBIP, got %v", err)
	}
}

// TestRPC_SetMockTime_RoundTrip confirms setmocktime takes effect: mining a
// fresh block under a mocktime far in the future advances getblockchaininfo's
// mediantime once enough blocks accumulate at that timestamp. Asserts the
// tip's block.time matches the mocked value within 1s.

func TestTestdummyConfig(t *testing.T) {
	cfg := testdummyConfig(t, 19702)
	if cfg.Host != "127.0.0.1:19702" {
		t.Errorf("Host = %q, want 127.0.0.1:19702", cfg.Host)
	}
	if !cfg.AcceptNonstdTxn {
		t.Error("expected AcceptNonstdTxn=true")
	}
	if len(cfg.VBParams) != 1 {
		t.Fatalf("expected 1 VBParam, got %d", len(cfg.VBParams))
	}
	vb := cfg.VBParams[0]
	if vb.Deployment != "testdummy" {
		t.Errorf("Deployment = %q, want testdummy", vb.Deployment)
	}
	if vb.StartTime != 0 || vb.Timeout != 9999999999 || vb.MinActivationHeight != 0 {
		t.Errorf("VBParam = %+v, want {testdummy 0 9999999999 0}", vb)
	}
}

// Test_VBParams_Render unit-tests Config.renderExtraArgs (no node spawned).
// Pins the wire format for -vbparams and the composition order:
// ExtraArgs first, then VBParams in declaration order, then -acceptnonstdtxn.

func TestVBParams_Render(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "empty",
			cfg:  Config{},
			want: nil,
		},
		{
			name: "extra-args-only",
			cfg:  Config{ExtraArgs: []string{"-debug=net"}},
			want: []string{"-debug=net"},
		},
		{
			name: "vbparams-explicit-zero-min-height",
			cfg: Config{
				VBParams: []VBParam{
					{Deployment: "testdummy", StartTime: 0, Timeout: 9999999999, MinActivationHeight: 0},
				},
			},
			// 3-field form when MinActivationHeight is 0 — required for
			// Inquisition compatibility, accepted by Core 24+.
			want: []string{"-vbparams=testdummy:0:9999999999"},
		},
		{
			name: "vbparams-explicit-nonzero-min-height",
			cfg: Config{
				VBParams: []VBParam{
					{Deployment: "testdummy", StartTime: 0, Timeout: 9999999999, MinActivationHeight: 432},
				},
			},
			// 4-field form is opt-in (Core 24+ only); user passes a non-zero
			// MinActivationHeight to request it.
			want: []string{"-vbparams=testdummy:0:9999999999:432"},
		},
		{
			name: "vbparams-helpers",
			cfg: Config{
				VBParams: []VBParam{
					VBAlwaysActive("anyprevout"),
					VBNeverActive("checktemplateverify"),
				},
			},
			want: []string{
				"-vbparams=anyprevout:-1:0",
				"-vbparams=checktemplateverify:-2:0",
			},
		},
		{
			name: "all-three-combine-in-order",
			cfg: Config{
				ExtraArgs: []string{"-debug=net", "-printtoconsole=0"},
				VBParams: []VBParam{
					{Deployment: "testdummy", StartTime: 0, Timeout: 9999999999, MinActivationHeight: 0},
				},
				AcceptNonstdTxn: true,
			},
			want: []string{
				"-debug=net",
				"-printtoconsole=0",
				"-vbparams=testdummy:0:9999999999",
				"-acceptnonstdtxn=1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.renderExtraArgs()
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Test_New_EmptyVBParamDeployment pins the validation contract that an empty
// Deployment field is rejected at New time rather than silently producing a
// malformed -vbparams= flag.

func TestNew_EmptyVBParamDeployment(t *testing.T) {
	_, err := New(&Config{
		VBParams: []VBParam{{Deployment: "", StartTime: 0, Timeout: 0, MinActivationHeight: 0}},
	})
	if err == nil {
		t.Fatal("expected error from empty Deployment, got nil")
	}
}

// Test_AcceptNonstdTxn verifies that Config.AcceptNonstdTxn maps to
// -acceptnonstdtxn=1 and actually changes mempool policy. Combined with
// -datacarrier=0 (which marks any OP_RETURN output as non-standard
// regardless of payload size — robust across Core versions that have
// relaxed OP_RETURN size limits), a tx with an OP_RETURN output should be
// rejected by default but accepted when AcceptNonstdTxn is on.
