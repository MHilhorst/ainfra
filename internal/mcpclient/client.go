// Package mcpclient is a minimal stdio JSON-RPC client for MCP servers. It
// performs the handshake required to call tools/list and returns the resulting
// toolset in a canonical (sorted, schema-normalized) form suitable for stable
// content hashing.
//
// The client speaks only three messages: initialize, notifications/initialized,
// and tools/list. The subprocess runs with a restricted environment (only PATH,
// HOME, and explicitly-declared keys reach it) and a wall-clock timeout. On
// timeout the subprocess is hard-killed.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// defaultTimeout is the wall-clock budget for the full handshake when the
// caller does not specify one.
const defaultTimeout = 15 * time.Second

// stderrTailLimit caps how many bytes of subprocess stderr are surfaced in
// wrapped errors.
const stderrTailLimit = 500

// Tool is one entry returned from tools/list, normalized for hashing.
//
// InputSchema is kept as the raw JSON bytes the server sent, but re-encoded so
// object key ordering is deterministic. Callers can hash InputSchema directly
// or feed the whole ToolList into a content hash.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolList is a canonical, deterministically-ordered list of tools.
type ToolList []Tool

// Request describes one introspection call.
type Request struct {
	// Command is the executable to run (resolved against PATH).
	Command string
	// Args are the subprocess arguments.
	Args []string
	// Env are the only env vars (beyond PATH and HOME) propagated to the
	// subprocess. Nil and empty mean "no extra env".
	Env map[string]string
	// Timeout is the wall-clock budget for the full handshake. Zero means
	// defaultTimeout (15s).
	Timeout time.Duration
	// Runner injects the subprocess transport. Nil means DefaultRunner().
	Runner Runner
}

// TimeoutError signals that the wall-clock budget was exhausted before
// tools/list returned. It unwraps to context.DeadlineExceeded so callers can
// match it with errors.Is.
type TimeoutError struct {
	Op      string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("mcpclient: %s: timeout after %s", e.Op, e.Timeout)
}

func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

// Introspect starts the configured MCP server as a subprocess, performs the
// JSON-RPC handshake, calls tools/list, and returns a canonical ToolList. The
// subprocess is always killed and reaped before Introspect returns.
func Introspect(ctx context.Context, req Request) (ToolList, error) {
	if req.Command == "" {
		return nil, fmt.Errorf("mcpclient: introspect: command is empty")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runner := req.Runner
	if runner == nil {
		runner = DefaultRunner()
	}

	env := buildEnv(req.Env)
	proc, err := runner.Start(req.Command, req.Args, env)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: start: %w", err)
	}

	// Collect stderr in the background so we can include a tail in any
	// wrapped error. Bounded by stderrTailLimit to avoid unbounded memory if
	// the subprocess goes chatty.
	stderrBuf := &boundedBuffer{limit: stderrTailLimit}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrBuf, proc.Stderr())
	}()

	type result struct {
		tools ToolList
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		tools, err := runHandshake(proc)
		resCh <- result{tools: tools, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var (
		tools ToolList
		opErr error
	)
	select {
	case res := <-resCh:
		tools = res.tools
		opErr = res.err
	case <-timer.C:
		_ = proc.Kill()
		<-resCh // ensure handshake goroutine has exited
		opErr = &TimeoutError{Op: "introspect", Timeout: timeout}
	case <-ctx.Done():
		_ = proc.Kill()
		<-resCh
		opErr = fmt.Errorf("mcpclient: introspect: %w", ctx.Err())
	}

	// Close stdin if the handshake goroutine has not already done so, so the
	// subprocess sees EOF and exits.
	if stdin := proc.Stdin(); stdin != nil {
		_ = stdin.Close()
	}
	// Always reap.
	_ = proc.Wait()
	<-stderrDone

	if opErr != nil {
		if tail := stderrBuf.tail(); len(tail) > 0 && !isTimeoutErr(opErr) {
			return nil, fmt.Errorf("%w: stderr=%q", opErr, tail)
		}
		return nil, opErr
	}
	return tools, nil
}

func isTimeoutErr(err error) bool {
	var te *TimeoutError
	return errors.As(err, &te)
}

// runHandshake speaks initialize, notifications/initialized, tools/list over
// the process's stdio and returns the canonical ToolList.
func runHandshake(proc Process) (ToolList, error) {
	stdin := proc.Stdin()
	if stdin == nil {
		return nil, fmt.Errorf("mcpclient: handshake: stdin is nil")
	}
	stdout := proc.Stdout()
	if stdout == nil {
		return nil, fmt.Errorf("mcpclient: handshake: stdout is nil")
	}
	reader := bufio.NewReader(stdout)

	// 1. initialize
	if err := writeFrame(stdin, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: initializeParams{
			ProtocolVersion: protocolVersion,
			Capabilities:    map[string]any{},
			ClientInfo:      clientInfo{Name: "ainfra", Version: "0"},
		},
	}); err != nil {
		return nil, fmt.Errorf("mcpclient: write initialize: %w", err)
	}
	initResp, err := readResponse(reader, 1)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: read initialize: %w", err)
	}
	if initResp.Error != nil {
		return nil, fmt.Errorf("mcpclient: initialize rpc error: code=%d %s", initResp.Error.Code, initResp.Error.Message)
	}

	// 2. notifications/initialized (no response expected)
	if err := writeFrame(stdin, rpcNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return nil, fmt.Errorf("mcpclient: write initialized: %w", err)
	}

	// 3. tools/list
	if err := writeFrame(stdin, rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}); err != nil {
		return nil, fmt.Errorf("mcpclient: write tools/list: %w", err)
	}
	toolsResp, err := readResponse(reader, 2)
	if err != nil {
		return nil, fmt.Errorf("mcpclient: read tools/list: %w", err)
	}
	if toolsResp.Error != nil {
		return nil, fmt.Errorf("mcpclient: tools/list rpc error: code=%d %s", toolsResp.Error.Code, toolsResp.Error.Message)
	}

	var listed toolsListResult
	if err := json.Unmarshal(toolsResp.Result, &listed); err != nil {
		return nil, fmt.Errorf("mcpclient: decode tools/list result: %w", err)
	}
	return canonicalize(listed.Tools)
}

