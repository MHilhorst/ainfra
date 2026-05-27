package mcpclient

import "encoding/json"

// protocolVersion is the MCP protocol revision this client negotiates.
const protocolVersion = "2024-11-05"

// rpcRequest is a JSON-RPC 2.0 request frame.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcNotification is a JSON-RPC 2.0 notification (no id, no response).
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response frame. Either Result or Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) String() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// initializeParams is the minimal initialize params payload.
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// toolsListResult is the result payload of the tools/list response.
type toolsListResult struct {
	Tools []rawTool `json:"tools"`
}

// rawTool is the on-wire shape of a tool entry inside tools/list.
type rawTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
