package regtest

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/rpcclient"
)

// extractP2PPort returns the P2P listening port for a host string in
// "host:rpcport" form, mirroring scripts/bitcoind_manager.sh which derives
// P2P_PORT = RPC_PORT + 1. Returns the empty string when the host has no
// explicit port (in which case the caller should fall back to the default
// regtest P2P port, 18444).
func extractP2PPort(host string) string {
	idx := strings.LastIndex(host, ":")
	if idx < 0 || idx == len(host)-1 {
		return ""
	}
	rpcStr := host[idx+1:]
	rpc, err := strconv.Atoi(rpcStr)
	if err != nil {
		return ""
	}
	return strconv.Itoa(rpc + 1)
}

// peerAddress builds the "host:p2p_port" address other should be reached at,
// derived from its Config().Host using the script's RPC+1 convention.
func peerAddress(other *Regtest) (string, error) {
	if other == nil {
		return "", fmt.Errorf("peer must not be nil")
	}
	host := other.Config().Host
	idx := strings.LastIndex(host, ":")
	if idx < 0 {
		return "", fmt.Errorf("peer host %q has no port", host)
	}
	p2p := extractP2PPort(host)
	if p2p == "" {
		return "", fmt.Errorf("peer host %q: cannot derive P2P port", host)
	}
	return host[:idx] + ":" + p2p, nil
}

// Connect tells this node to add the other regtest instance as a persistent
// peer. The peer's P2P port is derived from its Config().Host using the
// scripts/bitcoind_manager.sh convention (P2P = RPC + 1).
//
// Connect is asynchronous: bitcoind queues the addnode request and the
// handshake completes shortly after the call returns. Tests that depend on
// the connection being live should poll GetConnectionCount until it sees a
// non-zero value.
//
// Parameters:
//   - other: another running *Regtest instance (must not be nil)
//
// Returns:
//   - error: validation error for nil peer or unparseable host;
//     errNotConnected before Start; otherwise wrapped RPC error.
//
// Example:
//
//	if err := rt1.Connect(rt2); err != nil { return err }
//	// rt1 now has rt2 as a persistent peer.
func (r *Regtest) Connect(other *Regtest) error {
	return r.ConnectContext(context.Background(), other)
}

// ConnectContext is the context-aware variant of Connect.
func (r *Regtest) ConnectContext(ctx context.Context, other *Regtest) error {
	addr, err := peerAddress(other)
	if err != nil {
		return err
	}
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.AddNode(addr, rpcclient.ANAdd)
	})
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	return nil
}

// Disconnect drops the live connection to the other node via the
// disconnectnode RPC. Useful for inducing a network partition in
// reorg/propagation tests.
//
// Parameters:
//   - other: another running *Regtest instance (must not be nil)
//
// Returns:
//   - error: validation error for nil peer or unparseable host;
//     errNotConnected before Start; otherwise wrapped RPC error (including
//     bitcoind's "Node not found in connected nodes" if the peer was never
//     connected).
//
// Example:
//
//	if err := rt1.Disconnect(rt2); err != nil { return err }
func (r *Regtest) Disconnect(other *Regtest) error {
	return r.DisconnectContext(context.Background(), other)
}

// DisconnectContext is the context-aware variant of Disconnect.
func (r *Regtest) DisconnectContext(ctx context.Context, other *Regtest) error {
	addr, err := peerAddress(other)
	if err != nil {
		return err
	}
	if _, err := r.rawRPC(ctx, "disconnectnode", addr); err != nil {
		return fmt.Errorf("disconnect %s: %w", addr, err)
	}
	return nil
}

// AddNode is the lower-level escape hatch for connecting to a host bitcoind
// reachable at an arbitrary "host:p2p_port" address. Prefer Connect when both
// nodes are *Regtest instances managed by this library.
//
// Parameters:
//   - host: peer address ("host:port"). Must be non-empty.
//
// Returns:
//   - error: validation error for empty host; errNotConnected before Start;
//     otherwise wrapped RPC error.
//
// Example:
//
//	if err := rt.AddNode("127.0.0.1:18444"); err != nil { return err }
func (r *Regtest) AddNode(host string) error {
	return r.AddNodeContext(context.Background(), host)
}

// AddNodeContext is the context-aware variant of AddNode.
func (r *Regtest) AddNodeContext(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("host must not be empty")
	}
	client, err := r.lockedClient()
	if err != nil {
		return err
	}
	_, err = runWithContext(ctx, func() (struct{}, error) {
		return struct{}{}, client.AddNode(host, rpcclient.ANAdd)
	})
	if err != nil {
		return fmt.Errorf("addnode %s: %w", host, err)
	}
	return nil
}

// GetConnectionCount returns the number of peers currently connected to this
// node. Use it to confirm that a Connect call has produced a live link
// (bitcoind's addnode is asynchronous, so the count may briefly read 0).
//
// Returns:
//   - int64: number of active peer connections
//   - error: errNotConnected before Start; otherwise wrapped RPC error.
//
// Example:
//
//	if n, err := rt.GetConnectionCount(); err != nil { return err
//	} else if n == 0 { /* still connecting */ }
func (r *Regtest) GetConnectionCount() (int64, error) {
	return r.GetConnectionCountContext(context.Background())
}

// GetConnectionCountContext is the context-aware variant of GetConnectionCount.
func (r *Regtest) GetConnectionCountContext(ctx context.Context) (int64, error) {
	client, err := r.lockedClient()
	if err != nil {
		return 0, err
	}
	n, err := runWithContext(ctx, client.GetConnectionCount)
	if err != nil {
		return 0, fmt.Errorf("getconnectioncount: %w", err)
	}
	return n, nil
}
