// Package regtest provides a lightweight Go library for managing Bitcoin Core
// regtest environments. See doc.go for detailed documentation.
package regtest

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/rpcclient"
)

// ErrNotConnected is returned by RPC methods called before Start or after
// Stop. Test for it with errors.Is.
var ErrNotConnected = errors.New("RPC client not connected")

// ErrBinaryNotFound is returned by New when no bitcoind binary can be found,
// either at Config.BinaryPath or on PATH.
var ErrBinaryNotFound = errors.New("bitcoind binary not found")

const (
	// startTimeout bounds StartContext when the caller's context has no
	// deadline. Generous because cold starts on CI runners can be slow.
	startTimeout = 60 * time.Second
	// stopTimeout bounds StopContext when the caller's context has no
	// deadline. After it expires bitcoind is killed.
	stopTimeout = 30 * time.Second
	// readyPollInterval is how often StartContext checks whether RPC is up.
	readyPollInterval = 50 * time.Millisecond
)

// testHookBeforeStartLock, if set by a test, runs in StartContext after the
// entry ctx check and before taking the lifecycle lock.
var testHookBeforeStartLock func()

// Config holds the configuration for the Bitcoin regtest environment.
// Empty Host, User, Pass and DataDir fields fall back to DefaultConfig's values.
type Config struct {
	// RPC connection settings
	// Host is the RPC address (default: "127.0.0.1:18443"). bitcoind listens
	// on 127.0.0.1 only: RPC on this port and P2P on port+1.
	Host string
	User string // RPC username (default: "user")
	Pass string // RPC password (default: "pass")

	// DataDir is bitcoind's data directory (default: "./bitcoind_regtest").
	// Start deletes and recreates it; Stop deletes it if Start created it.
	// New rejects the filesystem root, the home directory, and the working
	// directory or any of its parents.
	DataDir string

	// Additional bitcoind arguments (optional)
	// Example: []string{"-txindex=1", "-fallbackfee=0.0001"}
	ExtraArgs []string

	// VBParams configures named BIP9 deployments. Each entry renders to one
	// -vbparams=<name>:<start>:<timeout>:<min_activation_height> flag. See
	// VBParam, VBAlwaysActive, and VBNeverActive in softfork.go.
	VBParams []VBParam

	// AcceptNonstdTxn maps to -acceptnonstdtxn=1 when true. Pre-standardness
	// soft-fork transactions (APO sigs, CTV-committed outputs, etc.) are
	// consensus-valid but mempool-rejected by default; flip this on for any
	// test that needs to broadcast such a tx through the mempool. Default false.
	AcceptNonstdTxn bool

	// BinaryPath overrides the bitcoind binary used by Start.
	//
	// When empty (the default), the harness searches PATH for
	// bitcoind-inquisition first, then falls back to bitcoind. Set this to
	// run against a non-standard build (e.g. /opt/bitcoin/bin/bitcoind)
	// without modifying PATH. Accepts an absolute path, a relative path, or a
	// bare name resolved via PATH.
	BinaryPath string
}

// clone returns a deep copy of c.
func (c *Config) clone() *Config {
	cp := *c
	cp.ExtraArgs = append([]string(nil), c.ExtraArgs...)
	cp.VBParams = append([]VBParam(nil), c.VBParams...)
	return &cp
}

// applyDefaults fills empty connection and data-dir fields from DefaultConfig.
func (c *Config) applyDefaults() {
	d := DefaultConfig()
	if c.Host == "" {
		c.Host = d.Host
	}
	if c.User == "" {
		c.User = d.User
	}
	if c.Pass == "" {
		c.Pass = d.Pass
	}
	if c.DataDir == "" {
		c.DataDir = d.DataDir
	}
}

