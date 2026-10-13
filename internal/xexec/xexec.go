// Package xexec runs external tools (ffmpeg, edge-tts) with context
// cancellation and keeps the tail of stderr for error messages.
package xexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// tailBuffer keeps the last max bytes written to it. It is safe for the
// concurrent writes os/exec makes from its copying goroutine.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
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

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

const stderrTail = 4 << 10

// Run executes a command and returns its stdout. On failure the error
// includes the command name and the end of stderr.
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	errBuf := &tailBuffer{max: stderrTail}
	cmd.Stdout = &out
	cmd.Stderr = errBuf
	if err := cmd.Run(); err != nil {
		return nil, wrap(ctx, name, err, errBuf)
	}
	return out.Bytes(), nil
}

// RunLog executes a command for its diagnostics: it returns the end of
// stderr, where tools like ffmpeg print their measurements.
func RunLog(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	errBuf := &tailBuffer{max: stderrTail}
	cmd.Stderr = errBuf
	if err := cmd.Run(); err != nil {
		return "", wrap(ctx, name, err, errBuf)
	}
	return errBuf.String(), nil
}

// Proc is a started command fed through stdin.
type Proc struct {
	name   string
	ctx    context.Context
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tailBuffer
}

// Start launches a command with a stdin pipe. Stdout is discarded.
func Start(ctx context.Context, name string, args ...string) (*Proc, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	p := &Proc{name: name, ctx: ctx, cmd: cmd, stderr: &tailBuffer{max: stderrTail}}
	cmd.Stderr = p.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p.stdin = in
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return p, nil
}

// Write sends data to the process. A write error usually means the process
// died; Wait then reports why.
func (p *Proc) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Wait closes stdin and waits for the process to exit.
func (p *Proc) Wait() error {
	cerr := p.stdin.Close()
	if err := p.cmd.Wait(); err != nil {
		return wrap(p.ctx, p.name, err, p.stderr)
	}
	return cerr
}

// Kill stops the process and reaps it. Use it on error paths.
func (p *Proc) Kill() {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
}

func wrap(ctx context.Context, name string, err error, stderr *tailBuffer) error {
	if cerr := context.Cause(ctx); cerr != nil {
		return fmt.Errorf("%s: %w", name, cerr)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed or not in PATH: %w", name, err)
	}
	if s := lastLines(stderr.String(), 2); s != "" {
		return fmt.Errorf("%s: %w: %s", name, err, s)
	}
	return fmt.Errorf("%s: %w", name, err)
}

// lastLines keeps the final n non-empty lines: for a Python traceback or an
// ffmpeg failure that is where the actual reason is.
func lastLines(s string, n int) string {
	var keep []string
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " | ")
}
