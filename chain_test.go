package regtest

import "testing"

// TestChainState exercises the chain inspection wrappers added in
// chain.go: GetBlockChainInfo, GetBestBlockHash, GetBlockHash, GetBlock,
// GetBlockVerbose, GetBlockHeader, GetChainTips. After mining 10 blocks the
// tip hash should agree across queries and there should be exactly one
// active tip on the linear chain.
func TestChainState(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)
	if err := rt.Warp(10, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	info, err := rt.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("GetBlockChainInfo: %v", err)
	}
	if info.Chain != "regtest" {
		t.Errorf("expected Chain=regtest, got %q", info.Chain)
	}
	if info.Blocks < 10 {
		t.Errorf("expected Blocks >= 10, got %d", info.Blocks)
	}
	if info.BestBlockHash == "" {
		t.Error("BestBlockHash empty")
	}

	bestHash, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	if bestHash.String() != info.BestBlockHash {
		t.Errorf("GetBestBlockHash %s != info.BestBlockHash %s", bestHash, info.BestBlockHash)
	}

	tipHash, err := rt.GetBlockHash(info.Blocks)
	if err != nil {
		t.Fatalf("GetBlockHash(%d): %v", info.Blocks, err)
	}
	if !tipHash.IsEqual(bestHash) {
		t.Errorf("GetBlockHash(tip) %s != GetBestBlockHash %s", tipHash, bestHash)
	}

	block, err := rt.GetBlock(bestHash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if len(block.Transactions) == 0 {
		t.Error("expected at least one tx (coinbase) in block")
	}

	verbose, err := rt.GetBlockVerbose(bestHash)
	if err != nil {
		t.Fatalf("GetBlockVerbose: %v", err)
	}
	if verbose.Height != info.Blocks {
		t.Errorf("verbose Height %d != info.Blocks %d", verbose.Height, info.Blocks)
	}

	hdr, err := rt.GetBlockHeader(bestHash)
	if err != nil {
		t.Fatalf("GetBlockHeader: %v", err)
	}
	if hdr == nil {
		t.Error("GetBlockHeader returned nil header")
	}

	tips, err := rt.GetChainTips()
	if err != nil {
		t.Fatalf("GetChainTips: %v", err)
	}
	activeCount := 0
	for _, tip := range tips {
		if tip.Status == "active" {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active tip on linear chain, got %d (tips=%+v)", activeCount, tips)
	}
}

func TestChainValidation(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	if _, err := rt.GetBlock(nil); err == nil {
		t.Error("GetBlock(nil) should return validation error")
	}
	if _, err := rt.GetBlockVerbose(nil); err == nil {
		t.Error("GetBlockVerbose(nil) should return validation error")
	}
	if _, err := rt.GetBlockHeader(nil); err == nil {
		t.Error("GetBlockHeader(nil) should return validation error")
	}
}
