package regtest

import "testing"

func TestReorg_InvalidateReconsider(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)
	if err := rt.Warp(5, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	beforeHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}

	if err := rt.InvalidateBlock(tip); err != nil {
		t.Fatalf("InvalidateBlock: %v", err)
	}
	afterHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount post-invalidate: %v", err)
	}
	if afterHeight != beforeHeight-1 {
		t.Errorf("height after invalidate = %d, want %d", afterHeight, beforeHeight-1)
	}

	if err := rt.ReconsiderBlock(tip); err != nil {
		t.Fatalf("ReconsiderBlock: %v", err)
	}
	restoredHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount post-reconsider: %v", err)
	}
	if restoredHeight != beforeHeight {
		t.Errorf("height after reconsider = %d, want %d", restoredHeight, beforeHeight)
	}
	restoredTip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash post-reconsider: %v", err)
	}
	if !restoredTip.IsEqual(tip) {
		t.Errorf("tip after reconsider = %s, want %s", restoredTip, tip)
	}
}

func TestReorg_PreciousBlock_Linear(t *testing.T) {
	t.Parallel()
	rt, addr := fundedNode(t)
	if err := rt.Warp(3, addr); err != nil {
		t.Fatalf("Warp: %v", err)
	}

	tip, err := rt.GetBestBlockHash()
	if err != nil {
		t.Fatalf("GetBestBlockHash: %v", err)
	}
	if err := rt.PreciousBlock(tip); err != nil {
		t.Fatalf("PreciousBlock(tip) on linear chain: %v", err)
	}
}

func TestReorgValidation(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	if err := rt.InvalidateBlock(nil); err == nil {
		t.Error("InvalidateBlock(nil) should error")
	}
	if err := rt.ReconsiderBlock(nil); err == nil {
		t.Error("ReconsiderBlock(nil) should error")
	}
	if err := rt.PreciousBlock(nil); err == nil {
		t.Error("PreciousBlock(nil) should error")
	}
}
