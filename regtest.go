// Package regtest provides a lightweight Go library for managing Bitcoin Core
// regtest environments. See doc.go for detailed documentation.
package regtest

import (
	"context"
	"encoding/json"
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

// errNotConnected is returned by RPC methods called before Start() or after Stop().
var errNotConnected = errors.New("RPC client not connected")

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

// Config holds the configuration for the Bitcoin regtest environment.
// Empty Host, User, Pass and DataDir fields fall back to DefaultConfig's values.
type Config struct {
	// RPC connection settings
	Host string // RPC host:port (default: "127.0.0.1:18443"). The P2P port is RPC port + 1.
	User string // RPC username (default: "user")
	Pass string // RPC password (default: "pass")

	// DataDir is bitcoind's data directory (default: "./bitcoind_regtest").
	// It is deleted and recreated by Start, and deleted by Stop.
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
	rpcPort      int
	p2pPort      int

	mu   sync.Mutex // guards lifecycle (Start/Stop) and proc
	proc *process   // the running bitcoind; nil when stopped

	client   *rpcclient.Client
	clientMu sync.RWMutex

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

	bitcoindPath, err := resolveBitcoind(cfg.BinaryPath)
	if err != nil {
		return nil, err
	}

	return &Regtest{
		config:       cfg,
		bitcoindPath: bitcoindPath,
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

	r.mu.Lock()
	defer r.mu.Unlock()

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
	if r.rpcPortOpen(ctx) {
		return fmt.Errorf("RPC port %d is already in use (another bitcoind running?)", r.rpcPort)
	}

	dataDir, err := filepath.Abs(r.config.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("clear data dir: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	p, err := startProcess(r.bitcoindPath, r.bitcoindArgs(dataDir))
	if err != nil {
		return fmt.Errorf("start bitcoind: %w", err)
	}
	if err := r.connectClient(); err != nil {
		p.kill()
		return err
	}
	if err := r.waitReady(ctx, p); err != nil {
		r.disconnectClient()
		p.kill()
		return err
	}
	r.proc = p
	return nil
}

// bitcoindArgs returns the full bitcoind command line for this instance.
func (r *Regtest) bitcoindArgs(dataDir string) []string {
	extra := r.config.renderExtraArgs()
	args := make([]string, 0, 11+len(extra))
	args = append(args,
		"-regtest",
		"-datadir="+dataDir,
		"-server",
		"-rpcuser="+r.config.User,
		"-rpcpassword="+r.config.Pass,
		"-rpcport="+strconv.Itoa(r.rpcPort),
		"-port="+strconv.Itoa(r.p2pPort),
		"-rpcbind=127.0.0.1",
		"-rpcallowip=127.0.0.1",
		"-fallbackfee=0.0002",
		"-txindex",
	)
	return append(args, extra...)
}

// waitReady polls until bitcoind answers getblockcount, the process exits, or
// ctx (bounded to startTimeout if it has no deadline) expires.
func (r *Regtest) waitReady(ctx context.Context, p *process) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startTimeout)
		defer cancel()
	}
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()
	for {
		if r.rpcReady(ctx) {
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("bitcoind exited during startup (%v). Last output:\n%s", p.err, p.output.lastLines(20))
		case <-ctx.Done():
			return fmt.Errorf("bitcoind not ready: %w. Last output:\n%s", ctx.Err(), p.output.lastLines(20))
		case <-ticker.C:
		}
	}
}

// rpcReady reports whether bitcoind answers getblockcount. bitcoind accepts
// RPC connections during warm-up but answers with an error until it is ready.
func (r *Regtest) rpcReady(ctx context.Context) bool {
	// Check the port first: btcd's client retries refused connections with
	// a multi-second backoff, which would make each poll slow.
	if !r.rpcPortOpen(ctx) {
		return false
	}
	client, err := r.lockedClient()
	if err != nil {
		return false
	}
	_, err = runWithContext(ctx, client.GetBlockCount)
	return err == nil
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
// Config.DataDir. If bitcoind has not exited when ctx is done (30 seconds
// when ctx has no deadline), it is killed. Calling StopContext on an instance
// that is not running only deletes the data directory. Safe to call more
// than once.
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

	if err := os.RemoveAll(r.config.DataDir); err != nil {
		return fmt.Errorf("remove data dir: %w", err)
	}
	return nil
}

// shutdownProcess sends the stop RPC and waits for p to exit, killing it if
// ctx expires first.
func (r *Regtest) shutdownProcess(ctx context.Context, p *process) {
	if p.exited() {
		return
	}
	if client, err := r.lockedClient(); err == nil {
		// Errors are ignored: if the RPC fails the kill below still runs.
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = runWithContext(stopCtx, func() (json.RawMessage, error) {
			return client.RawRequest("stop", nil)
		})
		cancel()
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

// disconnectClient shuts down and clears the RPC client, if any.
func (r *Regtest) disconnectClient() {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if r.client != nil {
		r.client.Shutdown()
		r.client = nil
	}
}
