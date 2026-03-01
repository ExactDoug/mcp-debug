package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPClient implements MCPClient using Streamable HTTP transport.
// It connects to MCP servers over HTTP POST with JSON-RPC payloads,
// handling both application/json and text/event-stream responses.
type HTTPClient struct {
	serverName   string
	url          string
	httpClient   *http.Client
	sessionID    string // Mcp-Session-Id from server
	idGen        *RequestIDGenerator
	authProvider AuthProvider // nil until Phase 2+
	connected    bool
	mu           sync.Mutex
	requestMu    sync.Mutex // Serialize requests (matches StdioClient pattern)
}

// NewHTTPClient creates a new HTTP-stream MCP client.
func NewHTTPClient(serverName, url string) *HTTPClient {
	return &HTTPClient{
		serverName: serverName,
		url:        url,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		idGen: &RequestIDGenerator{},
	}
}

// SetTimeout sets the HTTP client timeout.
func (c *HTTPClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// SetAuthProvider sets the authentication provider for requests.
func (c *HTTPClient) SetAuthProvider(provider AuthProvider) {
	c.authProvider = provider
}

// Connect validates the URL and marks the client as connected.
// Unlike StdioClient (which spawns a process), HTTP connection is lightweight —
// the real connection happens on the first request.
func (c *HTTPClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Validate URL
	if c.url == "" {
		return fmt.Errorf("HTTP client URL is empty")
	}
	if !strings.HasPrefix(c.url, "http://") && !strings.HasPrefix(c.url, "https://") {
		return fmt.Errorf("HTTP client URL must start with http:// or https://: %s", c.url)
	}

	c.connected = true
	log.Printf("[DEBUG] HTTPClient.Connect() SUCCESS: %s → %s", c.serverName, c.url)
	return nil
}

// Initialize performs MCP protocol handshake over HTTP.
// Sends initialize request, extracts Mcp-Session-Id, then sends initialized notification.
func (c *HTTPClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	if !connected {
		return nil, fmt.Errorf("client not connected")
	}

	// Create initialize request
	request := NewInitializeRequest(c.idGen, "mcp-debug", "1.0.0")

	// Send request
	response, err := c.sendHTTPRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	// Parse initialize result
	var result InitializeResult
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse initialize response: %w", err)
	}

	// Send notifications/initialized (fire-and-forget per MCP spec)
	c.sendInitializedNotification(ctx)

	return &result, nil
}

// ListTools discovers available tools from the server.
func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	if !connected {
		return nil, fmt.Errorf("client not connected")
	}

	request := NewListToolsRequest(c.idGen)

	response, err := c.sendHTTPRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	return result.Tools, nil
}

// CallTool invokes a specific tool with arguments.
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	log.Printf("[DEBUG] HTTPClient.CallTool(%s, %s): connected=%v", c.serverName, name, connected)

	if !connected {
		return nil, fmt.Errorf("client not connected")
	}

	request := NewCallToolRequest(c.idGen, name, args)

	response, err := c.sendHTTPRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}

	var result CallToolResult
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call response: %w", err)
	}

	return &result, nil
}

// Close terminates the HTTP session.
// Sends HTTP DELETE with Mcp-Session-Id to notify the server.
func (c *HTTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	// Send DELETE to terminate session (best-effort)
	if c.sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url, nil)
		if err == nil {
			req.Header.Set("Mcp-Session-Id", c.sessionID)
			if c.authProvider != nil {
				c.authProvider.ApplyAuth(req)
			}
			resp, err := c.httpClient.Do(req)
			if err != nil {
				log.Printf("[DEBUG] HTTPClient.Close(): DELETE request failed for %s: %v", c.serverName, err)
			} else {
				resp.Body.Close()
			}
		}
	}

	c.connected = false
	c.sessionID = ""
	log.Printf("[DEBUG] HTTPClient.Close(): %s - connected=%v", c.serverName, c.connected)
	return nil
}

// ServerName returns the configured name of this server.
func (c *HTTPClient) ServerName() string {
	return c.serverName
}

