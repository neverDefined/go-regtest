package regtest

import (
	"context"
	"fmt"
	"sort"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

const (
	// faucetFeeRate is the fee rate of FundAddress transactions, in sat/vB.
	faucetFeeRate = 2
	// faucetMinChange is the smallest change output FundAddress creates.
	// Smaller change goes to the fee.
	faucetMinChange = 1_000
	// faucetInputFee is what one P2WPKH input (68 vB) adds to the fee.
	// Outputs worth no more than this cost more to spend than they hold, so
	// the faucet ignores them.
	faucetInputFee = 68 * faucetFeeRate

	// coinbaseMaturity is how many blocks a coinbase output waits: one
	// created at height h can be spent in blocks at h+100 and later.
	coinbaseMaturity = 100
	// regtestHalvingInterval is how often regtest halves the block subsidy.
	regtestHalvingInterval = 150
	// initialSubsidy is the block subsidy before the first halving, in sats.
	initialSubsidy int64 = 5_000_000_000
)

// faucet is the key FundAddress spends from. It lives in memory for the
// lifetime of its Regtest, which guards it with faucetSem.
type faucet struct {
	key      *btcec.PrivateKey
	addr     string // P2WPKH address of key on regtest
	pkScript []byte // output script of addr

	// change holds the outputs the faucet created as change. Every other
	// output paying addr is a coinbase and must mature before it's spent.
	// Start wipes the chain, so entries can go stale; that is harmless, as a
	// stale outpoint never shows up in a scan.
	change map[wire.OutPoint]struct{}
}

// faucetCoin is an output the faucet can spend.
type faucetCoin struct {
	op    wire.OutPoint
	value int64
}

// faucetBalance is what a scan of the faucet address found, leaving out
// outputs worth faucetInputFee or less.
type faucetBalance struct {
	spendable []faucetCoin // can be spent in the next block
	held      int64        // value of all outputs, spendable or not
	immature  int64        // value of coinbases that can't be spent yet
}

// newFaucet creates a faucet with a fresh random key.
func newFaucet() (*faucet, error) {
	key, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate faucet key: %w", err)
	}
	pkHash := btcutil.Hash160(key.PubKey().SerializeCompressed())
	addr, err := btcutil.NewAddressWitnessPubKeyHash(pkHash, &chaincfg.RegressionNetParams)
	if err != nil {
		return nil, fmt.Errorf("faucet address: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("faucet script: %w", err)
	}
	return &faucet{
		key:      key,
		addr:     addr.EncodeAddress(),
		pkScript: pkScript,
		change:   make(map[wire.OutPoint]struct{}),
	}, nil
}

// FundAddress returns a confirmed output paying exactly sats to address. It
// needs no wallet and loads none: the coins come from a private key that this
// Regtest holds in memory, created on first use. FundAddress mines to that
// key's address, 101 or more blocks on first use and at least one on every
// call, and confirms its transaction with GenerateBlock, so mempool policy
// such as -blockmintxfee can't keep it out of the block.
//
// sats must be > 0 and at most 21 million BTC. Calls on one Regtest run one
// at a time. Regtest halves the block subsidy every 150 blocks, so past
// height 3,900 or so mining adds almost nothing, and FundAddress fails once
// the faucet's coins run out. Before Start it returns ErrNotConnected.
func (r *Regtest) FundAddress(address string, sats int64) (wire.OutPoint, error) {
	return r.FundAddressContext(context.Background(), address, sats)
}

// FundAddressContext is like FundAddress but takes a context, which also
// bounds the wait for a FundAddress call already running on this Regtest.
func (r *Regtest) FundAddressContext(ctx context.Context, address string, sats int64) (wire.OutPoint, error) {
	if sats <= 0 || sats > btcutil.MaxSatoshi {
		return wire.OutPoint{}, fmt.Errorf("sats must be > 0 and <= %d, got %d", int64(btcutil.MaxSatoshi), sats)
	}
	dest, err := btcutil.DecodeAddress(address, &chaincfg.RegressionNetParams)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("decode address: %w", err)
	}
	destScript, err := txscript.PayToAddrScript(dest)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("address script: %w", err)
	}

	sem := r.faucetLock()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", ctx.Err())
	}
	defer func() { <-sem }()

	if r.faucet == nil {
		if r.faucet, err = newFaucet(); err != nil {
			return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
		}
	}
	f := r.faucet

	coins, change, err := r.faucetCoins(ctx, f, sats)
	if err != nil {
		return wire.OutPoint{}, err
	}
	tx, err := f.buildTx(coins, destScript, sats, change)
	if err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: %w", err)
	}
	txid := tx.TxHash()
	// Record the change before mining: if ctx ends while generateblock is
	// still running, the block may land anyway.
	if change > 0 {
		f.change[wire.OutPoint{Hash: txid, Index: 1}] = struct{}{}
	}
	if _, err := r.GenerateBlockContext(ctx, f.addr, tx); err != nil {
		return wire.OutPoint{}, fmt.Errorf("FundAddress: confirm: %w", err)
	}
	return wire.OutPoint{Hash: txid, Index: 0}, nil
}

// faucetLock returns the semaphore that serializes FundAddress.
func (r *Regtest) faucetLock() chan struct{} {
	r.faucetOnce.Do(func() { r.faucetSem = make(chan struct{}, 1) })
	return r.faucetSem
}

