package regtest

import (
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

func TestConnection(t *testing.T) {
	t.Parallel()
	rt := StartT(t, nil)

	err := rt.HealthCheck()
	if err != nil {
		t.Fatalf("failed to check health: %v", err)
	}

}

// TestRPC_EnsureWallet_States drives EnsureWallet through a missing, an
// already-loaded, and an unloaded-on-disk wallet, and pins the Core error
// codes its branches rely on.

func TestRPCMethods_BeforeStart(t *testing.T) {
	rt, err := New(nil)
	if err != nil {
		t.Fatalf("failed to create regtest: %v", err)
	}
	t.Cleanup(func() { _ = rt.Cleanup() })

	checks := []struct {
		name string
		call func() error
	}{
		{"GetBlockCount", func() error { _, err := rt.GetBlockCount(); return err }},
		{"HealthCheck", func() error { return rt.HealthCheck() }},
		{"GetWalletInformation", func() error { _, err := rt.GetWalletInformation(); return err }},
		{"CreateWallet", func() error { _, err := rt.CreateWallet("w"); return err }},
		{"LoadWallet", func() error { _, err := rt.LoadWallet("w"); return err }},
		{"UnloadWallet", func() error { return rt.UnloadWallet("w") }},
		{"GenerateBech32", func() error { _, err := rt.GenerateBech32("l"); return err }},
		{"GenerateBech32m", func() error { _, err := rt.GenerateBech32m("l"); return err }},
		{"ScanTxOutSetForAddress", func() error {
			_, err := rt.ScanTxOutSetForAddress("bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
			return err
		}},
		{"Warp", func() error {
			return rt.Warp(1, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
		}},
		{"GetBlockChainInfo", func() error { _, err := rt.GetBlockChainInfo(); return err }},
		{"GetBestBlockHash", func() error { _, err := rt.GetBestBlockHash(); return err }},
		{"GetBlockHash", func() error { _, err := rt.GetBlockHash(0); return err }},
		{"GetBlock", func() error { _, err := rt.GetBlock(&chainhash.Hash{}); return err }},
		{"GetBlockVerbose", func() error { _, err := rt.GetBlockVerbose(&chainhash.Hash{}); return err }},
		{"GetBlockHeader", func() error { _, err := rt.GetBlockHeader(&chainhash.Hash{}); return err }},
		{"GetChainTips", func() error { _, err := rt.GetChainTips(); return err }},
		{"GetDeploymentInfo", func() error { _, err := rt.GetDeploymentInfo(); return err }},
		{"DeploymentStatus", func() error { _, err := rt.DeploymentStatus("taproot"); return err }},
		{"TestMempoolAccept", func() error {
			tx := wire.NewMsgTx(2)
			tx.AddTxIn(&wire.TxIn{})
			tx.AddTxOut(wire.NewTxOut(0, []byte{0x6a}))
			_, err := rt.TestMempoolAccept(tx)
			return err
		}},
		{"InvalidateBlock", func() error { return rt.InvalidateBlock(&chainhash.Hash{}) }},
		{"ReconsiderBlock", func() error { return rt.ReconsiderBlock(&chainhash.Hash{}) }},
		{"PreciousBlock", func() error { return rt.PreciousBlock(&chainhash.Hash{}) }},
		{"MineToHeight", func() error {
			return rt.MineToHeight(1, "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl")
		}},
		{"MineUntilActive", func() error {
			_, err := rt.MineUntilActive("testdummy", "bcrt1qvhadhnxjjeczwgm7y54m2dplur6q2895gtnthl", 100)
			return err
		}},
		{"GetBlockTemplate", func() error {
			_, err := rt.GetBlockTemplate(&btcjson.TemplateRequest{Mode: "template", Rules: []string{"segwit"}})
			return err
		}},
		{"SubmitBlock", func() error { return rt.SubmitBlock(&wire.MsgBlock{}) }},
		{"CreateRawTransaction", func() error {
			_, err := rt.CreateRawTransaction(nil, nil, nil)
			return err
		}},
		{"DecodeRawTransaction", func() error {
			_, err := rt.DecodeRawTransaction(wire.NewMsgTx(2))
			return err
		}},
		{"DecodeScript", func() error { _, err := rt.DecodeScript("00"); return err }},
		{"FundRawTransaction", func() error {
			_, err := rt.FundRawTransaction(wire.NewMsgTx(2), nil)
			return err
		}},
		{"Connect", func() error { return rt.Connect(&Regtest{config: DefaultConfig()}) }},
		{"Disconnect", func() error { return rt.Disconnect(&Regtest{config: DefaultConfig()}) }},
		{"AddNode", func() error { return rt.AddNode("127.0.0.1:18444") }},
		{"GetConnectionCount", func() error { _, err := rt.GetConnectionCount(); return err }},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); !errors.Is(err, ErrNotConnected) {
				t.Errorf("expected ErrNotConnected, got %v", err)
			}
		})
	}
}

// Test_StartContext_PreCancelled verifies that StartContext surfaces a
// pre-cancelled context's error rather than spawning bitcoind.