// Regtest manages a Bitcoin regtest node instance.
// Each instance can run independently with its own configuration.
// This design allows multiple regtest nodes to run simultaneously
// on different ports with different configurations.
type Regtest struct {
	config       *Config
	bitcoindPath string // resolved absolute path to bitcoind
	dataDir      string // absolute, checked by checkDataDir
	rpcPort      int
	p2pPort      int

	mu          sync.Mutex // guards lifecycle (Start/Stop), proc and ownsDataDir
	proc        *process   // the running bitcoind; nil when stopped
	ownsDataDir bool       // Start created dataDir and Stop has not removed it yet

	client        *rpcclient.Client
	walletClients map[string]*rpcclient.Client // per-wallet clients, see walletClient
	clientMu      sync.RWMutex                 // guards client and walletClients

	// variantMu guards variantCached / variant. The first VariantContext
	// call hits getnetworkinfo; subsequent calls return the cached value.
	variantMu     sync.Mutex
	variantCached bool
	variant       Variant
}

// New creates a Regtest instance. A nil config uses DefaultConfig. New
// validates the config and resolves the bitcoind binary (Config.BinaryPath if
// set, otherwise bitcoind-inquisition then bitcoind on PATH) but does not
// start anything.
//
// Returns ErrBinaryNotFound (errors.Is) when no bitcoind binary is found.
//
// Example:
//
//	rt, err := regtest.New(nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if err := rt.Start(); err != nil {
//	    log.Fatal(err)
//	}
//	defer rt.Stop()
func New(config *Config) (*Regtest, error) {
	cfg := DefaultConfig()
	if config != nil {
		cfg = config.clone()
		cfg.applyDefaults()
	}

	// An empty Deployment is a configuration mistake; catch it here rather
	// than let bitcoind silently ignore the flag.
	for i, vb := range cfg.VBParams {
		if vb.Deployment == "" {
			return nil, fmt.Errorf("VBParams[%d].Deployment must not be empty", i)
		}
	}

	rpcPort, err := parseRPCPort(cfg.Host)
	if err != nil {
		return nil, err
	}

	dataDir, err := checkDataDir(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	bitcoindPath, err := resolveBitcoind(cfg.BinaryPath)
	if err != nil {
		return nil, err
	}

	return &Regtest{
		config:       cfg,
		bitcoindPath: bitcoindPath,
		dataDir:      dataDir,
		rpcPort:      rpcPort,
		p2pPort:      rpcPort + 1,
	}, nil
}

// DefaultConfig returns a new Config with default regtest settings:
// Host "127.0.0.1:18443", User "user", Pass "pass", DataDir
// "./bitcoind_regtest".
func DefaultConfig() *Config {
	return &Config{
		Host:      "127.0.0.1:18443",
		User:      "user",
		Pass:      "pass",
		DataDir:   "./bitcoind_regtest",
		ExtraArgs: nil,
	}
}

// Config returns a copy of this instance's configuration, with defaults
// applied.
func (r *Regtest) Config() *Config {
	return r.config.clone()
}

// RPCConfig returns an RPC client configuration for connecting to this regtest node.
// This uses the configuration provided when creating the Regtest instance.
//
// Example:
//
//	client, err := rpcclient.New(rt.RPCConfig(), nil)
func (r *Regtest) RPCConfig() *rpcclient.ConnConfig {
	return &rpcclient.ConnConfig{
		Host:         r.config.Host,
		User:         r.config.User,
		Pass:         r.config.Pass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}
}

// Start starts bitcoind and waits until it answers RPC calls. It is
// StartContext with context.Background(), which bounds startup to 60 seconds.
func (r *Regtest) Start() error {
	return r.StartContext(context.Background())
}

// StartContext starts bitcoind as a child process and waits until it answers
// RPC calls. ctx bounds the startup wait only; the node keeps running after
// StartContext returns, until Stop. Without a deadline on ctx, startup is
// bounded to 60 seconds.
//
// Start wipes Config.DataDir first, so every Start begins at the genesis
// block. It fails if the RPC port is already in use or the instance is
// already running. On failure the error includes bitcoind's last output lines.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	if err := rt.StartContext(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer rt.Stop()
func (r *Regtest) StartContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start cancelled: %w", err)
	}
	if testHookBeforeStartLock != nil {
		testHookBeforeStartLock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// ctx may have expired while waiting for the lock.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start cancelled: %w", err)
	}

	if r.proc != nil {
		if !r.proc.exited() {
			return fmt.Errorf("bitcoind already started on port %d", r.rpcPort)
		}
		// The previous node died on its own; allow a fresh start.
		r.proc = nil
		r.disconnectClient()
	}

	// Refuse to start if something already listens on the RPC port. Without
	// this check the readiness probe could talk to that other process (for
	// example a node left over from a killed test run) while our bitcoind
	// fails to bind and exits.
	portInUse := r.rpcPortOpen(ctx)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start cancelled: %w", err)
	}
	if portInUse {
		return fmt.Errorf("RPC port %d is already in use (another bitcoind running?)", r.rpcPort)
	}

	// Re-check: the working directory may have changed since New.
	if _, err := checkDataDir(r.dataDir); err != nil {
		return err
	}
	if err := os.RemoveAll(r.dataDir); err != nil {
		return fmt.Errorf("clear data dir: %w", err)
	}
	if err := os.MkdirAll(r.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	r.ownsDataDir = true

	p, err := r.launch(ctx)
	if err != nil {
		_ = os.RemoveAll(r.dataDir)
		r.ownsDataDir = false
		return err
	}
	r.proc = p
	return nil
}

