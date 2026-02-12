package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockMCPServer creates a test HTTP server that handles MCP JSON-RPC requests.
// It responds with application/json by default.
func mockMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	sessionID := "test-session-123"
	deleteCalled := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		// Set session ID on all responses
		w.Header().Set("Mcp-Session-Id", sessionID)

		if r.Method == http.MethodDelete {
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "initialize":
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
			}
			result, _ := json.Marshal(map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":   map[string]interface{}{},
				"serverInfo": map[string]interface{}{
					"name":    "test-server",
					"version": "1.0.0",
				},
			})
			resp.Result = result
			json.NewEncoder(w).Encode(resp)

		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)

		case "tools/list":
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
			}
			result, _ := json.Marshal(map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "test_tool",
						"description": "A test tool",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			})
			resp.Result = result
			json.NewEncoder(w).Encode(resp)

		case "tools/call":
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
			}
			result, _ := json.Marshal(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "tool result"},
				},
			})
			resp.Result = result
			json.NewEncoder(w).Encode(resp)

		default:
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32601,
					Message: "Method not found",
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		defer mu.Unlock()
		if !deleteCalled {
			t.Log("Note: DELETE was not called during this test")
		}
	})
	return server
}

func TestHTTPClient_Connect(t *testing.T) {
	t.Run("valid URL", func(t *testing.T) {
		c := NewHTTPClient("test", "http://localhost:8080/mcp")
		err := c.Connect(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !c.IsConnected() {
			t.Error("expected client to be connected")
		}
	})

	t.Run("empty URL", func(t *testing.T) {
		c := NewHTTPClient("test", "")
		err := c.Connect(context.Background())
		if err == nil {
			t.Fatal("expected error for empty URL")
		}
	})

	t.Run("invalid URL scheme", func(t *testing.T) {
		c := NewHTTPClient("test", "ftp://localhost/mcp")
		err := c.Connect(context.Background())
		if err == nil {
			t.Fatal("expected error for invalid URL scheme")
		}
	})

	t.Run("already connected", func(t *testing.T) {
		c := NewHTTPClient("test", "http://localhost:8080/mcp")
		c.Connect(context.Background())
		err := c.Connect(context.Background())
		if err != nil {
			t.Fatalf("second connect should be no-op, got: %v", err)
		}
	})
}

func TestHTTPClient_Initialize(t *testing.T) {
	server := mockMCPServer(t)
	c := NewHTTPClient("test-server", server.URL)

	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	result, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if result.ProtocolVersion != "2025-06-18" {
		t.Errorf("expected protocol version 2025-06-18, got %s", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("expected server name test-server, got %s", result.ServerInfo.Name)
	}
}

func TestHTTPClient_SessionID(t *testing.T) {
	server := mockMCPServer(t)
	c := NewHTTPClient("test-server", server.URL)

	ctx := context.Background()
	c.Connect(ctx)
	c.Initialize(ctx)

	// Session ID should have been extracted from response
	if c.sessionID != "test-session-123" {
		t.Errorf("expected session ID test-session-123, got %s", c.sessionID)
	}
}

func TestHTTPClient_ListTools(t *testing.T) {
	server := mockMCPServer(t)
	c := NewHTTPClient("test-server", server.URL)

	ctx := context.Background()
	c.Connect(ctx)
	c.Initialize(ctx)

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "test_tool" {
		t.Errorf("expected tool name test_tool, got %s", tools[0].Name)
	}
}

func TestHTTPClient_CallTool(t *testing.T) {
	server := mockMCPServer(t)
	c := NewHTTPClient("test-server", server.URL)

	ctx := context.Background()
	c.Connect(ctx)
	c.Initialize(ctx)

	result, err := c.CallTool(ctx, "test_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "tool result" {
		t.Errorf("expected text 'tool result', got %s", result.Content[0].Text)
	}
}

func TestHTTPClient_Close_SendsDelete(t *testing.T) {
	deleteCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "sess-1")
		if r.Method == http.MethodDelete {
			deleteCalled = true
			// Verify session ID is sent with DELETE
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				t.Errorf("DELETE missing session ID header")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// Handle initialize
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		result, _ := json.Marshal(map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities":   map[string]interface{}{},
			"serverInfo":     map[string]interface{}{"name": "test", "version": "1.0"},
		})
		resp.Result = result
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewHTTPClient("test", server.URL)
	ctx := context.Background()
	c.Connect(ctx)
	c.Initialize(ctx)
	c.Close()

	if !deleteCalled {
		t.Error("expected DELETE to be called on Close()")
	}
}

func TestHTTPClient_NotConnected(t *testing.T) {
	c := NewHTTPClient("test", "http://localhost:9999/mcp")

	ctx := context.Background()

	_, err := c.Initialize(ctx)
	if err == nil {
		t.Error("expected error when not connected")
	}

	_, err = c.ListTools(ctx)
	if err == nil {
		t.Error("expected error when not connected")
	}

	_, err = c.CallTool(ctx, "test", nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestHTTPClient_ServerName(t *testing.T) {
	c := NewHTTPClient("my-server", "http://localhost/mcp")
	if c.ServerName() != "my-server" {
		t.Errorf("expected server name my-server, got %s", c.ServerName())
	}
}

func TestHTTPClient_SSEResponse(t *testing.T) {
	// Server that responds with SSE format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "sse-session")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Respond with SSE format
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		result, _ := json.Marshal(map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities":   map[string]interface{}{},
			"serverInfo":     map[string]interface{}{"name": "sse-server", "version": "1.0"},
		})
		resp.Result = result

		data, _ := json.Marshal(resp)
		w.Write([]byte("event: message\ndata: "))
		w.Write(data)
		w.Write([]byte("\n\n"))
	}))
	defer server.Close()

	c := NewHTTPClient("sse-test", server.URL)
	ctx := context.Background()
	c.Connect(ctx)

	result, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize with SSE failed: %v", err)
	}
	if result.ServerInfo.Name != "sse-server" {
		t.Errorf("expected server name sse-server, got %s", result.ServerInfo.Name)
	}
}

func TestHTTPClient_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	c := NewHTTPClient("error-test", server.URL)
	ctx := context.Background()
	c.Connect(ctx)

	_, err := c.Initialize(ctx)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestHTTPClient_Headers(t *testing.T) {
	// Verify correct headers are sent
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && receivedHeaders == nil {
			receivedHeaders = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "h-session")
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		result, _ := json.Marshal(map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities":   map[string]interface{}{},
			"serverInfo":     map[string]interface{}{"name": "h", "version": "1.0"},
		})
		resp.Result = result
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewHTTPClient("header-test", server.URL)
	ctx := context.Background()
	c.Connect(ctx)
	c.Initialize(ctx)

	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedHeaders.Get("Content-Type"))
	}
	if receivedHeaders.Get("Accept") != "application/json, text/event-stream" {
		t.Errorf("expected Accept header, got %s", receivedHeaders.Get("Accept"))
	}
	if receivedHeaders.Get("MCP-Protocol-Version") != "2025-06-18" {
		t.Errorf("expected MCP-Protocol-Version 2025-06-18, got %s", receivedHeaders.Get("MCP-Protocol-Version"))
	}
}
