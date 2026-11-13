package regtest

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// process is a bitcoind child running in the foreground. done is closed when
// the process exits; err holds cmd.Wait's result once done is closed.
type process struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output *tailBuffer
}

// waitDelay bounds how long Wait keeps reading output after bitcoind exits.
// Commands bitcoind spawns (e.g. -shutdownnotify) inherit its stdout and
// stderr; without a bound, Wait would block until they exit too.
const waitDelay = 2 * time.Second

// startProcess launches bitcoind and returns once it has started. stdout and
// stderr are merged into a bounded buffer so startup failures can report what
// bitcoind printed.
//
// It deliberately uses exec.Command rather than exec.CommandContext: the
// caller's context bounds startup only, not the node's lifetime.
func startProcess(path string, args []string) (*process, error) {
	cmd := exec.Command(path, args...)
	out := newTailBuffer(32 << 10)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = waitDelay
	setParentDeathSignal(cmd)

	p := &process{cmd: cmd, done: make(chan struct{}), output: out}
	started := make(chan error, 1)
	go func() {
		// On Linux the parent-death signal fires when the OS thread that
		// started the child exits, not when the Go process does. Start and
		// wait on one locked thread so that thread lives as long as bitcoind.
		// The goroutine never unlocks, so the thread exits after bitcoind does.
		runtime.LockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		p.err = cmd.Wait()
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, fmt.Errorf("launch %s: %w", path, err)
	}
	return p, nil
}

// exited reports whether the process has already exited.
func (p *process) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// terminate asks the process to shut down cleanly (SIGTERM). It fails where
// signals aren't supported (Windows).
func (p *process) terminate() error {
	return p.cmd.Process.Signal(syscall.SIGTERM)
}

// kill sends SIGKILL and waits (bounded) for the process to be reaped.
func (p *process) kill() {
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(waitDelay + 5*time.Second):
	}
}

// tailBuffer is an io.Writer that keeps only the last max bytes written.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer {
	return &tailBuffer{max: max}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

// lastLines returns up to the last n lines, ignoring leading and trailing
// whitespace.
func (t *tailBuffer) lastLines(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(string(bytes.TrimSpace(t.buf)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
