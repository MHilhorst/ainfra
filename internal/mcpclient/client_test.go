package mcpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func rawMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// twoToolResponse is the canonical happy-path tools/list payload used by
// several test cases.
func twoToolResponse() json.RawMessage {
	tools := []map[string]any{
		{
			"name":        "beta",
			"description": "B tool",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		},
		{
			"name":        "alpha",
			"description": "A tool",
			"inputSchema": map[string]any{"type": "object"},
		},
	}
	out, _ := json.Marshal(map[string]any{"tools": tools})
	return out
}

func newOkRunner(toolsResult json.RawMessage) *FakeRunner {
	return &FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": toolsResult,
		},
	}
}

func introspect(t *testing.T, runner Runner, req Request) (ToolList, error) {
	t.Helper()
	req.Runner = runner
	if req.Command == "" {
		req.Command = "fake-server"
	}
	if req.Timeout == 0 {
		req.Timeout = 2 * time.Second
	}
	return Introspect(context.Background(), req)
}

func TestIntrospectHappyPath(t *testing.T) {
	tools, err := introspect(t, newOkRunner(twoToolResponse()), Request{})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if got, want := len(tools), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if tools[0].Name != "alpha" || tools[1].Name != "beta" {
		t.Errorf("tools not sorted by name: %+v", tools)
	}
	if tools[0].Description != "A tool" {
		t.Errorf("description not preserved: %q", tools[0].Description)
	}
	// InputSchema is non-empty and valid JSON.
	var v any
	if err := json.Unmarshal(tools[0].InputSchema, &v); err != nil {
		t.Errorf("inputSchema not valid JSON: %v", err)
	}
}

func TestIntrospectSingleTool(t *testing.T) {
	result := rawMarshal(t, map[string]any{
		"tools": []map[string]any{
			{
				"name":        "only",
				"description": "single",
				"inputSchema": map[string]any{"type": "object"},
			},
		},
	})
	tools, err := introspect(t, newOkRunner(result), Request{})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "only" {
		t.Errorf("unexpected tools: %+v", tools)
	}
}

func TestIntrospectEmptyToolset(t *testing.T) {
	result := rawMarshal(t, map[string]any{"tools": []any{}})
	tools, err := introspect(t, newOkRunner(result), Request{})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if tools == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(tools) != 0 {
		t.Errorf("expected empty, got %d entries", len(tools))
	}
}