// launch starts bitcoind, connects the RPC client, and waits until the node
// is ready. On failure it disconnects and kills what it started.
func (r *Regtest) launch(ctx context.Context) (*process, error) {
	probe, err := newStartProbe()
	if err != nil {
		return nil, err
	}
	p, err := startProcess(r.bitcoindPath, r.bitcoindArgs(r.dataDir, probe))
	if err != nil {
		return nil, fmt.Errorf("start bitcoind: %w", err)
	}
	if err = r.connectClient(); err == nil {
		err = r.waitReady(ctx, p, probe)
	}
	if err != nil {
		r.disconnectClient()
		p.kill()
		return nil, err
	}
	return p, nil
}

// bitcoindArgs returns the full bitcoind command line for this instance.
func (r *Regtest) bitcoindArgs(dataDir string, probe startProbe) []string {
	extra := r.config.renderExtraArgs()
	args := make([]string, 0, 18+len(extra))
	args = append(args,
		"-regtest",
		"-datadir="+dataDir,
		"-server",
		"-rpcuser="+r.config.User,
		"-rpcpassword="+r.config.Pass,
		"-rpcport="+strconv.Itoa(r.rpcPort),
		"-port="+strconv.Itoa(r.p2pPort),
		// Don't contact a local Tor daemon to publish an onion service.
		"-listenonion=0",
		"-rpcbind=127.0.0.1",
		"-rpcallowip=127.0.0.1",
		"-fallbackfee=0.0002",
		"-txindex",
	)
	// An explicit bind keeps P2P on loopback. Without one, bitcoind listens
	// on all interfaces and adds a Tor listener on another port (a fixed
	// 18445 on Core 28 regtest, so parallel nodes collide). bitcoind rejects
	// -bind together with -listen=0, so skip it when listening is off.
	if !listenDisabled(extra) {
		args = append(args, "-bind=127.0.0.1:"+strconv.Itoa(r.p2pPort))
	}
	args = append(args, extra...)
	// The probe goes after ExtraArgs so -norpcauth there can't remove it.
	// If the caller's RPC whitelist already denies unlisted users, let the
	// probe call the one method readiness needs. Otherwise add nothing: a
	// whitelist entry would itself switch on default-deny.
	args = append(args, probe.rpcauthArg())
	if rpcDefaultDeny(extra) {
		args = append(args, "-rpcwhitelist="+probe.user+":getblockcount")
	}
	// bitcoind must stay a foreground child so Stop can manage it. The last
	// value of a repeated flag wins, so these override ExtraArgs and any
	// bitcoin.conf setting.
	return append(args, "-daemon=0", "-daemonwait=0")
}

