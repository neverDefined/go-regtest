/*
Package regtest provides a lightweight Go library for managing Bitcoin Core regtest environments.

Regtest mode creates a private blockchain for testing and development. This package simplifies
starting, managing, and interacting with regtest nodes programmatically.

Quick Start

	rt, err := regtest.New(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer rt.Stop()

	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}

	rt.EnsureWallet("miner")
	addr, _ := rt.GenerateBech32("miner")
	rt.Warp(101, addr) // Mine to maturity

	height, _ := rt.GetBlockCount()
	fmt.Printf("Block height: %d\n", height)

# Architecture

Each Regtest instance manages a single Bitcoin Core regtest node. Instances are thread-safe
and can run concurrently. Multiple instances can run simultaneously on different ports with
separate data directories.

# Configuration

Default settings:
  - RPC host: 127.0.0.1:18443
  - RPC user: user
  - RPC pass: pass
  - Data directory: ./bitcoind_regtest

Customize via Config struct when creating instances.

# Examples

Multiple Instances:

	rt1, _ := regtest.New(&regtest.Config{Host: "127.0.0.1:19000", DataDir: "./regtest_1"})
	rt2, _ := regtest.New(&regtest.Config{Host: "127.0.0.1:19100", DataDir: "./regtest_2"})
	rt1.Start()
	rt2.Start()
	defer rt1.Stop()
	defer rt2.Stop()

Transactions:

	rt.EnsureWallet("sender")
	rt.EnsureWallet("receiver")
	senderAddr, _ := rt.GenerateBech32("sender")
	receiverAddr, _ := rt.GenerateBech32("receiver")

	rt.Warp(101, senderAddr) // Fund sender
	txid, _ := rt.SendToAddress(receiverAddr, 50_000_000) // Send 0.5 BTC
	rt.Warp(1, senderAddr) // Confirm

	utxo, _ := rt.GetTxOut(txid, 0, true)
	fmt.Printf("Confirmed: %.8f BTC\n", utxo.Value)

UTXO Scanning:

	utxos, _ := rt.ScanTxOutSetForAddress(addr)
	for _, utxo := range utxos {
		fmt.Printf("UTXO: %s:%d with %.8f BTC\n", utxo.TxID, utxo.Vout, utxo.Amount)
	}

Transaction Signing:

	signedTx, _ := rt.SignRawTransactionWithWallet(unsignedTx)
	txid, _ := rt.BroadcastTransaction(signedTx)

Direct RPC Access:

	client := rt.Client()
	info, _ := client.GetBlockChainInfo()
	mempool, _ := client.GetRawMempool()

# Thread Safety

All Regtest methods are thread-safe. Multiple goroutines can safely call Start(), Stop(),
IsRunning(), and make RPC calls concurrently. Always use defer rt.Stop() for cleanup.

# Error Handling

Check errors from all methods. Common errors:
  - bitcoind not found in PATH
  - Port already in use
  - RPC connection failures
  - Invalid addresses or parameters
  - Insufficient funds

# Prerequisites

Install Bitcoin Core:
  - macOS: brew install bitcoin
  - Ubuntu/Debian: sudo apt-get install bitcoind
  - Arch: sudo pacman -S bitcoin-core

# Port Considerations

When running multiple instances, use widely spaced ports (e.g., 19000, 19100) because Bitcoin
Core uses both RPC and P2P ports (typically RPC port + 1). Each instance needs a unique
data directory.

# Use Cases

Ideal for integration testing, development, CI/CD pipelines, education, and multi-node testing.
NOT for production use.
*/
package regtest
