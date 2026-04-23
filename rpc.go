package regtest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/rpcclient"
)

// Client returns the RPC client for the Regtest instance.
// For advanced users that want to use the RPC client directly.
//
// Returns:
//   - *rpcclient.Client: The RPC client instance, or nil if not connected
func (r *Regtest) Client() *rpcclient.Client {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

// GetBlockCount returns the current block count.
func (r *Regtest) GetBlockCount() (int64, error) {
	return r.GetBlockCountContext(context.Background())
}

// GetBlockCountContext is the context-aware variant of GetBlockCount.
func (r *Regtest) GetBlockCountContext(ctx context.Context) (int64, error) {
	client, err := r.lockedClient()
	if err != nil {
		return 0, err
	}
	return runWithContext(ctx, func() (int64, error) {
		return client.GetBlockCount()
	})
}

// HealthCheck performs a health check by getting the block count.
func (r *Regtest) HealthCheck() error {
	return r.HealthCheckContext(context.Background())
}

// HealthCheckContext is the context-aware variant of HealthCheck.
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

// runWithContext runs fn in a goroutine and returns its result, or ctx.Err()
// if the context is cancelled first. The fn continues running in the background
// after ctx cancellation; its result is discarded. This is the best the package
// can offer for cancellation given that btcd's rpcclient calls are blocking and
// don't accept a context.
func runWithContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
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
