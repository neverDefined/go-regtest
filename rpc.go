package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

// Client returns the btcd RPC client the wrappers use, for RPCs this package
// doesn't wrap. It is nil before Start and after Stop. The client is shared,
// so don't call its Shutdown method.
func (r *Regtest) Client() *rpcclient.Client {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

// GetBlockCount returns the height of the chain tip, which is 0 on a fresh
// node.
func (r *Regtest) GetBlockCount() (int64, error) {
	return r.GetBlockCountContext(context.Background())
}

// GetBlockCountContext is like GetBlockCount but takes a context.
func (r *Regtest) GetBlockCountContext(ctx context.Context) (int64, error) {
	return call[int64](ctx, r, "getblockcount")
}

// HealthCheck returns an error if the node doesn't answer a getblockcount
// call.
func (r *Regtest) HealthCheck() error {
	return r.HealthCheckContext(context.Background())
}

// HealthCheckContext is like HealthCheck but takes a context.
func (r *Regtest) HealthCheckContext(ctx context.Context) error {
	if _, err := r.GetBlockCountContext(ctx); err != nil {
		return fmt.Errorf("failed to get block count (health check): %w", err)
	}
	return nil
}

// lockedClient returns the current RPC client under the read lock, or
// ErrNotConnected before Start or after Stop. The client stays safe to use
// after the lock is released because *rpcclient.Client is internally
// synchronized; only the pointer slot needs the lock.
func (r *Regtest) lockedClient() (*rpcclient.Client, error) {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	if r.client == nil {
		return nil, ErrNotConnected
	}
	return r.client, nil
}

// rawRPC sends method through the node's RPC client and returns the raw
// result. Each arg is JSON-marshaled; json.RawMessage values pass through. It
// returns ctx.Err() once ctx is done, even though btcd's RawRequest blocks.
func (r *Regtest) rawRPC(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	return rawRequest(ctx, client, method, args...)
}

// walletClient returns the cached client whose requests go to
// /wallet/<name>, creating it on first use. bitcoind needs that path for
// wallet RPCs once more than one wallet is loaded. Stop shuts these down
// with the main client.
func (r *Regtest) walletClient(name string) (*rpcclient.Client, error) {
	r.clientMu.RLock()
	c, connected := r.walletClients[name], r.client != nil
	r.clientMu.RUnlock()
	if !connected {
		return nil, ErrNotConnected
	}
	if c != nil {
		return c, nil
	}

	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if r.client == nil {
		return nil, ErrNotConnected
	}
	if c := r.walletClients[name]; c != nil {
		return c, nil
	}
	cfg := r.RPCConfig()
	cfg.Host += "/wallet/" + url.PathEscape(name)
	c, err := rpcclient.New(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("create RPC client for wallet %q: %w", name, err)
	}
	if r.walletClients == nil {
		r.walletClients = make(map[string]*rpcclient.Client)
	}
	r.walletClients[name] = c
	return c, nil
}

// rawRequest marshals args and sends method through client, honoring ctx.
func rawRequest(ctx context.Context, client *rpcclient.Client, method string, args ...any) (json.RawMessage, error) {
	params := make([]json.RawMessage, len(args))
	for i, a := range args {
		if rm, ok := a.(json.RawMessage); ok {
			params[i] = rm
			continue
		}
		b, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("rawRPC %q: failed to marshal param %d: %w", method, i, err)
		}
		params[i] = b
	}

	return runWithContext(ctx, func() (json.RawMessage, error) {
		resp, err := client.RawRequest(method, params)
		if err != nil {
			return nil, fmt.Errorf("rawRPC %q failed: %w", method, err)
		}
		return resp, nil
	})
}

// caller sends a raw RPC: *Regtest to the node (and its default wallet),
// *Wallet to one wallet's endpoint.
type caller interface {
	rawRPC(ctx context.Context, method string, args ...any) (json.RawMessage, error)
}

// call issues method via rawRPC and JSON-decodes the result into T. A JSON
// null result leaves T at its zero value, so use a pointer T for RPCs that
// return null to mean "not found".
func call[T any](ctx context.Context, r caller, method string, args ...any) (T, error) {
	var out T
	raw, err := r.rawRPC(ctx, method, args...)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("unmarshal %s: %w", method, err)
	}
	return out, nil
}

// callHash is call for RPCs whose result is a hex hash string (a txid or a
// block hash).
func callHash(ctx context.Context, r caller, method string, args ...any) (*chainhash.Hash, error) {
	s, err := call[string](ctx, r, method, args...)
	if err != nil {
		return nil, err
	}
	h, err := chainhash.NewHashFromStr(s)
	if err != nil {
		return nil, fmt.Errorf("parse %s result: %w", method, err)
	}
	return h, nil
}

// asRPCError returns the *btcjson.RPCError (bitcoind's error code and message)
// in err's chain, or nil if there is none.
func asRPCError(err error) *btcjson.RPCError {
	var rpcErr *btcjson.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	return nil
}

// runWithContext runs fn in a goroutine and returns its result, or ctx.Err()
// if ctx is done first. An already-done ctx returns ctx.Err() without calling
// fn. After cancellation fn keeps running in the background and its result is
// discarded: btcd's rpcclient calls block and don't take a context, so this is
// the best the package can do.
func runWithContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case r := <-ch:
		return r.val, r.err
	}
}
