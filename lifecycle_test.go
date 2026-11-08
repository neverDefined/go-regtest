package regtest

import (
	"context"
	"errors"
	"fmt"
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

	// StartContext checks ctx only after taking the lock, so an already
	// cancelled ctx exercises exactly the lock-wait path.
	rt.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errc := make(chan error, 1)
	go func() { errc <- rt.StartContext(ctx) }()
	rt.mu.Unlock()

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled, got %v", err)
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
	probe, err := newStartProbe()
	if err != nil {
		t.Fatal(err)
	}
	args := rt.bitcoindArgs("/data", probe)
	n := len(args)
	if n < 2 || args[n-2] != "-daemon=0" || args[n-1] != "-daemonwait=0" {
		t.Errorf("args must end with -daemon=0 -daemonwait=0 so they override ExtraArgs, got %v", args[n-2:])
	}
	want := []string{"-rpcport=19890", "-bind=127.0.0.1:19891", "-listenonion=0", "-datadir=/data", probe.rpcauthArg()}
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

	// bitcoind refuses -bind together with -listen=0.
	noListen, err := New(&Config{Host: "127.0.0.1:19890", ExtraArgs: []string{"-listen=0"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, a := range noListen.bitcoindArgs("/data", probe) {
		if strings.HasPrefix(a, "-bind=") {
			t.Errorf("-bind must be omitted with -listen=0, got %s", a)
		}
	}
}

// Test_StartProbe_IdentifiesNode checks the readiness probe accepts only the
// node launched with that probe's credential, even when another instance
// shares the same port and Config.User/Pass.
func Test_StartProbe_IdentifiesNode(t *testing.T) {
	rt, err := New(&Config{Host: "127.0.0.1:19900", DataDir: filepath.Join(t.TempDir(), "a")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ours, err := newStartProbe()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rt.dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := startProcess(rt.bitcoindPath, rt.bitcoindArgs(rt.dataDir, ours))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.kill)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.waitReady(ctx, p, ours); err != nil {
		t.Fatalf("waitReady with the launch probe: %v", err)
	}

	other, err := newStartProbe()
	if err != nil {
		t.Fatal(err)
	}
	client, err := rt.probeClient(other)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown()
	if rt.rpcReady(ctx, client) {
		t.Error("a node must reject another Start's probe credential")
	}
}

// Test_Start_LoggingAndListenOptions checks ExtraArgs that change logging or
// turn off listening still start: readiness must not depend on either.
func Test_Start_LoggingAndListenOptions(t *testing.T) {
	cases := map[string][]string{
		"nodebuglogfile":  {"-nodebuglogfile"},
		"external log":    {"-debuglogfile=" + filepath.Join(t.TempDir(), "node.log")},
		"listen disabled": {"-listen=0"},
	}
	port := 19920
	for name, extra := range cases {
		port += 2
		t.Run(name, func(t *testing.T) {
			rt, err := New(&Config{
				Host:      fmt.Sprintf("127.0.0.1:%d", port),
				DataDir:   filepath.Join(t.TempDir(), "d"),
				ExtraArgs: extra,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := rt.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = rt.Stop() })
			if err := rt.HealthCheck(); err != nil {
				t.Errorf("HealthCheck: %v", err)
			}
		})
	}
}
