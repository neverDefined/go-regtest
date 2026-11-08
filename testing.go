package regtest

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// StartT starts a regtest node for the duration of a test and returns it.
// The node is stopped and its data directory removed when the test and its
// subtests finish. Nodes started by StartT are independent, so tests that
// use it can call t.Parallel.
//
// cfg may be nil. If cfg.Host is empty, StartT picks two free consecutive
// local ports (RPC and P2P). If cfg.DataDir is empty, it uses a directory
// under tb.TempDir().
// Other fields are used as given.
//
// When no bitcoind binary is found, StartT skips the test, unless the CI
// environment variable is set, in which case it fails the test so a broken CI
// setup is not reported as green. Other startup errors fail the test. If the
// test fails, StartT logs bitcoind's last output lines.
//
// Example:
//
//	func TestSend(t *testing.T) {
//	    t.Parallel()
//	    rt := regtest.StartT(t, nil)
//	    if err := rt.EnsureWallet("w"); err != nil {
//	        t.Fatal(err)
//	    }
//	    // ...
//	}
func StartT(tb testing.TB, cfg *Config) *Regtest {
	tb.Helper()

	c := &Config{}
	if cfg != nil {
		c = cfg.clone()
	}
	if c.DataDir == "" {
		c.DataDir = filepath.Join(tb.TempDir(), "bitcoind")
	}
	pickPorts := c.Host == ""

	// Ports are checked free, then released before bitcoind binds them, so
	// another process can occasionally take one in between. Retry with new
	// ports in that case.
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if pickPorts {
			port, err := reservePorts()
			if err != nil {
				tb.Fatalf("regtest: pick ports: %v", err)
				return nil
			}
			c.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		}

		rt, err := New(c)
		if errors.Is(err, ErrBinaryNotFound) {
			if os.Getenv("CI") != "" {
				tb.Fatalf("regtest: %v", err)
			} else {
				tb.Skipf("regtest: %v", err)
			}
			return nil
		}
		if err != nil {
			tb.Fatalf("regtest: %v", err)
			return nil
		}

		lastErr = rt.Start()
		if lastErr == nil {
			tb.Cleanup(func() {
				if tb.Failed() {
					if out := rt.recentOutput(50); out != "" {
						tb.Logf("regtest: last bitcoind output (%s):\n%s", rt.config.Host, out)
					}
				}
				if err := rt.Stop(); err != nil {
					tb.Errorf("regtest: stop: %v", err)
				}
			})
			return rt
		}
		if !pickPorts || !isPortConflict(lastErr) {
			break
		}
	}
	tb.Fatalf("regtest: start: %v", lastErr)
	return nil
}

// recentOutput returns up to n of the running bitcoind's last output lines.
func (r *Regtest) recentOutput(n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.proc == nil {
		return ""
	}
	return r.proc.output.lastLines(n)
}

// isPortConflict reports whether a Start error means a port was taken.
func isPortConflict(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already in use") || strings.Contains(msg, "Unable to bind")
}

// handedOut records base ports reservePorts has returned in this process, so
// parallel tests never get overlapping ranges even after a node stops.
var (
	handedOutMu sync.Mutex
	handedOut   = map[int]bool{}
)

// reservePorts returns a base port p such that p (RPC) and p+1 (P2P) are free
// on this machine and overlap no range handed out earlier in this process.
// Other processes can still take them before bitcoind binds; Start detects
// that and StartT retries.
func reservePorts() (int, error) {
	handedOutMu.Lock()
	defer handedOutMu.Unlock()

	for tries := 0; tries < 50; tries++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		base := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()

		if base+1 > 65535 || overlapsHandedOut(base) {
			continue
		}
		if portFree("127.0.0.1", base) && portFree("127.0.0.1", base+1) {
			handedOut[base] = true
			return base, nil
		}
	}
	return 0, fmt.Errorf("no free pair of consecutive ports found")
}

// overlapsHandedOut reports whether [base, base+1] intersects a range
// returned earlier. Caller holds handedOutMu.
func overlapsHandedOut(base int) bool {
	for b := base - 1; b <= base+1; b++ {
		if handedOut[b] {
			return true
		}
	}
	return false
}

// portFree reports whether a TCP listener can bind host:port.
func portFree(host string, port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}