// waitReady polls until bitcoind answers getblockcount with the probe
// credential, the process exits, or ctx (bounded to startTimeout if it has no
// deadline) expires.
func (r *Regtest) waitReady(ctx context.Context, p *process, probe startProbe) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startTimeout)
		defer cancel()
	}
	client, err := r.probeClient(probe)
	if err != nil {
		return err
	}
	defer client.Shutdown()
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		if r.rpcReady(ctx, client) {
			return nil
		}
		select {
		case <-p.done:
			if p.err != nil {
				return fmt.Errorf("bitcoind exited during startup: %w. Last output:\n%s", p.err, p.output.lastLines(20))
			}
			return fmt.Errorf("bitcoind exited during startup with status 0. Last output:\n%s", p.output.lastLines(20))
		case <-ctx.Done():
			return fmt.Errorf("bitcoind not ready: %w. Last output:\n%s", ctx.Err(), p.output.lastLines(20))
		case <-ticker.C:
		}
	}
}

// rpcReady reports whether our bitcoind answers getblockcount through
// client, which uses the probe credential. bitcoind accepts RPC connections
// during warm-up but answers with an error until it is ready.
func (r *Regtest) rpcReady(ctx context.Context, client *rpcclient.Client) bool {
	// Check the port first: btcd's client retries refused connections with
	// a multi-second backoff, which would make each poll slow.
	if !r.rpcPortOpen(ctx) {
		return false
	}
	_, err := runWithContext(ctx, client.GetBlockCount)
	return err == nil
}

// startProbe is a one-time RPC credential that Start adds with -rpcauth and
// uses for the readiness check. Another process can take the RPC port
// between our free-port check and bitcoind binding it (for example a
// parallel test binary). That node may share Config.User/Pass, but it
// rejects the probe, so readiness can't succeed against it.
type startProbe struct{ user, pass, salt string }

func newStartProbe() (startProbe, error) {
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		return startProbe{}, fmt.Errorf("generate probe credential: %w", err)
	}
	return startProbe{
		user: "go-regtest-probe-" + hex.EncodeToString(b[:8]),
		pass: hex.EncodeToString(b[8:24]),
		salt: hex.EncodeToString(b[24:]),
	}, nil
}

// rpcauthArg renders the -rpcauth flag the way Core's share/rpcauth tool
// does: user:salt$hex(HMAC-SHA256(key=salt, message=password)).
func (s startProbe) rpcauthArg() string {
	mac := hmac.New(sha256.New, []byte(s.salt))
	mac.Write([]byte(s.pass))
	return "-rpcauth=" + s.user + ":" + s.salt + "$" + hex.EncodeToString(mac.Sum(nil))
}

// probeClient returns an RPC client that authenticates with probe.
func (r *Regtest) probeClient(probe startProbe) (*rpcclient.Client, error) {
	cfg := r.RPCConfig()
	cfg.User, cfg.Pass = probe.user, probe.pass
	client, err := rpcclient.New(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("create probe RPC client: %w", err)
	}
	return client, nil
}

// listenDisabled reports whether args turn off P2P listening, as bitcoind
// reads them.
func listenDisabled(args []string) bool {
	listen, set := lastBool(args, "listen")
	return set && !listen
}

// rpcDefaultDeny reports whether args make bitcoind deny RPC calls from
// users without a -rpcwhitelist entry: -rpcwhitelistdefault if given,
// otherwise whether any whitelist entry remains (-norpcwhitelist clears
// earlier entries).
func rpcDefaultDeny(args []string) bool {
	if deny, set := lastBool(args, "rpcwhitelistdefault"); set {
		return deny
	}
	entries := 0
	for _, a := range args {
		name, value, hasValue := splitFlag(a)
		switch {
		case name == "rpcwhitelist":
			entries++
		case name == "norpcwhitelist" && (!hasValue || coreBool(value)):
			entries = 0
		}
	}
	return entries > 0
}

