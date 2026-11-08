//go:build !linux

package regtest

import "os/exec"

// setParentDeathSignal is a no-op outside Linux. If the Go process is killed
// without running Stop, bitcoind keeps running until it is stopped by hand.
func setParentDeathSignal(*exec.Cmd) {}
