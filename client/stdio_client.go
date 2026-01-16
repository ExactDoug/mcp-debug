package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"mcp-debug/config"
)

// StdioClient implements MCPClient using stdio transport
type StdioClient struct {
	serverName string
	command    string
	args       []string
	env        []string
	inheritCfg *config.InheritConfig  // NEW: inheritance configuration

	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	reader   *bufio.Reader
	idGen    *RequestIDGenerator

	connected bool
	mu        sync.Mutex
	requestMu sync.Mutex  // Serialize all I/O operations
}

// NewStdioClient creates a new stdio-based MCP client
func NewStdioClient(serverName, command string, args []string) *StdioClient {
	return &StdioClient{
		serverName: serverName,
		command:    command,
		args:       args,
		idGen:      &RequestIDGenerator{},
	}
}

// SetEnvironment sets environment variables for the server process
func (c *StdioClient) SetEnvironment(env []string) {
	c.env = env
}

// SetInheritConfig sets the inheritance configuration for environment variables
func (c *StdioClient) SetInheritConfig(cfg *config.InheritConfig) {
	c.inheritCfg = cfg
}

// Connect establishes connection to the MCP server
func (c *StdioClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.connected {
		return nil
	}
	
	// Create command
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)
	if c.env != nil || c.inheritCfg != nil {
		// Convert []string env to map[string]string for overrides
		overrides := make(map[string]string)
		if c.env != nil {
			for _, entry := range c.env {
				key, value := splitEnvEntry(entry)
				if key != "" {
					overrides[key] = value
				}
			}
		}

		// Build a minimal ServerConfig with environment overrides and inheritance config
		serverConfig := &config.ServerConfig{
			Env:     overrides,
			Inherit: c.inheritCfg,
		}

		// BuildEnvironment handles defaulting to tier1 if Inherit is nil
		c.cmd.Env = BuildEnvironment(serverConfig, nil)
	}
	// Note: When both c.env and c.inheritCfg are nil, c.cmd.Env stays nil (Go's default)
	
	// Create pipes
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	c.stdin = stdin
	
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	c.stdout = stdout
	c.reader = bufio.NewReader(stdout)
	
	// Start the process
	if err := c.cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to start MCP server: %w", err)
	}
	
	c.connected = true
	return nil
}

// Initialize performs MCP protocol handshake
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}
	
	// Create initialize request
	request := NewInitializeRequest(c.idGen, "dynamic-mcp-proxy", "1.0.0")
	
	// Send request and get response
	response, err := c.sendRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}
	
	// Parse initialize result
	var result InitializeResult
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse initialize response: %w", err)
	}
	
	return &result, nil
}

// ListTools discovers available tools from the server
func (c *StdioClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}
	
	// Create tools/list request
	request := NewListToolsRequest(c.idGen)
	
	// Send request and get response
	response, err := c.sendRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}
	
	// Parse tools list result
	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}
	
	return result.Tools, nil
}

// CallTool invokes a specific tool with arguments
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}
	
	// Create tools/call request
	request := NewCallToolRequest(c.idGen, name, args)
	
	// Send request and get response
	response, err := c.sendRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}
	
	// Parse tool call result
	var result CallToolResult
	if err := ParseResponse(response, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call response: %w", err)
	}
	
	return &result, nil
}

// Close terminates the connection
func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.connected {
		return nil
	}
	
	var errs []error
	
	// Close pipes
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdin: %w", err))
		}
	}
	
	if c.stdout != nil {
		if err := c.stdout.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdout: %w", err))
		}
	}
	
	// Terminate process
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			errs = append(errs, fmt.Errorf("failed to kill process: %w", err))
		}
		
		// Wait for process to exit
		if err := c.cmd.Wait(); err != nil {
			// Process kill is expected to cause exit error, so ignore
		}
	}
	
	c.connected = false
	
	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}
	
	return nil
}

// ServerName returns the configured name of this server
func (c *StdioClient) ServerName() string {
	return c.serverName
}

// IsConnected returns true if the client is currently connected
func (c *StdioClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// sendRequest sends a JSON-RPC request and waits for response
func (c *StdioClient) sendRequest(ctx context.Context, request *JSONRPCRequest) (*JSONRPCResponse, error) {
	// Serialize all I/O operations
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	// Check connected state while holding lock
	if !c.connected {
		return nil, fmt.Errorf("client not connected")
	}

	// Set timeout for the request
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Serialize request
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request - now protected by mutex
	requestLine := append(requestBytes, '\n')
	if _, err := c.stdin.Write(requestLine); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response - now protected by mutex
	responseLine, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse and validate response
	var response JSONRPCResponse
	if err := json.Unmarshal(responseLine, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.ID != request.ID {
		return nil, fmt.Errorf("response ID mismatch: expected %d, got %d", request.ID, response.ID)
	}

	return &response, nil
}