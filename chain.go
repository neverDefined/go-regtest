package regtest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// BlockChainInfo is a curated subset of bitcoind's getblockchaininfo response.
// Only fields stable across Bitcoin Core versions are included; the legacy
// softforks/deployments shape (which has shifted between Core releases) is
// intentionally omitted — use GetDeploymentInfo for soft-fork state.
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

// GetBlockChainInfo returns curated chain-state information from bitcoind.
//
// This wrapper uses the raw getblockchaininfo RPC rather than btcd's typed
// GetBlockChainInfo, which issues a second getnetworkinfo call internally and
// applies version-specific unmarshaling that has historically broken across
// Bitcoin Core releases (compare BroadcastTransaction in tx.go for the same
// pattern).
//
// Returns:
//   - *BlockChainInfo: chain, height, best block hash, etc.
//   - error: errNotConnected if Start has not been called; otherwise the
//     wrapped RPC or unmarshal error.
//
// Example:
//
//	info, err := rt.GetBlockChainInfo()
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("chain=%s height=%d\n", info.Chain, info.Blocks)
func (r *Regtest) GetBlockChainInfo() (*BlockChainInfo, error) {
	return r.GetBlockChainInfoContext(context.Background())
}

// GetBlockChainInfoContext is the context-aware variant of GetBlockChainInfo.
func (r *Regtest) GetBlockChainInfoContext(ctx context.Context) (*BlockChainInfo, error) {
	raw, err := r.rawRPC(ctx, "getblockchaininfo")
	if err != nil {
		return nil, fmt.Errorf("getblockchaininfo: %w", err)
	}
	var info BlockChainInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("unmarshal getblockchaininfo: %w", err)
	}
	return &info, nil
}

// GetBestBlockHash returns the hash of the best (tip) block in the longest chain.
//
// Returns:
//   - *chainhash.Hash: hash of the chain tip
//   - error: errNotConnected if Start has not been called; otherwise wrapped RPC error.
//
// Example:
//
//	hash, err := rt.GetBestBlockHash()
//	if err != nil {
//	    return err
//	}
//	fmt.Println("tip:", hash)
func (r *Regtest) GetBestBlockHash() (*chainhash.Hash, error) {
	return r.GetBestBlockHashContext(context.Background())
}

// GetBestBlockHashContext is the context-aware variant of GetBestBlockHash.
func (r *Regtest) GetBestBlockHashContext(ctx context.Context) (*chainhash.Hash, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	hash, err := runWithContext(ctx, client.GetBestBlockHash)
	if err != nil {
		return nil, fmt.Errorf("getbestblockhash: %w", err)
	}
	return hash, nil
}

// GetBlockHash returns the hash of the block at the given height.
//
// Parameters:
//   - height: block height (>= 0)
//
// Returns:
//   - *chainhash.Hash: hash of the block at height
//   - error: errNotConnected if Start has not been called; otherwise wrapped RPC error
//     (e.g. block height out of range).
//
// Example:
//
//	hash, err := rt.GetBlockHash(101)
//	if err != nil {
//	    return err
//	}
//	fmt.Println("block 101:", hash)
func (r *Regtest) GetBlockHash(height int64) (*chainhash.Hash, error) {
	return r.GetBlockHashContext(context.Background(), height)
}

// GetBlockHashContext is the context-aware variant of GetBlockHash.
func (r *Regtest) GetBlockHashContext(ctx context.Context, height int64) (*chainhash.Hash, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	hash, err := runWithContext(ctx, func() (*chainhash.Hash, error) {
		return client.GetBlockHash(height)
	})
	if err != nil {
		return nil, fmt.Errorf("getblockhash %d: %w", height, err)
	}
	return hash, nil
}

// GetBlock returns the deserialized block (raw form) for the given hash.
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - *wire.MsgBlock: the deserialized block
//   - error: validation error for nil hash; errNotConnected if Start has not
//     been called; otherwise wrapped RPC error (e.g. block not found).
//
// Example:
//
//	block, err := rt.GetBlock(hash)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("block has %d txs\n", len(block.Transactions))
func (r *Regtest) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	return r.GetBlockContext(context.Background(), hash)
}

