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

// Connect adds other as a persistent peer of this node, at other's P2P port
// (its RPC port + 1), and asks bitcoind to connect right away.
//
// The handshake finishes after Connect returns. If the test needs the link to
// be up, poll GetConnectionCount until it is non-zero.
func (r *Regtest) Connect(other *Regtest) error {
	return r.ConnectContext(context.Background(), other)
}

// ConnectContext is like Connect but takes a context.
func (r *Regtest) ConnectContext(ctx context.Context, other *Regtest) error {
	addr, err := peerAddress(other)
	if err != nil {
		return err
	}
	// First add a persistent peer. This is idempotent: "Node already added" is
	// a benign error when re-connecting after a Disconnect race. Then make a
	// one-time attempt to force an immediate handshake instead of waiting for
	// bitcoind's addnode poller. onetry returns null whether or not the
	// connection succeeds, so a nil error doesn't mean the peer is reachable;
	// use SyncBlocks or GetConnectionCount to wait for the link.
	_, _ = r.rawRPC(ctx, "addnode", addr, "add")
	if _, err := r.rawRPC(ctx, "addnode", addr, "onetry"); err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	return nil
}

// Disconnect undoes Connect: it removes other from this node's addnode list
// and drops any live connection to it. Removing it from the list stops
// bitcoind's automatic reconnect, so a later Connect starts fresh. Use it to
// partition nodes in reorg and propagation tests.
//
// Only the disconnectnode error is returned; removing the addnode entry is
// best-effort.
func (r *Regtest) Disconnect(other *Regtest) error {
	return r.DisconnectContext(context.Background(), other)
}

// DisconnectContext is like Disconnect but takes a context.
func (r *Regtest) DisconnectContext(ctx context.Context, other *Regtest) error {
	addr, err := peerAddress(other)
	if err != nil {
		return err
	}
	// Remove from the addnode list first so bitcoind's auto-reconnect timer
	// can't race the disconnectnode call that follows. Errors here are expected
	// when the peer was never added (an inbound-only link, or a previous
	// Disconnect already removed it), so ignore them.
	_, _ = r.rawRPC(ctx, "addnode", addr, "remove")
	if _, err := r.rawRPC(ctx, "disconnectnode", addr); err != nil {
		return fmt.Errorf("disconnect %s: %w", addr, err)
	}
	return nil
}

// AddNode adds host, a "host:port" P2P address, to this node's addnode list
// as a persistent peer. Unlike Connect, it doesn't ask for an immediate
// connection attempt. Prefer Connect when the peer is another Regtest.
func (r *Regtest) AddNode(host string) error {
	return r.AddNodeContext(context.Background(), host)
}

// AddNodeContext is like AddNode but takes a context.
func (r *Regtest) AddNodeContext(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("host must not be empty")
	}
	if _, err := r.rawRPC(ctx, "addnode", host, "add"); err != nil {
		return fmt.Errorf("addnode %s: %w", host, err)
	}
	return nil
}

// GetConnectionCount returns the number of peers connected to this node.
func (r *Regtest) GetConnectionCount() (int64, error) {
	return r.GetConnectionCountContext(context.Background())
}

// GetConnectionCountContext is like GetConnectionCount but takes a context.
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

// SyncBlocksContext is like SyncBlocks but waits until ctx is done instead
// of 30 seconds.
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
