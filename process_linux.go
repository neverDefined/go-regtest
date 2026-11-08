//go:build linux

package regtest

import (
	"os/exec"
	"syscall"
)

// setParentDeathSignal makes the kernel SIGKILL bitcoind if the Go process
// dies (test timeout, Ctrl-C, kill -9), so no node is left holding its ports.
func setParentDeathSignal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
