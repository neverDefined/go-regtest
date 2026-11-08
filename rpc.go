package regtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

// Client returns the underlying btcsuite/rpcclient connection for callers
// that need an RPC the typed wrappers in this package don't cover. The
// returned client is shared with the wrappers; callers must not Shutdown it.
//
// Returns:
//   - *rpcclient.Client: the live client, or nil before Start has succeeded
//     (or after Stop). Prefer the typed wrappers on Regtest where available.
//
// Example:
//
//	client := rt.Client()
//	if client == nil {
//	    return errors.New("Start must be called first")
//	}
//	info, _ := client.GetBlockChainInfo()
func (r *Regtest) Client() *rpcclient.Client {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

// GetBlockCount returns the chain tip height. Convenience wrapper around
// GetBlockCountContext using context.Background().
//
// Returns:
//   - int64: current block height (0 on a fresh regtest node before any
//     blocks have been mined).
//   - error: errNotConnected before Start; otherwise the wrapped RPC error.
//
// Example:
//
//	h, err := rt.GetBlockCount()
//	if err != nil { return err }
//	fmt.Printf("tip at height %d\n", h)
func (r *Regtest) GetBlockCount() (int64, error) {
	return r.GetBlockCountContext(context.Background())
}

// GetBlockCountContext is the context-aware variant of GetBlockCount.
//
// Parameters:
//   - ctx: cancellation / timeout. A pre-cancelled context returns ctx.Err().
//
// Returns:
//   - int64: current block height.
//   - error: errNotConnected before Start; ctx.Err() on cancellation;
//     otherwise the wrapped RPC error.
func (r *Regtest) GetBlockCountContext(ctx context.Context) (int64, error) {
	return call[int64](ctx, r, "getblockcount")
}

// HealthCheck performs a minimal RPC round-trip (getblockcount) to confirm
// the node is reachable and responsive. Convenience wrapper around
// HealthCheckContext using context.Background().
//
// Returns:
//   - error: errNotConnected before Start; otherwise the wrapped RPC error
//     from getblockcount.
//
// Example:
//
//	if err := rt.HealthCheck(); err != nil {
//	    t.Fatalf("node not healthy: %v", err)
//	}
func (r *Regtest) HealthCheck() error {
	return r.HealthCheckContext(context.Background())
}

// HealthCheckContext is the context-aware variant of HealthCheck.
//
// Parameters:
//   - ctx: cancellation / timeout. A pre-cancelled context returns ctx.Err().
//
// Returns:
//   - error: errNotConnected before Start; ctx.Err() on cancellation;
//     otherwise the wrapped RPC error.
func (r *Regtest) HealthCheckContext(ctx context.Context) error {
	if _, err := r.GetBlockCountContext(ctx); err != nil {
		return fmt.Errorf("failed to get block count (health check): %w", err)
	}
	return nil
}

// lockedClient returns the current RPC client under read-lock, or errNotConnected
// if Start() has not been called (or Stop() cleared the client). The returned
// client is safe to use after the lock is released because *rpcclient.Client is
// internally synchronized; only the pointer slot needs lock protection.
func (r *Regtest) lockedClient() (*rpcclient.Client, error) {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	if r.client == nil {
		return nil, errNotConnected
	}
	return r.client, nil
}

// rawRPC issues a JSON-RPC call via the underlying btcd rpcclient and returns
// the raw response. Each arg is JSON-marshaled (json.RawMessage values pass
// through). The call respects ctx cancellation by returning ctx.Err() when the
// context is done, even though btcd's RawRequest is itself blocking.
func (r *Regtest) rawRPC(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}

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

// call issues method via rawRPC and JSON-decodes the result into T. A JSON
// null result leaves T at its zero value, so use a pointer T for RPCs that
// return null to mean "not found".
func call[T any](ctx context.Context, r *Regtest, method string, args ...any) (T, error) {
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
func callHash(ctx context.Context, r *Regtest, method string, args ...any) (*chainhash.Hash, error) {
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
// if the context is cancelled first. An already-done ctx returns ctx.Err()
// without calling fn. The fn continues running in the background after ctx
// cancellation; its result is discarded. This is the best the package can
// offer for cancellation given that btcd's rpcclient calls are blocking and
// don't accept a context.
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
