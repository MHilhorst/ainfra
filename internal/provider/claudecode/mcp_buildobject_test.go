package claudecode

import (
	"encoding/json"
	"strings"
	"testing"
)

// render.go declares envMap/headersMap as `var ... map[string]string` and stores
// them into the payload even when no env/headers are set. Those typed-nil maps
// must not surface as `"env": null` / `"headers": null` in .mcp.json, which
// Claude Code rejects for stdio servers ("env: expected record, received null").
func TestBuildMCPServerObject_OmitsTypedNilFields(t *testing.T) {
	var nilEnv map[string]string
	var nilHeaders map[string]string

	payload := map[string]any{
		"command":   "npx",
		"args":      []string{"-y", "chrome-devtools-mcp@1.0.1"},
		"env":       nilEnv,
		"transport": "stdio",
		"url":       "",
		"headers":   nilHeaders,
	}

	obj := buildMCPServerObject(payload)

	if _, ok := obj["env"]; ok {
		t.Errorf("env should be omitted for a typed-nil map, got %#v", obj["env"])
	}
	if _, ok := obj["headers"]; ok {
		t.Errorf("headers should be omitted for a typed-nil map, got %#v", obj["headers"])
	}

	encoded, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "null") {
		t.Errorf(".mcp.json entry must not contain null fields, got %s", encoded)
	}
}
