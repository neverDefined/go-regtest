package regtest

import (
	"context"
	"encoding/json"
	"fmt"
)

// GenerateBech32 generates a new Bech32 (native SegWit) address for the given label.
// Bech32 addresses start with "bc1" on mainnet or "bcrt1" on regtest and provide
// better error detection and lower transaction fees compared to legacy addresses.
//
// Parameters:
//   - labelStr: Human-readable label for the address (used for organization)
//
// Returns:
//   - string: A new Bech32 address (e.g., "bcrt1q...")
//   - error: RPC error if address generation fails or no wallet is loaded
//
// The generated address:
//   - Is derived from the wallet's HD seed
//   - Can be used for receiving Bitcoin payments
//   - Supports SegWit transactions with lower fees
//   - Has better error detection than legacy addresses
//
// Example:
//
//	address, err := rt.GenerateBech32("my_receiving_address")
//	if err != nil {
//	    return fmt.Errorf("failed to generate address: %w", err)
//	}
//	fmt.Printf("Generated Bech32 address: %s\n", address)
func (r *Regtest) GenerateBech32(labelStr string) (string, error) {
	return r.GenerateBech32Context(context.Background(), labelStr)
}

// GenerateBech32Context is the context-aware variant of GenerateBech32.
func (r *Regtest) GenerateBech32Context(ctx context.Context, labelStr string) (string, error) {
	return r.generateAddress(ctx, labelStr, "bech32")
}

// GenerateBech32m generates a new Bech32m (Taproot) address for the given label.
// Bech32m addresses are used for Taproot outputs and provide enhanced privacy
// and efficiency through the Taproot upgrade. They start with "bc1p" on mainnet
// or "bcrt1p" on regtest.
//
// Parameters:
//   - labelStr: Human-readable label for the address (used for organization)
//
// Returns:
//   - string: A new Bech32m Taproot address (e.g., "bcrt1p...")
//   - error: RPC error if address generation fails or no wallet is loaded
//
// The generated address:
//   - Is a Taproot address supporting advanced scripting
//   - Provides better privacy through key aggregation
//   - Enables complex smart contract functionality
//   - Has the same error detection as Bech32 but with different checksum
//
// Example:
//
//	address, err := rt.GenerateBech32m("my_taproot_address")
//	if err != nil {
//	    return fmt.Errorf("failed to generate Taproot address: %w", err)
//	}
//	fmt.Printf("Generated Bech32m address: %s\n", address)
func (r *Regtest) GenerateBech32m(labelStr string) (string, error) {
	return r.GenerateBech32mContext(context.Background(), labelStr)
}

// GenerateBech32mContext is the context-aware variant of GenerateBech32m.
func (r *Regtest) GenerateBech32mContext(ctx context.Context, labelStr string) (string, error) {
	return r.generateAddress(ctx, labelStr, "bech32m")
}

// generateAddress is the shared implementation behind GenerateBech32 and
// GenerateBech32m. addrType is forwarded as the second argument to bitcoind's
// getnewaddress RPC.
func (r *Regtest) generateAddress(ctx context.Context, label, addrType string) (string, error) {
	resp, err := r.rawRPC(ctx, "getnewaddress", label, addrType)
	if err != nil {
		return "", fmt.Errorf("failed to get new address (%s): %w", addrType, err)
	}
	var address string
	if err := json.Unmarshal(resp, &address); err != nil {
		return "", fmt.Errorf("failed to unmarshal address response: %w", err)
	}
	return address, nil
}