func TestIntrospectSortStability(t *testing.T) {
	forward := twoToolResponse()
	// Build the same payload but with tools in reverse order.
	var parsed map[string][]map[string]any
	if err := json.Unmarshal(forward, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed["tools"][0], parsed["tools"][1] = parsed["tools"][1], parsed["tools"][0]
	reversed := rawMarshal(t, parsed)

	a, err := introspect(t, newOkRunner(forward), Request{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := introspect(t, newOkRunner(reversed), Request{})
	if err != nil {
		t.Fatal(err)
	}

	aBytes, _ := json.Marshal(a)
	bBytes, _ := json.Marshal(b)
	if !bytes.Equal(aBytes, bBytes) {
		t.Errorf("canonical output differs under reordering:\n a=%s\n b=%s", aBytes, bBytes)
	}
}

func TestInputSchemaCanonicalization(t *testing.T) {
	// Same logical schema, different key order.
	a := rawMarshal(t, map[string]any{
		"tools": []map[string]any{
			{"name": "t", "description": "d", "inputSchema": map[string]any{"type": "object", "title": "X"}},
		},
	})
	b := rawMarshal(t, map[string]any{
		"tools": []map[string]any{
			{"name": "t", "description": "d", "inputSchema": map[string]any{"title": "X", "type": "object"}},
		},
	})

	ta, err := introspect(t, newOkRunner(a), Request{})
	if err != nil {
		t.Fatal(err)
	}
	tb, err := introspect(t, newOkRunner(b), Request{})
	if err != nil {
		t.Fatal(err)
	}

	hashA := sha256.Sum256(ta[0].InputSchema)
	hashB := sha256.Sum256(tb[0].InputSchema)
	if hex.EncodeToString(hashA[:]) != hex.EncodeToString(hashB[:]) {
		t.Errorf("canonical InputSchema differs across key orders:\n a=%s\n b=%s", ta[0].InputSchema, tb[0].InputSchema)
	}
}

func TestIntrospectTimeoutKillsSubprocess(t *testing.T) {
	r := newOkRunner(twoToolResponse())
	r.ResponseDelay = 200 * time.Millisecond

	_, err := introspect(t, r, Request{Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Errorf("err is not *TimeoutError: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected errors.Is(err, context.DeadlineExceeded), got %v", err)
	}
	proc := r.LastProcess()
	if proc == nil {
		t.Fatal("no fake process recorded")
	}
	if !proc.Killed() {
		t.Error("subprocess Kill() was not called on timeout")
	}
}

func TestIntrospectProtocolErrorOnInitialize(t *testing.T) {
	r := &FakeRunner{
		Errors: map[string]*rpcError{
			"initialize": {Code: -32603, Message: "boom"},
		},
	}
	_, err := introspect(t, r, Request{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "initialize rpc error") {
		t.Errorf("error missing initialize context: %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error missing server message: %v", err)
	}
}

func TestIntrospectSubprocessExitsEarly(t *testing.T) {
	r := &FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
		},
		ExitEarly:     true,
		StderrOnStart: "fatal: out of memory\n",
	}
	_, err := introspect(t, r, Request{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fatal: out of memory") {
		t.Errorf("error missing stderr tail: %v", err)
	}
}

func TestIntrospectMalformedJSONResponse(t *testing.T) {
	r := &FakeRunner{
		Responses: map[string]json.RawMessage{
			// `result` will be malformed for tools/list — the framing is
			// valid JSON but the tools/list result content can't decode as
			// {tools: [...]}.
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": json.RawMessage(`"not-an-object"`),
		},
	}
	_, err := introspect(t, r, Request{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode tools/list result") {
		t.Errorf("error not wrapped as decode failure: %v", err)
	}
}

func TestIntrospectRestrictedEnv(t *testing.T) {
	r := newOkRunner(twoToolResponse())
	declared := map[string]string{"GITHUB_TOKEN": "tkn", "FOO": "bar"}
	if _, err := introspect(t, r, Request{Env: declared}); err != nil {
		t.Fatal(err)
	}

	got := r.LastEnv()
	allowed := map[string]bool{"PATH": true, "HOME": true, "GITHUB_TOKEN": true, "FOO": true}
	for _, kv := range got {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			t.Errorf("malformed env entry %q", kv)
			continue
		}
		key := kv[:eq]
		if !allowed[key] {
			t.Errorf("env contains disallowed key %q (full=%q)", key, kv)
		}
	}

	// Spot-check the declared keys made it through.
	want := map[string]string{"GITHUB_TOKEN=tkn": "", "FOO=bar": ""}
	for _, kv := range got {
		delete(want, kv)
	}
	for missing := range want {
		t.Errorf("declared env not propagated: %s", missing)
	}
}

func TestIntrospectEmptyCommandRejected(t *testing.T) {
	_, err := Introspect(context.Background(), Request{Runner: newOkRunner(twoToolResponse())})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "command is empty") {
		t.Errorf("error did not mention empty command: %v", err)
	}
}

func TestIntrospectContextCancellation(t *testing.T) {
	r := newOkRunner(twoToolResponse())
	r.ResponseDelay = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := Introspect(ctx, Request{Command: "fake", Runner: r, Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
	if p := r.LastProcess(); p == nil || !p.Killed() {
		t.Error("subprocess was not killed on context cancellation")
	}
}

// Verify buildEnv directly: it must inherit only PATH/HOME and propagate the
// declared keys, with no other host env leaking through.
func TestBuildEnvRestricted(t *testing.T) {
	t.Setenv("PATH", "/bin")
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("SHOULD_NOT_LEAK", "leak")

	got := buildEnv(map[string]string{"FOO": "bar"})
	want := map[string]string{"PATH": "/bin", "HOME": "/tmp/home", "FOO": "bar"}
	if len(got) != len(want) {
		t.Errorf("env length got=%d want=%d (%v)", len(got), len(want), got)
	}
	for _, kv := range got {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			t.Errorf("malformed entry %q", kv)
			continue
		}
		key := kv[:eq]
		val := kv[eq+1:]
		if w, ok := want[key]; !ok {
			t.Errorf("unexpected env key %q", key)
		} else if val != w {
			t.Errorf("env %s=%q, want %q", key, val, w)
		}
	}
}

// Verify the FakeRunner gracefully reports start failures.
func TestIntrospectStartFailure(t *testing.T) {
	r := &FakeRunner{StartErr: io.ErrUnexpectedEOF}
	_, err := introspect(t, r, Request{})
	if err == nil {
		t.Fatal("expected start error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected wrap of io.ErrUnexpectedEOF, got %v", err)
	}
	if !strings.Contains(err.Error(), "mcpclient: start") {
		t.Errorf("error not wrapped with op tag: %v", err)
	}
}
