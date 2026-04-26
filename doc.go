/*
Package regtest provides a lightweight Go library for managing Bitcoin Core
or Bitcoin Inquisition regtest environments.

Regtest mode creates a private blockchain for testing and development. This package simplifies
starting, managing, and interacting with regtest nodes programmatically. The same Config works
against stock Bitcoin Core and against Bitcoin Inquisition (the experimental Core fork that
activates upcoming soft forks: BIP54, BIP118 ANYPREVOUT, BIP119 OP_CHECKTEMPLATEVERIFY,
BIP347 OP_CAT, BIP348 OP_CHECKSIGFROMSTACK, BIP349 OP_INTERNALKEY).

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
  - Binary: PATH auto-detect — bitcoind-inquisition first, then bitcoind

Customize via Config struct when creating instances. Set Config.BinaryPath to point at a
non-default bitcoind build (absolute path, relative path, or bare name resolved via PATH).
The bitcoin-cli companion is derived from the same directory, falling back to PATH.

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

# Soft-fork Testing

VBParams configure named BIP9 deployments via -vbparams. DeploymentStatus and
GetDeploymentInfo expose the current state machine; MineUntilActive (string-keyed) and
MineUntilActiveBIP (typed BIPID) drive a deployment to SoftForkActive over retarget windows.

The curated registry maps typed BIPID constants to deployment names, BIP numbers, and doc
URLs:

  - BIPTestdummy, BIPTaproot — present on both Core and Inquisition
  - BIP54, BIP118, BIP119, BIP347, BIP348, BIP349 — Inquisition-only

ListDeployments returns the merged registry-and-live view; SupportsBIP is the canonical
skip-when-missing primitive for tests that need an Inquisition-only deployment:

	if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
	    t.Skip("requires bitcoind-inquisition")
	}

Variant reports VariantCore or VariantInquisition (parsed from getnetworkinfo.subversion)
once Start has succeeded; the result is cached so repeat calls are free.

# Thread Safety

All Regtest methods are thread-safe. Multiple goroutines can safely call Start(), Stop(),
IsRunning(), and make RPC calls concurrently. Always use defer rt.Stop() for cleanup.

# Error Handling

Check errors from all methods. Common errors:
  - bitcoind not found in PATH (tried bitcoind-inquisition, bitcoind)
  - Port already in use
  - RPC connection failures
  - Invalid addresses or parameters
  - Insufficient funds

Sentinels are errors.Is-compatible:
  - errNotConnected — RPC method called before Start
  - ErrUnknownDeployment — deployment name not in getdeploymentinfo
  - ErrUnknownBIP — BIPID not in the curated registry

# Prerequisites

Install Bitcoin Core:
  - macOS: brew install bitcoin
  - Ubuntu/Debian: sudo apt-get install bitcoind
  - Arch: sudo pacman -S bitcoin-core

For testing upcoming soft forks, build Bitcoin Inquisition from source — see the README
for the cmake recipe. The built bitcoind can be picked up via Config.BinaryPath, or by
symlinking it as bitcoind-inquisition on PATH so the auto-detect chain finds it.

# Port Considerations

When running multiple instances, use widely spaced ports (e.g., 19000, 19100) because Bitcoin
Core uses both RPC and P2P ports (typically RPC port + 1). Each instance needs a unique
data directory.

# Use Cases

Ideal for integration testing, development, CI/CD pipelines, education, and multi-node testing.
NOT for production use.
*/
package regtest
