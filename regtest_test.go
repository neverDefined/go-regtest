package regtest

import (
	"testing"

	"github.com/btcsuite/btcd/rpcclient"
)

func Test_Regtest(t *testing.T) {
	err := StartBitcoinRegtest()
	if err != nil {
		t.Fatalf("failed to start bitcoind: %v", err)
	}

	rpcClient, err := rpcclient.New(DefaultRegtestConfig(), nil)
	if err != nil {
		t.Fatalf("failed to connect via rpc client: %v", err)
	}
	defer rpcClient.Shutdown()

	_, err = rpcClient.GetBlockCount()
	if err != nil {
		t.Fatalf("failed to get block count (health check), %v", err)
	}

	// Test stopping bitcoind
	err = StopBitcoinRegtest()
	if err != nil {
		t.Fatalf("failed to stop bitcoind: %v", err)
	}

	// Test that it's actually stopped
	running, err := IsBitcoindRunning()
	if err != nil {
		t.Fatalf("failed to check if bitcoind is running: %v", err)
	}
	if running {
		t.Fatal("bitcoind should not be running after stop")
	}

	t.Log("bitcoind management test passed")
}
