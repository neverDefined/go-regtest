package regtest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func Test_StartT(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)
	if err := rt.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if got := rt.Config().Host; got == DefaultConfig().Host {
		t.Errorf("StartT should pick its own ports, got the default %s", got)
	}
}

// Test_StartT_ParallelNodes starts several nodes at once and checks they all
// come up on distinct ports.
func Test_StartT_ParallelNodes(t *testing.T) {
	t.Parallel()
	const n = 4
	hosts := make(chan string, n)
	t.Run("group", func(t *testing.T) {
		for i := 0; i < n; i++ {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				rt := StartT(t, nil)
				if err := rt.HealthCheck(); err != nil {
					t.Fatalf("HealthCheck: %v", err)
				}
				hosts <- rt.Config().Host
			})
		}
	})
	close(hosts)
	seen := map[string]bool{}
	for h := range hosts {
		if seen[h] {
			t.Errorf("two nodes got the same host %s", h)
		}
		seen[h] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d running nodes, got %d", n, len(seen))
	}
}

// Test_StartT_StopsAtCleanup checks the node is gone once its test finishes.
func Test_StartT_StopsAtCleanup(t *testing.T) {
	t.Parallel()
	var host string
	t.Run("inner", func(t *testing.T) {
		host = StartT(t, nil).Config().Host
	})
	probe, err := New(&Config{Host: host})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	running, err := probe.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Errorf("node at %s still running after its test finished", host)
	}
}

// Test_StartT_UsesConfig checks that fields other than Host and DataDir are
// passed through.
func Test_StartT_UsesConfig(t *testing.T) {
	t.Parallel()
	rt := StartT(t, &Config{ExtraArgs: []string{"-debug=mempool"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := rt.rawRPC(ctx, "logging")
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	var categories map[string]bool
	if err := json.Unmarshal(raw, &categories); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !categories["mempool"] {
		t.Errorf("ExtraArgs not applied: mempool logging is off")
	}
}

// fakeTB records Skipf/Fatalf instead of ending the test.
type fakeTB struct {
	testing.TB
	skipped, fatal bool
}

func (f *fakeTB) Helper()               {}
func (f *fakeTB) Skipf(string, ...any)  { f.skipped = true }
func (f *fakeTB) Fatalf(string, ...any) { f.fatal = true }

// Test_StartT_MissingBinary checks StartT skips without bitcoind, but fails
// when the CI environment variable is set.
func Test_StartT_MissingBinary(t *testing.T) {
	cfg := &Config{BinaryPath: "/nonexistent/go-regtest/bitcoind"}

	t.Setenv("CI", "")
	local := &fakeTB{TB: t}
	if rt := StartT(local, cfg); rt != nil || !local.skipped || local.fatal {
		t.Errorf("without CI: want skip, got rt=%v skipped=%v fatal=%v", rt, local.skipped, local.fatal)
	}

	t.Setenv("CI", "true")
	ci := &fakeTB{TB: t}
	if rt := StartT(ci, cfg); rt != nil || ci.skipped || !ci.fatal {
		t.Errorf("with CI: want fatal, got rt=%v skipped=%v fatal=%v", rt, ci.skipped, ci.fatal)
	}
}

func Test_ReservePorts(t *testing.T) {
	a, err := reservePorts()
	if err != nil {
		t.Fatal(err)
	}
	b, err := reservePorts()
	if err != nil {
		t.Fatal(err)
	}
	if d := a - b; d > -3 && d < 3 {
		t.Errorf("ranges overlap: %d and %d", a, b)
	}
}