// faucetCoins mines to the faucet until it can pay sats, then returns the
// coins to spend and the change to send back (0 for none).
func (r *Regtest) faucetCoins(ctx context.Context, f *faucet, sats int64) ([]faucetCoin, int64, error) {
	for {
		tip, err := r.GetBlockCountContext(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("FundAddress: %w", err)
		}
		utxos, err := r.ScanTxOutSetForAddressContext(ctx, f.addr)
		if err != nil {
			return nil, 0, fmt.Errorf("FundAddress: %w", err)
		}
		bal, err := f.balance(utxos, tip)
		if err != nil {
			return nil, 0, fmt.Errorf("FundAddress: %w", err)
		}
		if coins, change, ok := selectCoins(bal.spendable, sats); ok {
			return coins, change, nil
		}
		if err := bal.canGrow(sats, tip+1); err != nil {
			return nil, 0, fmt.Errorf("FundAddress: %w", err)
		}
		// 101 blocks: the first of them matures by the next block.
		if err := r.WarpContext(ctx, coinbaseMaturity+1, f.addr); err != nil {
			return nil, 0, fmt.Errorf("FundAddress: mine to faucet: %w", err)
		}
	}
}

// balance sorts the scanned faucet outputs by whether they can be spent in
// the block after tip.
func (f *faucet) balance(utxos []ScantxoutsetUnspent, tip int64) (faucetBalance, error) {
	var b faucetBalance
	for _, u := range utxos {
		value, err := btcutil.NewAmount(u.Amount)
		if err != nil {
			return b, fmt.Errorf("faucet output %s:%d: %w", u.TxID, u.Vout, err)
		}
		if int64(value) <= faucetInputFee {
			continue
		}
		hash, err := chainhash.NewHashFromStr(u.TxID)
		if err != nil {
			return b, fmt.Errorf("faucet output %s:%d: %w", u.TxID, u.Vout, err)
		}
		coin := faucetCoin{op: wire.OutPoint{Hash: *hash, Index: u.Vout}, value: int64(value)}
		b.held += coin.value
		if _, isChange := f.change[coin.op]; isChange || tip+1-u.Height >= coinbaseMaturity {
			b.spendable = append(b.spendable, coin)
		} else {
			b.immature += coin.value
		}
	}
	return b, nil
}

// canGrow returns an error if mining from height next on can't give the
// faucet enough spendable value to pay sats.
func (b faucetBalance) canGrow(sats, next int64) error {
	if future := subsidyFrom(next); sats > b.held+future {
		return fmt.Errorf("can't fund %d sats: the faucet holds %d and the block subsidy from height %d on adds at most %d",
			sats, b.held, next, future)
	}
	if subsidy := blockSubsidy(next); b.immature == 0 && subsidy <= faucetInputFee {
		return fmt.Errorf("can't fund %d sats plus fee: the faucet's spendable outputs fall short and the block subsidy at height %d (%d sats) is too small to add more",
			sats, next, subsidy)
	}
	return nil
}

// selectCoins picks coins, largest first, until they pay sats and the fee.
// It returns the picked coins, the change (0 when it's below
// faucetMinChange and goes to the fee instead), and whether coins suffice.
// It reorders coins.
func selectCoins(coins []faucetCoin, sats int64) ([]faucetCoin, int64, bool) {
	sort.Slice(coins, func(i, j int) bool { return coins[i].value > coins[j].value })
	var sum int64
	for i, c := range coins {
		sum += c.value
		n := i + 1
		if sum < sats+faucetFee(n, 1) {
			continue
		}
		change := sum - sats - faucetFee(n, 2)
		if change < faucetMinChange {
			change = 0
		}
		return coins[:n], change, true
	}
	return nil, 0, false
}

// faucetFee is the fee of a transaction spending inputs P2WPKH outputs into
// outputs P2WPKH-sized outputs. Its vsize is about 10.5 + 68 per input + 31
// per output, rounded up.
func faucetFee(inputs, outputs int) int64 {
	doubled := 21 + 136*int64(inputs) + 62*int64(outputs)
	return (doubled + 1) / 2 * faucetFeeRate
}

// buildTx returns a signed version 2 transaction spending coins: output 0
// pays sats to destScript, and output 1, if change > 0, returns change to
// the faucet.
func (f *faucet) buildTx(coins []faucetCoin, destScript []byte, sats, change int64) (*wire.MsgTx, error) {
	tx := wire.NewMsgTx(2)
	prevOuts := make(map[wire.OutPoint]*wire.TxOut, len(coins))
	for _, c := range coins {
		tx.AddTxIn(wire.NewTxIn(&c.op, nil, nil))
		prevOuts[c.op] = wire.NewTxOut(c.value, f.pkScript)
	}
	tx.AddTxOut(wire.NewTxOut(sats, destScript))
	if change > 0 {
		tx.AddTxOut(wire.NewTxOut(change, f.pkScript))
	}

	sigHashes := txscript.NewTxSigHashes(tx, txscript.NewMultiPrevOutFetcher(prevOuts))
	for i, c := range coins {
		wit, err := txscript.WitnessSignature(tx, sigHashes, i, c.value, f.pkScript, txscript.SigHashAll, f.key, true)
		if err != nil {
			return nil, fmt.Errorf("sign input %d: %w", i, err)
		}
		tx.TxIn[i].Witness = wit
	}
	return tx, nil
}

// blockSubsidy returns the regtest block subsidy at height, in sats.
func blockSubsidy(height int64) int64 {
	halvings := height / regtestHalvingInterval
	if halvings >= 64 {
		return 0
	}
	return initialSubsidy >> halvings
}

// subsidyFrom returns the total subsidy of all blocks from height on.
func subsidyFrom(height int64) int64 {
	var total int64
	for s := blockSubsidy(height); s > 0; s = blockSubsidy(height) {
		next := (height/regtestHalvingInterval + 1) * regtestHalvingInterval
		total += s * (next - height)
		height = next
	}
	return total
}
