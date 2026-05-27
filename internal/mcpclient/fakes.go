package mcpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// FakeRunner is an in-process Runner used by tests. It records the cmd/args/env
// passed to Start and returns a FakeProcess that replies to JSON-RPC requests
// from a scripted response table keyed by method name.
type FakeRunner struct {
	// Responses maps method name (e.g. "tools/list") to the raw JSON to put in
	// the "result" field of the response. Missing methods cause the
	// FakeProcess to return a JSON-RPC error.
	Responses map[string]json.RawMessage
	// Errors maps method name to a JSON-RPC error to return for that method.
	// Useful for protocol-error tests.
	Errors map[string]*rpcError
	// ResponseDelay is applied before each response is written. Used to
	// exercise the wall-clock timeout path.
	ResponseDelay time.Duration
	// ExitEarly, when true, causes the FakeProcess to close stdout (EOF)
	// after the initialize response, simulating a server that died before
	// tools/list.
	ExitEarly bool
	// StderrOnStart is written to the process's stderr at startup. Used to
	// exercise the stderr-tail surface of wrapped errors.
	StderrOnStart string
	// StartErr, when non-nil, is returned from Start without spawning a
	// process.
	StartErr error

	mu       sync.Mutex
	lastCmd  string
	lastArgs []string
	lastEnv  []string
	procs    []*FakeProcess
}

// LastCmd returns the cmd passed to the most recent Start.
func (r *FakeRunner) LastCmd() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCmd
}

// LastArgs returns the args passed to the most recent Start.
func (r *FakeRunner) LastArgs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lastArgs...)
}

// LastEnv returns the env slice passed to the most recent Start.
func (r *FakeRunner) LastEnv() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lastEnv...)
}

// LastProcess returns the most recent FakeProcess (for asserting Kill calls).
func (r *FakeRunner) LastProcess() *FakeProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.procs) == 0 {
		return nil
	}
	return r.procs[len(r.procs)-1]
}

// Start implements Runner. It captures the call and returns a new FakeProcess.
func (r *FakeRunner) Start(cmd string, args []string, env []string) (Process, error) {
	r.mu.Lock()
	r.lastCmd = cmd
	r.lastArgs = append([]string(nil), args...)
	r.lastEnv = append([]string(nil), env...)
	r.mu.Unlock()

	if r.StartErr != nil {
		return nil, r.StartErr
	}

	p := newFakeProcess(r)
	r.mu.Lock()
	r.procs = append(r.procs, p)
	r.mu.Unlock()

	go p.run()
	return p, nil
}

// FakeProcess is the Process implementation produced by FakeRunner.
type FakeProcess struct {
	runner *FakeRunner

	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter

	mu       sync.Mutex
	killed   bool
	killedAt time.Time
	done     chan struct{}
}

func newFakeProcess(r *FakeRunner) *FakeProcess {
	sinR, sinW := io.Pipe()
	soR, soW := io.Pipe()
	seR, seW := io.Pipe()
	return &FakeProcess{
		runner:  r,
		stdinR:  sinR,
		stdinW:  sinW,
		stdoutR: soR,
		stdoutW: soW,
		stderrR: seR,
		stderrW: seW,
		done:    make(chan struct{}),
	}
}

func (p *FakeProcess) Stdin() io.WriteCloser { return p.stdinW }
func (p *FakeProcess) Stdout() io.ReadCloser { return p.stdoutR }
func (p *FakeProcess) Stderr() io.ReadCloser { return p.stderrR }

// Kill marks the process killed and tears down its pipes.
func (p *FakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed {
		return nil
	}
	p.killed = true
	p.killedAt = time.Now()
	_ = p.stdoutW.Close()
	_ = p.stderrW.Close()
	_ = p.stdinR.CloseWithError(io.ErrClosedPipe)
	return nil
}

// Killed reports whether Kill was called.
func (p *FakeProcess) Killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// Wait blocks until the run goroutine returns.
func (p *FakeProcess) Wait() error {
	<-p.done
	return nil
}

// run reads JSON-RPC frames from stdin and writes scripted responses to stdout.
func (p *FakeProcess) run() {
	defer close(p.done)
	defer p.stdoutW.Close()
	defer p.stderrW.Close()
	// Close the read end of stdin so any pending or future writes from the
	// client see EPIPE rather than blocking forever.
	defer p.stdinR.Close()

	if p.runner.StderrOnStart != "" {
		_, _ = p.stderrW.Write([]byte(p.runner.StderrOnStart))
	}

	dec := json.NewDecoder(p.stdinR)
	answered := 0
	for {
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int            `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&msg); err != nil {
			return
		}
		if msg.ID == nil {
			// Notification; no reply.
			continue
		}

		if p.runner.ResponseDelay > 0 {
			select {
			case <-time.After(p.runner.ResponseDelay):
			case <-p.done:
				return
			}
			// If we were killed during the delay, exit.
			if p.Killed() {
				return
			}
		}

		resp := rpcResponse{JSONRPC: "2.0", ID: *msg.ID}
		if rpcErr, ok := p.runner.Errors[msg.Method]; ok && rpcErr != nil {
			resp.Error = rpcErr
		} else if result, ok := p.runner.Responses[msg.Method]; ok {
			resp.Result = result
		} else {
			resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("method not scripted: %s", msg.Method)}
		}

		raw, err := json.Marshal(resp)
		if err != nil {
			return
		}
		raw = append(raw, '\n')
		if _, err := p.stdoutW.Write(raw); err != nil {
			return
		}
		answered++

		if p.runner.ExitEarly && msg.Method == "initialize" {
			return
		}
	}
}
