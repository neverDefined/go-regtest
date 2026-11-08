package regtest

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// process is a bitcoind child running in the foreground. done is closed when
// the process exits; err holds cmd.Wait's result once done is closed.
type process struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output *tailBuffer
}

// startProcess launches bitcoind and returns immediately. stdout and stderr
// are merged into a bounded buffer so startup failures can report what
// bitcoind printed.
//
// It deliberately uses exec.Command rather than exec.CommandContext: the
// caller's context bounds startup only, not the node's lifetime.
func startProcess(path string, args []string) (*process, error) {
	cmd := exec.Command(path, args...)
	out := newTailBuffer(32 << 10)
	cmd.Stdout = out
	cmd.Stderr = out
	setParentDeathSignal(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch %s: %w", path, err)
	}
	p := &process{cmd: cmd, done: make(chan struct{}), output: out}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
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

// kill sends SIGKILL and waits for the process to exit.
func (p *process) kill() {
	_ = p.cmd.Process.Kill()
	<-p.done
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

// lastLines returns up to n trailing non-empty lines.
func (t *tailBuffer) lastLines(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(string(bytes.TrimSpace(t.buf)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
