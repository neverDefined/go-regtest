package regtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Test_CheckDataDir pins the directories Start refuses to delete. Nothing is
// deleted here.
func Test_CheckDataDir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	refused := []string{"/", ".", "..", "./", wd, filepath.Dir(wd)}
	if home, err := os.UserHomeDir(); err == nil {
		refused = append(refused, home)
	}
	for _, d := range refused {
		if _, err := checkDataDir(d); err == nil {
			t.Errorf("checkDataDir(%q) should refuse", d)
		}
	}
	allowed := []string{"./bitcoind_regtest", "sub/dir", filepath.Join(t.TempDir(), "node")}
	for _, d := range allowed {
		abs, err := checkDataDir(d)
		if err != nil {
			t.Errorf("checkDataDir(%q): %v", d, err)
		}
		if !filepath.IsAbs(abs) {
			t.Errorf("checkDataDir(%q) = %q, want an absolute path", d, abs)
		}
	}
	if _, err := New(&Config{DataDir: "."}); err == nil {
		t.Error(`New should reject DataDir "."`)
	}
}

// Test_Stop_NeverStarted_KeepsDataDir checks Stop on an instance that never
// started leaves an existing directory alone.
func Test_Stop_NeverStarted_KeepsDataDir(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := New(&Config{Host: "127.0.0.1:19840", DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("Stop deleted a directory it did not create: %v", err)
	}
}

// Test_ExtraArgs_DaemonOverridden checks -daemon in ExtraArgs can't detach
// bitcoind from the process that Stop manages.
func Test_ExtraArgs_DaemonOverridden(t *testing.T) {
	rt, err := New(&Config{
		Host:      "127.0.0.1:19850",
		DataDir:   filepath.Join(t.TempDir(), "d"),
		ExtraArgs: []string{"-daemon=1", "-daemonwait=1"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if running, _ := rt.IsRunning(); running {
		t.Error("bitcoind still running after Stop: -daemon escaped process management")
	}
}

// Test_Stop_OutputHolderDoesNotBlock checks Stop returns promptly even when a
// command bitcoind spawned keeps its stdout open after bitcoind exits. The
// sleep runs in the background: bitcoind waits for -shutdownnotify commands
// themselves, but not for what they leave running.
func Test_Stop_OutputHolderDoesNotBlock(t *testing.T) {
	rt, err := New(&Config{
		Host:      "127.0.0.1:19860",
		DataDir:   filepath.Join(t.TempDir(), "d"),
		ExtraArgs: []string{"-shutdownnotify=sleep 60 &"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rt.Start(); err != nil {
		if strings.Contains(err.Error(), "shutdownnotify") {
			t.Skipf("bitcoind lacks -shutdownnotify: %v", err)
		}
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })

	start := time.Now()
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("Stop took %s; it waited for the -shutdownnotify child", d)
	}
}

// Test_StartContext_ExpiresWhileWaitingForLock checks a StartContext whose
// context expires while another lifecycle call holds the lock does not touch
// the data dir or launch bitcoind.
func Test_StartContext_ExpiresWhileWaitingForLock(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "d")
	rt, err := New(&Config{Host: "127.0.0.1:19870", DataDir: dataDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })

	rt.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- rt.StartContext(ctx) }()
	<-ctx.Done()
	rt.mu.Unlock()

	if err := <-errc; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if rt.proc != nil {
		t.Error("bitcoind was launched after the context expired")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("data dir was created after the context expired (stat err %v)", err)
	}
}