// lastBool returns the value of the boolean flag name as bitcoind reads
// args: the last -name or -noname wins, either with one or two dashes, and
// values are read by coreBool. set is false if neither appears.
func lastBool(args []string, name string) (value, set bool) {
	for _, a := range args {
		n, v, hasValue := splitFlag(a)
		b := !hasValue || coreBool(v)
		switch n {
		case name:
			value, set = b, true
		case "no" + name:
			value, set = !b, true
		}
	}
	return value, set
}

// splitFlag splits "-name=value" (or "--name=value") into its parts.
func splitFlag(arg string) (name, value string, hasValue bool) {
	return strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
}

// coreBool interprets a flag value like bitcoind's InterpretBool: empty is
// true; otherwise leading whitespace is skipped and the leading integer is
// read (none means 0, overflow saturates) and compared with 0. So "false"
// and "no" are false, "2" and " 1" are true.
func coreBool(v string) bool {
	if v == "" {
		return true
	}
	v = strings.TrimLeft(v, " \t\n\v\f\r")
	i := 0
	if i < len(v) && (v[i] == '+' || v[i] == '-') {
		i++
	}
	for _, c := range v[i:] {
		if c < '0' || c > '9' {
			break
		}
		if c != '0' {
			return true // any non-zero digit: non-zero, even if it would overflow
		}
	}
	return false
}

// rpcPortOpen reports whether something accepts TCP connections on the RPC
// address.
func (r *Regtest) rpcPortOpen(ctx context.Context) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", r.config.Host)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Stop stops bitcoind and deletes Config.DataDir. It is StopContext with
// context.Background(), which waits up to 30 seconds before killing bitcoind.
func (r *Regtest) Stop() error {
	return r.StopContext(context.Background())
}

// StopContext asks bitcoind to shut down, waits for it to exit, and deletes
// the data directory Start created. If bitcoind has not exited when ctx is
// done (30 seconds when ctx has no deadline), it is killed. StopContext only
// touches a node and directory this instance started; on any other instance
// it does nothing. Safe to call more than once.
//
// Example:
//
//	if err := rt.Start(); err != nil {
//	    return err
//	}
//	defer rt.Stop()
func (r *Regtest) StopContext(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, stopTimeout)
		defer cancel()
	}

	if p := r.proc; p != nil {
		r.proc = nil
		r.shutdownProcess(ctx, p)
	}
	r.disconnectClient()

	if r.ownsDataDir {
		if err := os.RemoveAll(r.dataDir); err != nil {
			return fmt.Errorf("remove data dir: %w", err)
		}
		r.ownsDataDir = false
	}
	return nil
}

// shutdownProcess asks p to shut down and waits for it to exit, killing it
// if ctx expires first.
func (r *Regtest) shutdownProcess(ctx context.Context, p *process) {
	if p.exited() {
		return
	}
	// bitcoind treats SIGTERM like the stop RPC. Signalling our own PID,
	// rather than sending stop to whatever answers on the port, can't shut
	// down some other node. If signals aren't supported, kill.
	if err := p.terminate(); err != nil {
		p.kill()
		return
	}
	select {
	case <-p.done:
	case <-ctx.Done():
		p.kill()
	}
}

// Cleanup is kept for compatibility and always returns nil. Earlier versions
// wrote a helper script to a temporary directory that Cleanup removed; Start
// and Stop no longer create temporary files.
func (r *Regtest) Cleanup() error {
	return nil
}

// IsRunning checks if the Bitcoin regtest node is currently running by
// attempting a short-timeout RPC call against the configured host. It does not
// depend on this instance having started the node.
//
// Returns true if the node responds to a getblockcount call within ~2 seconds.
// Returns false (with nil error) if the connection is refused or times out —
// the typical "not running" signals. Other RPC errors (auth failures, malformed
// responses) are propagated.
//
// Example:
//
//	running, err := rt.IsRunning()
//	if err != nil {
//	    return fmt.Errorf("failed to check node status: %w", err)
//	}
func (r *Regtest) IsRunning() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.IsRunningContext(ctx)
}

