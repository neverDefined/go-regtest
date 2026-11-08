/*
Package regtest runs a Bitcoin Core (or Bitcoin Inquisition) node in regtest
mode for Go tests, and wraps the RPCs those tests need.

# Starting a node

In a test, StartT gives each test its own node on free ports with a temporary
data directory, and stops it when the test ends:

	func TestSomething(t *testing.T) {
		t.Parallel()
		rt := regtest.StartT(t, nil)
		// ...
	}

Elsewhere, use New, then Start and Stop. Start launches bitcoind as a child
process and returns once it answers RPC calls. Every Start begins at the
genesis block, because Start clears the data directory.

The bitcoind binary is Config.BinaryPath if set, otherwise
bitcoind-inquisition or bitcoind from PATH. A node listens on 127.0.0.1 only,
on two consecutive ports: Config.Host's port for RPC and the next one for P2P.

# Wallets

Wallet methods on Regtest itself use bitcoind's default wallet endpoint, which
works while exactly one wallet is loaded. For more than one, use Wallet
handles, which address a wallet by name:

	_ = rt.EnsureWallet("alice")
	_ = rt.EnsureWallet("bob")
	alice, bob := rt.Wallet("alice"), rt.Wallet("bob")
	addr, _ := alice.NewAddress("", regtest.AddressBech32)
	_ = rt.Warp(101, addr) // mature coinbase for alice
	bobAddr, _ := bob.NewAddress("", regtest.AddressBech32m)
	_, _ = alice.SendToAddress(bobAddr, 50_000_000)

FundAddress returns a confirmed output paying any address, without wallet
setup.

# Soft forks

VBParams configure BIP9 deployments. DeploymentStatus, GetDeploymentInfo and
ListDeployments report their state. MineUntilActive and MineUntilActiveBIP
mine until a deployment is active. The BIPID constants name the deployments
this package knows (testdummy, taproot, and Inquisition's BIP54, BIP118,
BIP119, BIP347, BIP348, BIP349). SupportsBIP checks the running node, so a
test can skip when its binary lacks a deployment:

	if ok, _ := rt.SupportsBIP(regtest.BIP119); !ok {
		t.Skip("needs bitcoind-inquisition")
	}

TestMempoolAccept reports why a transaction would be rejected, and
GenerateBlock confirms a consensus-valid transaction that mempool policy
rejects.

# Errors

RPC methods return ErrNotConnected before Start or after Stop. Deployment
lookups return ErrUnknownDeployment or ErrUnknownBIP, and New returns
ErrBinaryNotFound when there is no bitcoind. Check them with errors.Is. Other
errors wrap bitcoind's *btcjson.RPCError where there is one.

# Concurrency

A Regtest is safe for concurrent use. Every RPC method has a Context variant.
Because btcd's client can't cancel a request in flight, cancelling the context
returns early but lets the request finish in the background.
*/
package regtest
