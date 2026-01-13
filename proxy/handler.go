package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"mcp-debug/client"
	"mcp-debug/discovery"
)

// RecorderFunc is a function that records JSON-RPC messages with metadata
type RecorderFunc func(direction, messageType, toolName, serverName string, message interface{})

// CreateProxyHandler creates a handler that forwards tool calls to remote servers
// The optional recorder function enables recording of tool call traffic
func CreateProxyHandler(mcpClient client.MCPClient, remoteTool discovery.RemoteTool, recorder RecorderFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Record the request if recorder is provided
		if recorder != nil {
			recorder("request", "tool_call", remoteTool.PrefixedName, remoteTool.ServerName, request)
		}
		// Extract arguments from the request
		args, err := extractArguments(request)
		if err != nil {
			errResult := mcp.NewToolResultError(fmt.Sprintf("Failed to extract arguments: %v", err))
			if recorder != nil {
				recorder("response", "tool_call", remoteTool.PrefixedName, remoteTool.ServerName, errResult)
			}
			return errResult, nil
		}

		// Forward the call to the remote server using the original tool name
		result, err := mcpClient.CallTool(ctx, remoteTool.OriginalName, args)
		if err != nil {
			// Wrap error with server context
			errorMsg := fmt.Sprintf("[%s] %v", remoteTool.ServerName, err)
			errResult := mcp.NewToolResultError(errorMsg)
			if recorder != nil {
				recorder("response", "tool_call", remoteTool.PrefixedName, remoteTool.ServerName, errResult)
			}
			return errResult, nil
		}
		
		// Transform the result back to MCP format
		mcpResult := transformResult(result)

		// Record the response if recorder is provided
		if recorder != nil {
			recorder("response", "tool_call", remoteTool.PrefixedName, remoteTool.ServerName, mcpResult)
		}

		return mcpResult, nil
	}
}

// extractArguments extracts arguments from a CallToolRequest
func extractArguments(request mcp.CallToolRequest) (map[string]interface{}, error) {
	// Use the GetArguments method to get all arguments as a map
	args := request.GetArguments()
	
	// The GetArguments method returns map[string]any, which is compatible with map[string]interface{}
	// Convert the map to ensure compatibility
	result := make(map[string]interface{})
	for key, value := range args {
		result[key] = value
	}
	
	return result, nil
}

// transformResult transforms a client.CallToolResult to mcp.CallToolResult
func transformResult(clientResult *client.CallToolResult) *mcp.CallToolResult {
	if clientResult.IsError {
		// If the client result indicates an error, create an error result
		if len(clientResult.Content) > 0 {
			return mcp.NewToolResultError(clientResult.Content[0].Text)
		}
		return mcp.NewToolResultError("Tool execution failed")
	}
	
	// For successful results, convert content to text
	if len(clientResult.Content) > 0 {
		// For now, combine all text content
		var text string
		for i, content := range clientResult.Content {
			if i > 0 {
				text += "\n"
			}
			text += content.Text
		}
		return mcp.NewToolResultText(text)
	}
	
	return mcp.NewToolResultText("Tool executed successfully")
}

// ToolRegistry manages the mapping of tools to their handlers and clients
type ToolRegistry struct {
	tools   map[string]discovery.RemoteTool
	clients map[string]client.MCPClient
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:   make(map[string]discovery.RemoteTool),
		clients: make(map[string]client.MCPClient),
	}
}

// RegisterTool registers a tool with its associated client
func (r *ToolRegistry) RegisterTool(tool discovery.RemoteTool, mcpClient client.MCPClient) {
	r.tools[tool.PrefixedName] = tool
	r.clients[tool.ServerName] = mcpClient
}

// GetTool returns the tool metadata for a prefixed tool name
func (r *ToolRegistry) GetTool(prefixedName string) (discovery.RemoteTool, bool) {
	tool, exists := r.tools[prefixedName]
	return tool, exists
}

// GetClient returns the MCP client for a server name
func (r *ToolRegistry) GetClient(serverName string) (client.MCPClient, bool) {
	client, exists := r.clients[serverName]
	return client, exists
}

// GetAllTools returns all registered tools
func (r *ToolRegistry) GetAllTools() []discovery.RemoteTool {
	var tools []discovery.RemoteTool
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// CreateHandlerForTool creates a proxy handler for a specific tool
func (r *ToolRegistry) CreateHandlerForTool(prefixedToolName string, recorder RecorderFunc) (server.ToolHandlerFunc, error) {
	// Get tool metadata
	tool, exists := r.GetTool(prefixedToolName)
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", prefixedToolName)
	}

	// Get associated client
	mcpClient, exists := r.GetClient(tool.ServerName)
	if !exists {
		return nil, fmt.Errorf("client not found for server: %s", tool.ServerName)
	}

	// Create and return the handler with optional recorder
	return CreateProxyHandler(mcpClient, tool, recorder), nil
}