package client

import (
	"encoding/json"
	"sync/atomic"
)

// JSON-RPC 2.0 protocol structures

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int64       `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP-specific request parameters

// InitializeParams represents parameters for the initialize request
type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

// ClientInfo represents client information
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// CallToolParams represents parameters for tool invocation
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// RequestIDGenerator generates unique request IDs
type RequestIDGenerator struct {
	counter int64
}

// NextID returns the next unique request ID
func (g *RequestIDGenerator) NextID() int64 {
	return atomic.AddInt64(&g.counter, 1)
}

// NewInitializeRequest creates a new initialize request
func NewInitializeRequest(idGen *RequestIDGenerator, clientName, clientVersion string) *JSONRPCRequest {
	return &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: "2025-06-18",
			Capabilities: map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			ClientInfo: ClientInfo{
				Name:    clientName,
				Version: clientVersion,
			},
		},
		ID: idGen.NextID(),
	}
}

// NewListToolsRequest creates a new tools/list request
func NewListToolsRequest(idGen *RequestIDGenerator) *JSONRPCRequest {
	return &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      idGen.NextID(),
	}
}

// NewCallToolRequest creates a new tools/call request
func NewCallToolRequest(idGen *RequestIDGenerator, toolName string, args map[string]interface{}) *JSONRPCRequest {
	return &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
		ID: idGen.NextID(),
	}
}

// ParseResponse parses a JSON-RPC response and returns typed result
func ParseResponse(response *JSONRPCResponse, result interface{}) error {
	if response.Error != nil {
		return &ClientError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
		}
	}
	
	return json.Unmarshal(response.Result, result)
}