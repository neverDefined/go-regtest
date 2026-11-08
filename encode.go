package regtest

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/wire"
)

// txHex serializes tx (with witness data, if any) as the hex string bitcoind
// RPCs take.
func txHex(tx *wire.MsgTx) (string, error) {
	return serializeHex(tx, "tx")
}

// blockHex serializes block (with witness data, if any) as a hex string.
func blockHex(block *wire.MsgBlock) (string, error) {
	return serializeHex(block, "block")
}

// decodeTxHex parses a hex-encoded transaction. If the witness encoding fails
// it retries the legacy encoding: a transaction with no inputs (e.g. fresh
// createrawtransaction output) is ambiguous under the witness encoding.
func decodeTxHex(s string) (*wire.MsgTx, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode tx hex: %w", err)
	}
	var tx wire.MsgTx
	if err := tx.Deserialize(bytes.NewReader(b)); err != nil {
		tx = wire.MsgTx{}
		if legacyErr := tx.DeserializeNoWitness(bytes.NewReader(b)); legacyErr != nil {
			return nil, fmt.Errorf("deserialize tx: %w", err)
		}
	}
	return &tx, nil
}

// decodeBlockHex parses a hex-encoded block.
func decodeBlockHex(s string) (*wire.MsgBlock, error) {
	var block wire.MsgBlock
	if err := deserializeHex(s, &block, "block"); err != nil {
		return nil, err
	}
	return &block, nil
}

// decodeHeaderHex parses a hex-encoded 80-byte block header.
func decodeHeaderHex(s string) (*wire.BlockHeader, error) {
	var hdr wire.BlockHeader
	if err := deserializeHex(s, &hdr, "block header"); err != nil {
		return nil, err
	}
	return &hdr, nil
}

func serializeHex(v interface{ Serialize(io.Writer) error }, what string) (string, error) {
	var buf bytes.Buffer
	if err := v.Serialize(&buf); err != nil {
		return "", fmt.Errorf("serialize %s: %w", what, err)
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

func deserializeHex(s string, v interface{ Deserialize(io.Reader) error }, what string) error {
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("decode %s hex: %w", what, err)
	}
	if err := v.Deserialize(bytes.NewReader(b)); err != nil {
		return fmt.Errorf("deserialize %s: %w", what, err)
	}
	return nil
}
