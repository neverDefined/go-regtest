package regtest

import (
	"context"
	"fmt"
)

// AddressType is the output type of a new wallet address (getnewaddress's
// address_type argument).
type AddressType string

// Address types accepted by Wallet.NewAddress.
const (
	AddressLegacy     AddressType = "legacy"      // P2PKH
	AddressP2SHSegwit AddressType = "p2sh-segwit" // P2SH-wrapped P2WPKH
	AddressBech32     AddressType = "bech32"      // P2WPKH
	AddressBech32m    AddressType = "bech32m"     // P2TR
)

// GenerateBech32 returns a new P2WPKH ("bcrt1q...") address with the given
// label from the default wallet (see Wallet).
func (r *Regtest) GenerateBech32(labelStr string) (string, error) {
	return r.GenerateBech32Context(context.Background(), labelStr)
}

// GenerateBech32Context is like GenerateBech32 but takes a context.
func (r *Regtest) GenerateBech32Context(ctx context.Context, labelStr string) (string, error) {
	return newAddress(ctx, r, labelStr, AddressBech32)
}

// GenerateBech32m returns a new P2TR ("bcrt1p...") address with the given
// label from the default wallet (see Wallet).
func (r *Regtest) GenerateBech32m(labelStr string) (string, error) {
	return r.GenerateBech32mContext(context.Background(), labelStr)
}

// GenerateBech32mContext is like GenerateBech32m but takes a context.
func (r *Regtest) GenerateBech32mContext(ctx context.Context, labelStr string) (string, error) {
	return newAddress(ctx, r, labelStr, AddressBech32m)
}

// newAddress calls getnewaddress on the node's default wallet or on a Wallet.
func newAddress(ctx context.Context, c caller, label string, addrType AddressType) (string, error) {
	address, err := call[string](ctx, c, "getnewaddress", label, string(addrType))
	if err != nil {
		return "", fmt.Errorf("failed to get new address (%s): %w", addrType, err)
	}
	return address, nil
}
