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
// the data dir or launch bitcoind. The sentinel file would be deleted by
// Start's data-dir reset if the post-lock check were missing.
func Test_StartContext_ExpiresWhileWaitingForLock(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "d")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dataDir, "sentinel")
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := New(&Config{Host: "127.0.0.1:19870", DataDir: dataDir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop() })

	rt.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- rt.StartContext(ctx) }()
	time.Sleep(100 * time.Millisecond) // let it pass the entry check and block on the lock
	<-ctx.Done()
	rt.mu.Unlock()

	if err := <-errc; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if rt.proc != nil {
		t.Error("bitcoind was launched after the context expired")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("data dir was reset after the context expired: %v", err)
	}
}

// Test_CheckDataDir_SymlinkedParent checks a path that reaches the working
// directory through a symlinked parent is refused. Nothing is deleted.
func Test_CheckDataDir_SymlinkedParent(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	repo := filepath.Join(work, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(work, alias); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	for _, d := range []string{filepath.Join(alias, "repo"), alias} {
		if _, err := checkDataDir(d); err == nil {
			t.Errorf("checkDataDir(%q) should refuse: it resolves to the working directory or a parent", d)
		}
	}
	if _, err := checkDataDir(filepath.Join(alias, "repo", "node")); err != nil {
		t.Errorf("a child of the working directory should be allowed: %v", err)
	}
}

// Test_BitcoindArgs pins flags that process management depends on.
func Test_BitcoindArgs(t *testing.T) {
	rt, err := New(&Config{Host: "127.0.0.1:19890", ExtraArgs: []string{"-daemon=1"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	args := rt.bitcoindArgs("/data")
	n := len(args)
	if n < 2 || args[n-2] != "-daemon=0" || args[n-1] != "-daemonwait=0" {
		t.Errorf("args must end with -daemon=0 -daemonwait=0 so they override ExtraArgs, got %v", args[n-2:])
	}
	want := []string{"-rpcport=19890", "-bind=127.0.0.1:19891", "-listenonion=0", "-datadir=/data"}
	for _, a := range args {
		if strings.HasSuffix(a, "=onion") {
			t.Errorf("unexpected Tor listener bind %s", a)
		}
	}
	for _, w := range want {
		found := false
		for _, a := range args {
			found = found || a == w
		}
		if !found {
			t.Errorf("missing %s in %v", w, args)
		}
	}
}

// Test_ServesOurDataDir checks readiness can tell our node from another one
// answering on the same port with the same credentials.
func Test_ServesOurDataDir(t *testing.T) {
	ours, err := New(&Config{Host: "127.0.0.1:19900", DataDir: filepath.Join(t.TempDir(), "a")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ours.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ours.Stop() })

	other, err := New(&Config{Host: "127.0.0.1:19900", DataDir: filepath.Join(t.TempDir(), "b")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := other.connectClient(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.disconnectClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !ours.servesOurDataDir(ctx) {
		t.Error("the node should be recognised by the instance that started it")
	}
	if other.servesOurDataDir(ctx) {
		t.Error("another instance must not accept this node as its own")
	}
}