// GetAuthProvider returns the current auth provider (may be nil).
func (c *HTTPClient) GetAuthProvider() AuthProvider {
	return c.authProvider
}

// IsConnected returns true if the client is currently connected.
func (c *HTTPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// sendHTTPRequest sends a JSON-RPC request over HTTP and returns the response.
// Handles both application/json and text/event-stream response content types.
func (c *HTTPClient) sendHTTPRequest(ctx context.Context, request *JSONRPCRequest) (*JSONRPCResponse, error) {
	c.mu.Lock()
	connected := c.connected
	c.mu.Unlock()

	if !connected {
		return nil, fmt.Errorf("client not connected")
	}

	// Serialize requests
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set required headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("MCP-Protocol-Version", "2025-06-18")

	// Set session ID if we have one
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	// Apply authentication if configured
	if c.authProvider != nil {
		if err := c.authProvider.ApplyAuth(httpReq); err != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", err)
		}
	}

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Extract session ID from response headers
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	// Handle 401 Unauthorized — try token refresh and retry once
	if resp.StatusCode == http.StatusUnauthorized {
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()

		if refresher, ok := c.authProvider.(TokenRefresher); ok {
			log.Printf("[DEBUG] HTTPClient: received 401, attempting token refresh for %s", c.serverName)
			if err := refresher.RefreshToken(ctx, wwwAuth); err != nil {
				return nil, fmt.Errorf("authentication failed for %s: %w", c.serverName, err)
			}
			// Retry the request once with new token
			return c.retrySendHTTPRequest(ctx, request)
		}

		return nil, fmt.Errorf("HTTP 401 Unauthorized for %s (no token refresher configured)", c.serverName)
	}

	// Check for other error status codes
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Handle 202 Accepted (used for notifications) — no response body expected
	if resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	// Parse response based on Content-Type
	contentType := resp.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "text/event-stream") {
		// SSE response — parse the stream for our response
		return ParseSSEResponse(resp.Body, request.ID)
	}

	// Default: application/json response
	var response JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if response.ID != request.ID {
		return nil, fmt.Errorf("response ID mismatch: expected %d, got %d", request.ID, response.ID)
	}

	return &response, nil
}

// retrySendHTTPRequest retries a request after token refresh.
// This does NOT hold requestMu since it's called from sendHTTPRequest which already holds it.
func (c *HTTPClient) retrySendHTTPRequest(ctx context.Context, request *JSONRPCRequest) (*JSONRPCResponse, error) {
	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set required headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("MCP-Protocol-Version", "2025-06-18")

	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	// Apply refreshed authentication
	if c.authProvider != nil {
		if err := c.authProvider.ApplyAuth(httpReq); err != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", err)
		}
	}

	// Execute retry request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP retry request failed: %w", err)
	}
	defer resp.Body.Close()

	// Extract session ID
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	// Check for errors (no more retries)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d after retry: %s", resp.StatusCode, string(bodyBytes))
	}

	if resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	// Parse response
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return ParseSSEResponse(resp.Body, request.ID)
	}

	var response JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if response.ID != request.ID {
		return nil, fmt.Errorf("response ID mismatch: expected %d, got %d", request.ID, response.ID)
	}

	return &response, nil
}

// sendInitializedNotification sends the notifications/initialized message.
// This is fire-and-forget per the MCP spec — the server responds with 202 Accepted.
func (c *HTTPClient) sendInitializedNotification(ctx context.Context) {
	notification := &JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	// Marshal notification (no ID field for notifications)
	body, err := json.Marshal(notification)
	if err != nil {
		log.Printf("[DEBUG] HTTPClient: failed to marshal initialized notification: %v", err)
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[DEBUG] HTTPClient: failed to create initialized notification request: %v", err)
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	if c.authProvider != nil {
		c.authProvider.ApplyAuth(httpReq)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[DEBUG] HTTPClient: initialized notification failed: %v", err)
		return
	}
	resp.Body.Close()
}