// writeFrame encodes v as a single line of JSON terminated with \n.
func writeFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// readResponse reads line-delimited JSON-RPC responses until it finds one
// whose id matches wantID, skipping any unrelated notifications the server
// sends in between.
func readResponse(r *bufio.Reader, wantID int) (*rpcResponse, error) {
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		// Peek for an id field. Notifications and server-initiated requests
		// have no matching id; ignore them.
		var probe struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if probe.ID == nil {
			// Server notification or request from server. Ignore.
			continue
		}
		if *probe.ID != wantID {
			// Response to a different request — should not happen for this
			// client, but skip rather than fail.
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &resp, nil
	}
}

// readLine reads one newline-terminated JSON frame. Returns io.EOF if the
// server closed stdout before sending a complete frame.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		// ReadSlice returns io.ErrBufferFull for long lines; keep reading.
		if err == bufio.ErrBufferFull {
			line = append(line, chunk...)
			continue
		}
		if len(chunk) > 0 {
			line = append(line, chunk...)
		}
		if err != nil {
			if err == io.EOF && len(bytes.TrimSpace(line)) > 0 {
				return line, nil
			}
			return nil, err
		}
		return line, nil
	}
}

// canonicalize returns tools sorted by name with each InputSchema re-encoded
// so object key ordering is deterministic.
func canonicalize(in []rawTool) (ToolList, error) {
	out := make(ToolList, 0, len(in))
	for _, t := range in {
		schema, err := canonicalJSON(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("canonicalize tool %q schema: %w", t.Name, err)
		}
		out = append(out, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// canonicalJSON decodes raw into a generic Go value (which json.Marshal will
// then re-encode with sorted map keys) and returns the result. Empty input
// becomes a JSON null literal so downstream hashing is still defined.
func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return json.RawMessage(out), nil
}

// boundedBuffer accumulates the last `limit` bytes written to it.
type boundedBuffer struct {
	buf   []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *boundedBuffer) tail() string {
	return string(bytes.TrimSpace(b.buf))
}
