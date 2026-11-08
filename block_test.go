package regtest

import (
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// assembleTrivialRegtestBlock builds a minimum valid regtest block on top of
// tmpl: a single coinbase tx paying to OP_TRUE, with the witness commitment
// the template provided, then brute-force solves the (trivial) regtest PoW.
// On regtest the difficulty target is essentially MAX_HASH so the loop
// almost always solves at nonce=0.
func assembleTrivialRegtestBlock(t *testing.T, tmpl *btcjson.GetBlockTemplateResult) *wire.MsgBlock {
	t.Helper()

	prev, err := chainhash.NewHashFromStr(tmpl.PreviousHash)
	if err != nil {
		t.Fatalf("parse previous hash: %v", err)
	}
	bitsU64, err := strconv.ParseUint(tmpl.Bits, 16, 32)
	if err != nil {
		t.Fatalf("parse bits %q: %v", tmpl.Bits, err)
	}
	bits := uint32(bitsU64)
	if tmpl.CoinbaseValue == nil {
		t.Fatalf("template missing CoinbaseValue")
	}

	// Coinbase scriptSig: BIP34 height + extranonce.
	cbScript, err := txscript.NewScriptBuilder().
		AddInt64(tmpl.Height).
		AddInt64(0).
		Script()
	if err != nil {
		t.Fatalf("build coinbase script: %v", err)
	}
	coinbase := wire.NewMsgTx(2)
	coinbase.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{}, Index: 0xffffffff},
		SignatureScript:  cbScript,
		Sequence:         0xffffffff,
		Witness:          wire.TxWitness{make([]byte, 32)},
	})
	coinbase.AddTxOut(wire.NewTxOut(*tmpl.CoinbaseValue, []byte{txscript.OP_TRUE}))
	if tmpl.DefaultWitnessCommitment != "" {
		commitScript, err := hex.DecodeString(tmpl.DefaultWitnessCommitment)
		if err != nil {
			t.Fatalf("decode witness commitment: %v", err)
		}
		coinbase.AddTxOut(wire.NewTxOut(0, commitScript))
	}

	// With one tx in the block, merkle root = coinbase txid.
	merkleRoot := coinbase.TxHash()

	block := wire.NewMsgBlock(&wire.BlockHeader{
		Version:    tmpl.Version,
		PrevBlock:  *prev,
		MerkleRoot: merkleRoot,
		Timestamp:  time.Unix(tmpl.MinTime+1, 0),
		Bits:       bits,
	})
	block.AddTransaction(coinbase)

	target := blockchain.CompactToBig(bits)
	for nonce := uint32(0); nonce < (1 << 30); nonce++ {
		block.Header.Nonce = nonce
		h := block.Header.BlockHash()
		if blockchain.HashToBig(&h).Cmp(target) <= 0 {
			return block
		}
	}
	t.Fatal("could not solve regtest PoW within nonce range")
	return nil
}

// TestGetBlockTemplate_SubmitBlock pins the consensus-test path: assemble
// a block from the template, submit it, observe the height advance. This is
// the "include a tx in a block without going through the mempool" pattern
// soft-fork tests rely on.
func TestGetBlockTemplate_SubmitBlock(t *testing.T) {
	t.Parallel()
	rt, _ := fundedNode(t)

	// assembleTrivialRegtestBlock constructs a coinbase that BIP54 (Consensus
	// Cleanup, active on Inquisition) rejects with bad-cb-locktime. Skip until
	// the helper learns BIP54-correct coinbase assembly. PR2's SupportsBIP
	// will replace this Variant check with a BIP-aware skip.
	if v, _ := rt.Variant(); v == VariantInquisition {
		t.Skip("hand-rolled coinbase fails BIP54 cleanup rules on Inquisition")
	}

	tmpl, err := rt.GetBlockTemplate(&btcjson.TemplateRequest{
		Mode:  "template",
		Rules: []string{"segwit"},
	})
	if err != nil {
		t.Fatalf("GetBlockTemplate: %v", err)
	}
	if tmpl.Height <= 0 || tmpl.PreviousHash == "" || tmpl.Bits == "" {
		t.Fatalf("template missing required fields: %+v", tmpl)
	}

	startHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount: %v", err)
	}
	if tmpl.Height != startHeight+1 {
		t.Fatalf("template height %d != current+1 (%d)", tmpl.Height, startHeight+1)
	}

	block := assembleTrivialRegtestBlock(t, tmpl)
	if err := rt.SubmitBlock(block); err != nil {
		t.Fatalf("SubmitBlock: %v", err)
	}

	endHeight, err := rt.GetBlockCount()
	if err != nil {
		t.Fatalf("GetBlockCount after submit: %v", err)
	}
	if endHeight != startHeight+1 {
		t.Errorf("expected height %d -> %d, got %d", startHeight, startHeight+1, endHeight)
	}
}

func TestBlockValidation(t *testing.T) {
	t.Parallel()
	wantArgErr(t, "SubmitBlock(nil)", unstarted(t).SubmitBlock(nil))

	// An empty block passes the argument check; bitcoind must reject it.
	rt := StartT(t, nil)
	bogus := &wire.MsgBlock{Header: wire.BlockHeader{}}
	if err := rt.SubmitBlock(bogus); err == nil {
		t.Fatal("expected SubmitBlock(empty) to error, got nil")
	}
}
