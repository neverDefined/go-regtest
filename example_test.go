package regtest_test

import (
	"fmt"
	"log"

	"github.com/btcsuite/btcd/wire"
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
	var rt *regtest.Regtest // a started node

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
	var rt *regtest.Regtest // a started node
	var miner string        // an address to mine to

	if ok, err := rt.SupportsBIP(regtest.BIP119); err != nil || !ok {
		fmt.Println("needs bitcoind-inquisition")
		return
	}
	mined, err := rt.MineUntilActiveBIP(regtest.BIP119, miner, 2000)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("CTV active after", mined, "blocks")
}

// Confirm a transaction that mempool policy rejects but consensus allows.
func ExampleRegtest_GenerateBlock() {
	var rt *regtest.Regtest // a started node
	var tx *wire.MsgTx      // a signed, consensus-valid, non-standard tx
	var miner string        // an address to mine to

	res, err := rt.TestMempoolAccept(tx)
	if err != nil {
		log.Fatal(err)
	}
	if !res[0].Allowed {
		fmt.Println("mempool rejects it:", res[0].RejectReason)
	}
	hash, err := rt.GenerateBlock(miner, tx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("confirmed in", hash)
}

// FundAddress needs no wallet setup on the node's side.
func ExampleRegtest_FundAddress() {
	var rt *regtest.Regtest // a started node
	var addr string         // any regtest address, e.g. one your code derived

	op, err := rt.FundAddress(addr, 1_000_000)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("spendable output:", op)
}
