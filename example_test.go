package regtest_test

import (
	"fmt"
	"log"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/neverDefined/go-regtest"
)

// Outside tests: start a node, mine to a wallet address, read the height.
// In tests, regtest.StartT(t, nil) replaces New, Start and Stop.
func Example() {
	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet("miner"); err != nil {
		log.Fatal(err)
	}
	addr, err := rt.Wallet("miner").NewAddress("", regtest.AddressBech32)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Warp(101, addr); err != nil { // 101 blocks: the first coinbase matures
		log.Fatal(err)
	}
	height, err := rt.GetBlockCount()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("height:", height)
}

// Two loaded wallets need Wallet handles; the default-endpoint wallet
// methods on Regtest only work while one wallet is loaded.
func ExampleRegtest_Wallet() {
	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	for _, name := range []string{"alice", "bob"} {
		if err := rt.EnsureWallet(name); err != nil {
			log.Fatal(err)
		}
	}
	alice, bob := rt.Wallet("alice"), rt.Wallet("bob")

	aliceAddr, _ := alice.NewAddress("", regtest.AddressBech32)
	_ = rt.Warp(101, aliceAddr)
	bobAddr, _ := bob.NewAddress("", regtest.AddressBech32m)
	if _, err := alice.SendToAddress(bobAddr, 50_000_000); err != nil {
		log.Fatal(err)
	}
	_ = rt.Warp(1, aliceAddr)

	balance, _ := bob.Balance()
	fmt.Println("bob has", balance)
}

// Skip when the node's binary doesn't know a deployment, then mine until it
// is active.
func ExampleRegtest_SupportsBIP() {
	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	ok, err := rt.SupportsBIP(regtest.BIP119)
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("needs bitcoind-inquisition")
		return
	}
	if err := rt.EnsureWallet("miner"); err != nil {
		log.Fatal(err)
	}
	miner, err := rt.Wallet("miner").NewAddress("", regtest.AddressBech32m)
	if err != nil {
		log.Fatal(err)
	}
	mined, err := rt.MineUntilActiveBIP(regtest.BIP119, miner, 2000)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("CTV active after", mined, "blocks")
}

// Confirm a transaction that mempool policy rejects but consensus allows:
// here, one with version 4.
func ExampleRegtest_GenerateBlock() {
	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	if err := rt.EnsureWallet("w"); err != nil {
		log.Fatal(err)
	}
	w := rt.Wallet("w")
	addr, err := w.NewAddress("", regtest.AddressBech32)
	if err != nil {
		log.Fatal(err)
	}
	op, err := rt.FundAddress(addr, 100_000_000)
	if err != nil {
		log.Fatal(err)
	}
	dest, err := btcutil.DecodeAddress(addr, &chaincfg.RegressionNetParams)
	if err != nil {
		log.Fatal(err)
	}
	tx, err := rt.CreateRawTransaction(
		[]btcjson.TransactionInput{{Txid: op.Hash.String(), Vout: op.Index}},
		map[btcutil.Address]btcutil.Amount{dest: 99_990_000},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}
	tx.Version = 4 // valid by consensus, non-standard by policy
	signed, err := w.SignRawTransaction(tx)
	if err != nil {
		log.Fatal(err)
	}

	res, err := rt.TestMempoolAccept(signed)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("mempool rejects it:", res[0].RejectReason)

	hash, err := rt.GenerateBlock(addr, signed)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("confirmed in", hash)
}

// FundAddress pays any regtest address, here one derived from a key held in
// Go, with no wallet on the node.
func ExampleRegtest_FundAddress() {
	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	key, err := btcec.NewPrivateKey()
	if err != nil {
		log.Fatal(err)
	}
	addr, err := btcutil.NewAddressWitnessPubKeyHash(
		btcutil.Hash160(key.PubKey().SerializeCompressed()), &chaincfg.RegressionNetParams)
	if err != nil {
		log.Fatal(err)
	}

	op, err := rt.FundAddress(addr.EncodeAddress(), 1_000_000)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("spendable output:", op)
}
