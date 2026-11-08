package regtest

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// peerAddress returns the host:port other accepts P2P connections on: the
// host from its Config.Host and its P2P port (RPC port + 1).
func peerAddress(other *Regtest) (string, error) {
	if other == nil {
		return "", fmt.Errorf("peer must not be nil")
	}
	host, _, err := net.SplitHostPort(other.config.Host)
	if err != nil {
		return "", fmt.Errorf("peer host %q: %w", other.config.Host, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(other.p2pPort)), nil
}

// Connect tells this node to add the other regtest instance as a persistent
// peer, at the other node's P2P port (its RPC port + 1).
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
//     ErrNotConnected before Start; otherwise wrapped RPC error.
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
	// First register as a persistent peer (idempotent — "Node already added"
	// is a benign error when re-Connecting after a Disconnect race), then
	// fire a one-time-try to force an immediate handshake without waiting
	// for bitcoind's internal addnode-poller. The onetry call is the one
	// whose error we surface — it tells us whether the peer is reachable
	// right now.
	_, _ = r.rawRPC(ctx, "addnode", addr, "add")
	if _, err := r.rawRPC(ctx, "addnode", addr, "onetry"); err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	return nil
}

// Disconnect is the inverse of Connect: it removes the peer from the addnode
// list AND drops any live connection. Useful for inducing a network
// partition in reorg/propagation tests where a subsequent Connect should
// behave as a fresh setup rather than racing bitcoind's auto-reconnect timer.
//
// Parameters:
//   - other: another running *Regtest instance (must not be nil)
//
// Returns:
//   - error: validation error for nil peer or unparseable host;
//     ErrNotConnected before Start; otherwise wrapped RPC error from
//     disconnectnode (the addnode-remove step is best-effort and ignored).
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
	// Remove from addnode list first so bitcoind's auto-reconnect timer
	// can't race the disconnectnode call that follows. Errors here are
	// expected when the peer was never explicitly added (e.g. inbound-only
	// link, or a previous Disconnect already removed it) — ignore them.
	_, _ = r.rawRPC(ctx, "addnode", addr, "remove")
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
//   - error: validation error for empty host; ErrNotConnected before Start;
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
	if _, err := r.rawRPC(ctx, "addnode", host, "add"); err != nil {
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
//   - error: ErrNotConnected before Start; otherwise wrapped RPC error.
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
	n, err := call[int64](ctx, r, "getconnectioncount")
	if err != nil {
		return 0, fmt.Errorf("getconnectioncount: %w", err)
	}
	return n, nil
}

// syncTimeout bounds SyncBlocks.
const syncTimeout = 30 * time.Second

// SyncBlocks waits until all nodes have the same best block, for up to 30
// seconds. Use it after Connect or after mining on one node, instead of
// sleeping. It compares a snapshot of the nodes' tips, so agreement seen
// while a reorg is still under way can be transient.
func SyncBlocks(nodes ...*Regtest) error {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	return SyncBlocksContext(ctx, nodes...)
}

// SyncBlocksContext is the context-aware variant of SyncBlocks. It polls
// until the nodes agree or ctx is done.
func SyncBlocksContext(ctx context.Context, nodes ...*Regtest) error {
	for i, n := range nodes {
		if n == nil {
			return fmt.Errorf("nodes[%d] must not be nil", i)
		}
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		tips := make([]string, len(nodes))
		synced := true
		for i, n := range nodes {
			h, err := n.GetBestBlockHashContext(ctx)
			if err != nil {
				return fmt.Errorf("SyncBlocks: node %d: %w", i, err)
			}
			tips[i] = h.String()
			synced = synced && tips[i] == tips[0]
		}
		if synced {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("SyncBlocks: tips still differ %v: %w", tips, ctx.Err())
		case <-ticker.C:
		}
	}
}