// IsRunningContext is the context-aware variant of IsRunning. The supplied ctx
// bounds how long this call will wait for the node to respond.
func (r *Regtest) IsRunningContext(ctx context.Context) (bool, error) {
	if !r.rpcPortOpen(ctx) {
		if errors.Is(ctx.Err(), context.Canceled) {
			return false, ctx.Err()
		}
		return false, nil
	}

	// Use the live client if Start() has been called; otherwise build an
	// ephemeral one so callers can probe the node before / after lifecycle calls.
	client, err := r.lockedClient()
	if err != nil {
		ephemeral, newErr := rpcclient.New(r.RPCConfig(), nil)
		if newErr != nil {
			return false, fmt.Errorf("failed to create RPC client for status check: %w", newErr)
		}
		defer ephemeral.Shutdown()
		client = ephemeral
	}

	_, err = runWithContext(ctx, func() (int64, error) {
		return client.GetBlockCount()
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// Treat "no response in time" as not running, but surface an
		// explicit cancellation to the caller.
		if errors.Is(ctx.Err(), context.Canceled) {
			return false, ctx.Err()
		}
		return false, nil
	}
	if isConnRefusedErr(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check bitcoind status: %w", err)
}

// isConnRefusedErr returns true when err looks like a TCP connection refusal
// or similar "nobody is listening" condition, which we treat as "not running".
func isConnRefusedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF")
}

// parseRPCPort returns the port in a host:port address. The port must leave
// room for the P2P port at port+1.
func parseRPCPort(hostport string) (int, error) {
	_, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return 0, fmt.Errorf("Config.Host %q: %w", hostport, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65534 {
		return 0, fmt.Errorf("Config.Host %q: port must be a number from 1 to 65534", hostport)
	}
	return port, nil
}

// checkDataDir returns the absolute form of dir, or an error if deleting it
// recursively would be dangerous: the filesystem root, the home directory,
// or the working directory or one of its parents.
func checkDataDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("Config.DataDir %q: %w", dir, err)
	}
	refuse := func(why string) (string, error) {
		return "", fmt.Errorf("Config.DataDir %q is %s; Start would delete it", dir, why)
	}
	// Compare symlink-resolved paths: RemoveAll follows symlinked parent
	// directories, so /tmp/alias/repo can mean the working directory.
	target := canonicalPath(abs)
	if filepath.Dir(target) == target {
		return refuse("the filesystem root")
	}
	if home, err := os.UserHomeDir(); err == nil && target == canonicalPath(home) {
		return refuse("the home directory")
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(target, canonicalPath(wd)); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return refuse("the working directory or one of its parents")
		}
	}
	return abs, nil
}

// canonicalPath resolves symlinks in the longest existing prefix of the
// absolute path p and appends the part that doesn't exist yet.
func canonicalPath(p string) string {
	existing, rest := filepath.Clean(p), ""
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return filepath.Clean(p)
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

// resolveBitcoind picks the bitcoind binary. A non-empty path is resolved via
// exec.LookPath, so absolute paths, relative paths and bare names all work.
// An empty path tries bitcoind-inquisition, then bitcoind, on PATH.
func resolveBitcoind(path string) (string, error) {
	if path != "" {
		p, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("%w: Config.BinaryPath %q: %w", ErrBinaryNotFound, path, err)
		}
		return p, nil
	}
	for _, name := range []string{"bitcoind-inquisition", "bitcoind"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w on PATH (tried bitcoind-inquisition, bitcoind): install Bitcoin Core or set Config.BinaryPath", ErrBinaryNotFound)
}

// connectClient creates and stores the RPC client connection.
func (r *Regtest) connectClient() error {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()

	if r.client != nil {
		return nil // Already connected
	}

	client, err := rpcclient.New(r.RPCConfig(), nil)
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}

	r.client = client
	return nil
}

// disconnectClient shuts down and clears the RPC clients, if any.
func (r *Regtest) disconnectClient() {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if r.client != nil {
		r.client.Shutdown()
		r.client = nil
	}
	for _, c := range r.walletClients {
		c.Shutdown()
	}
	r.walletClients = nil
}
