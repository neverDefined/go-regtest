package regtest

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// BlockChainInfo is the part of bitcoind's getblockchaininfo result that is
// stable across Bitcoin Core versions. Soft-fork state is left out because its
// shape has changed between releases; use GetDeploymentInfo for that.
type BlockChainInfo struct {
	Chain                string  `json:"chain"`
	Blocks               int64   `json:"blocks"`
	Headers              int64   `json:"headers"`
	BestBlockHash        string  `json:"bestblockhash"`
	Difficulty           float64 `json:"difficulty"`
	MedianTime           int64   `json:"mediantime"`
	InitialBlockDownload bool    `json:"initialblockdownload"`
	Chainwork            string  `json:"chainwork"`
	Pruned               bool    `json:"pruned"`
}

// GetBlockChainInfo returns the node's getblockchaininfo result.
func (r *Regtest) GetBlockChainInfo() (*BlockChainInfo, error) {
	return r.GetBlockChainInfoContext(context.Background())
}

// GetBlockChainInfoContext is like GetBlockChainInfo but takes a context.
func (r *Regtest) GetBlockChainInfoContext(ctx context.Context) (*BlockChainInfo, error) {
	info, err := call[BlockChainInfo](ctx, r, "getblockchaininfo")
	if err != nil {
		return nil, fmt.Errorf("getblockchaininfo: %w", err)
	}
	return &info, nil
}

// GetBestBlockHash returns the hash of the chain tip.
func (r *Regtest) GetBestBlockHash() (*chainhash.Hash, error) {
	return r.GetBestBlockHashContext(context.Background())
}

// GetBestBlockHashContext is like GetBestBlockHash but takes a context.
func (r *Regtest) GetBestBlockHashContext(ctx context.Context) (*chainhash.Hash, error) {
	hash, err := callHash(ctx, r, "getbestblockhash")
	if err != nil {
		return nil, fmt.Errorf("getbestblockhash: %w", err)
	}
	return hash, nil
}

// GetBlockHash returns the hash of the active chain's block at height.
func (r *Regtest) GetBlockHash(height int64) (*chainhash.Hash, error) {
	return r.GetBlockHashContext(context.Background(), height)
}

// GetBlockHashContext is like GetBlockHash but takes a context.
func (r *Regtest) GetBlockHashContext(ctx context.Context, height int64) (*chainhash.Hash, error) {
	hash, err := callHash(ctx, r, "getblockhash", height)
	if err != nil {
		return nil, fmt.Errorf("getblockhash %d: %w", height, err)
	}
	return hash, nil
}

// GetBlock returns the block with the given hash.
func (r *Regtest) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	return r.GetBlockContext(context.Background(), hash)
}

// GetBlockContext is like GetBlock but takes a context.
func (r *Regtest) GetBlockContext(ctx context.Context, hash *chainhash.Hash) (*wire.MsgBlock, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	h, err := call[string](ctx, r, "getblock", hash.String(), 0)
	if err != nil {
		return nil, fmt.Errorf("getblock %s: %w", hash, err)
	}
	block, err := decodeBlockHex(h)
	if err != nil {
		return nil, fmt.Errorf("getblock %s: %w", hash, err)
	}
	return block, nil
}

// GetBlockVerbose returns getblock's verbose result for the block with the
// given hash. It lists txids, not full transactions.
func (r *Regtest) GetBlockVerbose(hash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	return r.GetBlockVerboseContext(context.Background(), hash)
}

// GetBlockVerboseContext is like GetBlockVerbose but takes a context.
func (r *Regtest) GetBlockVerboseContext(ctx context.Context, hash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	res, err := call[btcjson.GetBlockVerboseResult](ctx, r, "getblock", hash.String(), 1)
	if err != nil {
		return nil, fmt.Errorf("getblock verbose %s: %w", hash, err)
	}
	return &res, nil
}

// GetBlockHeader returns the header of the block with the given hash.
func (r *Regtest) GetBlockHeader(hash *chainhash.Hash) (*wire.BlockHeader, error) {
	return r.GetBlockHeaderContext(context.Background(), hash)
}

// GetBlockHeaderContext is like GetBlockHeader but takes a context.
func (r *Regtest) GetBlockHeaderContext(ctx context.Context, hash *chainhash.Hash) (*wire.BlockHeader, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	h, err := call[string](ctx, r, "getblockheader", hash.String(), false)
	if err != nil {
		return nil, fmt.Errorf("getblockheader %s: %w", hash, err)
	}
	hdr, err := decodeHeaderHex(h)
	if err != nil {
		return nil, fmt.Errorf("getblockheader %s: %w", hash, err)
	}
	return hdr, nil
}

// GetChainTips returns every known chain tip: the active chain's and those of
// any forks. A chain without forks has one tip, with Status "active".
func (r *Regtest) GetChainTips() ([]*btcjson.GetChainTipsResult, error) {
	return r.GetChainTipsContext(context.Background())
}

// GetChainTipsContext is like GetChainTips but takes a context.
func (r *Regtest) GetChainTipsContext(ctx context.Context) ([]*btcjson.GetChainTipsResult, error) {
	tips, err := call[[]*btcjson.GetChainTipsResult](ctx, r, "getchaintips")
	if err != nil {
		return nil, fmt.Errorf("getchaintips: %w", err)
	}
	return tips, nil
}