// GetBlockContext is the context-aware variant of GetBlock.
func (r *Regtest) GetBlockContext(ctx context.Context, hash *chainhash.Hash) (*wire.MsgBlock, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	block, err := runWithContext(ctx, func() (*wire.MsgBlock, error) {
		return client.GetBlock(hash)
	})
	if err != nil {
		return nil, fmt.Errorf("getblock %s: %w", hash, err)
	}
	return block, nil
}

// GetBlockVerbose returns the verbose JSON form of the block (with tx ids,
// confirmations, height, etc.) for the given hash.
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - *btcjson.GetBlockVerboseResult: the verbose result
//   - error: validation error for nil hash; errNotConnected if Start has not
//     been called; otherwise wrapped RPC error.
//
// Example:
//
//	v, err := rt.GetBlockVerbose(hash)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("height=%d confirmations=%d\n", v.Height, v.Confirmations)
func (r *Regtest) GetBlockVerbose(hash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	return r.GetBlockVerboseContext(context.Background(), hash)
}

// GetBlockVerboseContext is the context-aware variant of GetBlockVerbose.
func (r *Regtest) GetBlockVerboseContext(ctx context.Context, hash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	res, err := runWithContext(ctx, func() (*btcjson.GetBlockVerboseResult, error) {
		return client.GetBlockVerbose(hash)
	})
	if err != nil {
		return nil, fmt.Errorf("getblock verbose %s: %w", hash, err)
	}
	return res, nil
}

// GetBlockHeader returns the deserialized header for the given block hash.
//
// Parameters:
//   - hash: block hash (must be non-nil)
//
// Returns:
//   - *wire.BlockHeader: the deserialized header
//   - error: validation error for nil hash; errNotConnected if Start has not
//     been called; otherwise wrapped RPC error.
//
// Example:
//
//	hdr, err := rt.GetBlockHeader(hash)
//	if err != nil {
//	    return err
//	}
//	fmt.Println("merkle root:", hdr.MerkleRoot)
func (r *Regtest) GetBlockHeader(hash *chainhash.Hash) (*wire.BlockHeader, error) {
	return r.GetBlockHeaderContext(context.Background(), hash)
}

// GetBlockHeaderContext is the context-aware variant of GetBlockHeader.
func (r *Regtest) GetBlockHeaderContext(ctx context.Context, hash *chainhash.Hash) (*wire.BlockHeader, error) {
	if hash == nil {
		return nil, fmt.Errorf("hash must not be nil")
	}
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	hdr, err := runWithContext(ctx, func() (*wire.BlockHeader, error) {
		return client.GetBlockHeader(hash)
	})
	if err != nil {
		return nil, fmt.Errorf("getblockheader %s: %w", hash, err)
	}
	return hdr, nil
}

// GetChainTips returns information about all known tips in the block tree
// (active main chain plus any orphan branches).
//
// Returns:
//   - []*btcjson.GetChainTipsResult: one entry per tip; on a linear chain there
//     is exactly one with Status == "active"
//   - error: errNotConnected if Start has not been called; otherwise wrapped RPC error.
//
// Example:
//
//	tips, err := rt.GetChainTips()
//	if err != nil {
//	    return err
//	}
//	for _, tip := range tips {
//	    fmt.Printf("tip %s height=%d status=%s\n", tip.Hash, tip.Height, tip.Status)
//	}
func (r *Regtest) GetChainTips() ([]*btcjson.GetChainTipsResult, error) {
	return r.GetChainTipsContext(context.Background())
}

// GetChainTipsContext is the context-aware variant of GetChainTips.
func (r *Regtest) GetChainTipsContext(ctx context.Context) ([]*btcjson.GetChainTipsResult, error) {
	client, err := r.lockedClient()
	if err != nil {
		return nil, err
	}
	tips, err := runWithContext(ctx, client.GetChainTips)
	if err != nil {
		return nil, fmt.Errorf("getchaintips: %w", err)
	}
	return tips, nil
}
