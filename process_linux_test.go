//go:build linux

package regtest

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const orphanChildEnv = "GO_REGTEST_ORPHAN_CHILD_HOST"

// TestHelperOrphanChild is not a real test. Test_KilledParent_StopsBitcoind
// runs the test binary again with only this test selected; it starts a node,
// prints "ready", and blocks until it is killed.
func TestHelperOrphanChild(t *testing.T) {
	host := os.Getenv(orphanChildEnv)
	if host == "" {
		t.Skip("helper process for Test_KilledParent_StopsBitcoind")
	}
	rt, err := New(&Config{Host: host, DataDir: filepath.Join(os.TempDir(), "go-regtest-orphan-"+filepath.Base(host))})
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	if err := rt.Start(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("ready")
	select {}
}

// Test_KilledParent_StopsBitcoind checks that bitcoind does not outlive a Go
// process that is killed without calling Stop (go test timeout, Ctrl-C,
// kill -9). Without the parent-death signal the node would keep its ports.
func Test_KilledParent_StopsBitcoind(t *testing.T) {
	const host = "127.0.0.1:19830"
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperOrphanChild$", "-test.v")
	cmd.Env = append(os.Environ(), orphanChildEnv+"="+host)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if line := sc.Text(); line == "ready" || len(line) > 6 && line[:6] == "error:" {
				ready <- line
				return
			}
		}
		ready <- "child exited before ready"
	}()
	select {
	case line := <-ready:
		if line != "ready" {
			t.Fatalf("child: %s", line)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("child did not start a node in time")
	}

	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond)
		if err != nil {
			return // port closed: bitcoind went down with its parent
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("bitcoind still listening on %s 15s after its parent was killed", host)
}
