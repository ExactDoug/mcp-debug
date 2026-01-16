package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"mcp-debug/client"
	"mcp-debug/config"
	"mcp-debug/discovery"
)

// DynamicWrapper provides dynamic server management for mark3labs/mcp-go
type DynamicWrapper struct {
	baseServer    *server.MCPServer
	proxyServer   *ProxyServer
	dynamicServers map[string]*DynamicServerInfo
	mu            sync.RWMutex
	
	// Recording functionality
	recordFile     *os.File
	recordEnabled  bool
	recordMu       sync.Mutex
	recordFilename string // Path to the recording file (for metadata)
}

type DynamicServerInfo struct {
	Name         string
	Client       client.MCPClient
	Tools        []string
	Config       config.ServerConfig
	IsConnected  bool
	ErrorMessage string
}

// RecordedMessage represents a JSON-RPC message with metadata
type RecordedMessage struct {
	Timestamp   time.Time       `json:"timestamp"`
	Direction   string          `json:"direction"` // "request" or "response"
	MessageType string          `json:"message_type"` // "tool_call", "initialize", etc.
	ToolName    string          `json:"tool_name,omitempty"`
	ServerName  string          `json:"server_name,omitempty"`
	Message     json.RawMessage `json:"message"`
}

// RecordingSession represents a complete recording session
type RecordingSession struct {
	StartTime   time.Time         `json:"start_time"`
	ServerInfo  string            `json:"server_info"`
	Messages    []RecordedMessage `json:"messages"`
}

// NewDynamicWrapper creates a wrapper that adds dynamic capabilities
func NewDynamicWrapper(cfg *config.ProxyConfig) *DynamicWrapper {
	// Create base MCP server with management tools
	baseServer := server.NewMCPServer(
		"Dynamic MCP Proxy",
		"1.0.0",
		server.WithToolCapabilities(true),
	)
	
	// Create proxy server
	proxyServer := NewProxyServer(cfg)
	proxyServer.mcpServer = baseServer
	
	wrapper := &DynamicWrapper{
		baseServer:     baseServer,
		proxyServer:    proxyServer,
		dynamicServers: make(map[string]*DynamicServerInfo),
	}
	
	// Register management tools
	wrapper.registerManagementTools()
	
	return wrapper
}

// EnableRecording starts recording JSON-RPC traffic to the specified file
func (w *DynamicWrapper) EnableRecording(filename string) error {
	w.recordMu.Lock()
	defer w.recordMu.Unlock()


	if w.recordEnabled {
		return fmt.Errorf("recording already enabled")
	}
	
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create recording file: %w", err)
	}

	w.recordFile = file
	w.recordFilename = filename
	w.recordEnabled = true

	// Write session header
	session := RecordingSession{
		StartTime:  time.Now(),
		ServerInfo: "Dynamic MCP Proxy v1.0.0",
		Messages:   []RecordedMessage{},
	}

	headerBytes, _ := json.Marshal(session)
	fmt.Fprintf(file, "# MCP Recording Session\n# Started: %s\n%s\n",
		session.StartTime.Format(time.RFC3339), string(headerBytes))

	// Inject recorder and metadata function into proxy server for static server recording
	w.proxyServer.recorderFunc = w.recordMessage
	w.proxyServer.metadataFunc = w.addRecordingMetadata

	log.Printf("Recording enabled to: %s", filename)
	return nil
}

// recordMessage records a JSON-RPC message with metadata
func (w *DynamicWrapper) recordMessage(direction, messageType, toolName, serverName string, message interface{}) {
	if !w.recordEnabled {
		return
	}
	
	w.recordMu.Lock()
	defer w.recordMu.Unlock()
	
	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal message for recording: %v", err)
		return
	}
	
	recorded := RecordedMessage{
		Timestamp:   time.Now(),
		Direction:   direction,
		MessageType: messageType,
		ToolName:    toolName,
		ServerName:  serverName,
		Message:     json.RawMessage(messageBytes),
	}
	
	recordedBytes, err := json.Marshal(recorded)
	if err != nil {
		log.Printf("Failed to marshal recorded message: %v", err)
		return
	}
	
	fmt.Fprintf(w.recordFile, "%s\n", string(recordedBytes))
	w.recordFile.Sync() // Ensure immediate write
}

// addRecordingMetadata adds recording file information to tool results when recording is active
func (w *DynamicWrapper) addRecordingMetadata(result *mcp.CallToolResult) *mcp.CallToolResult {
	if !w.recordEnabled {
		return result
	}

	w.recordMu.Lock()
	filename := w.recordFilename
	w.recordMu.Unlock()

	if filename == "" {
		return result
	}

	// Compute absolute path
	absPath, err := filepath.Abs(filename)
	if err != nil {
		absPath = filename // fallback to original if abs fails
	}

	// Build metadata text
	metadataText := fmt.Sprintf(
		"📹 Recording: %s\n   Full path: %s\n   Purpose: JSON-RPC message log for debugging and playback testing",
		filename,
		absPath,
	)

	// Create metadata content item
	metadataItem := mcp.NewTextContent(metadataText)

	// Copy-on-write to avoid mutating input
	newResult := &mcp.CallToolResult{
		Content: make([]mcp.Content, len(result.Content), len(result.Content)+1),
		IsError: result.IsError,
	}

	// Copy existing content
	copy(newResult.Content, result.Content)

	// Append metadata to NEW slice
	newResult.Content = append(newResult.Content, metadataItem)

	return newResult
}

func (w *DynamicWrapper) registerManagementTools() {
	// server_add tool
	addTool := mcp.NewTool("server_add",
		mcp.WithDescription("Add a new MCP server to the proxy dynamically"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name/prefix for the server"),
		),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Command to run (e.g., 'npx -y @modelcontextprotocol/filesystem /path')"),
		),
	)
	
	w.baseServer.AddTool(addTool, w.handleServerAdd)
	
	// server_remove tool
	removeTool := mcp.NewTool("server_remove",
		mcp.WithDescription("Remove an MCP server from the proxy"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the server to remove"),
		),
	)
	
	w.baseServer.AddTool(removeTool, w.handleServerRemove)
	
	// server_list tool
	listTool := mcp.NewTool("server_list",
		mcp.WithDescription("List all connected MCP servers"),
	)
	
	w.baseServer.AddTool(listTool, w.handleServerList)
	
	// server_disconnect tool
	disconnectTool := mcp.NewTool("server_disconnect",
		mcp.WithDescription("Disconnect a server (tools remain but return errors)"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the server to disconnect"),
		),
	)
	
	w.baseServer.AddTool(disconnectTool, w.handleServerDisconnect)
	
	// server_reconnect tool
	reconnectTool := mcp.NewTool("server_reconnect",
		mcp.WithDescription("Reconnect a server with optional new command (use after server_disconnect)"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the server to reconnect"),
		),
		mcp.WithString("command",
			mcp.Description("New command to run. If omitted, uses stored configuration from config.yaml."),
		),
	)
	
	w.baseServer.AddTool(reconnectTool, w.handleServerReconnect)
}

func (w *DynamicWrapper) handleServerAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record the request
	w.recordMessage("request", "tool_call", "server_add", "proxy", request)
	
	name, err := request.RequireString("name")
	if err != nil {
		result := mcp.NewToolResultError("name is required")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}

	command, err := request.RequireString("command")
	if err != nil {
		result := mcp.NewToolResultError("command is required")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}
	
	w.mu.Lock()
	defer w.mu.Unlock()
	
	// Check if already exists
	if _, exists := w.dynamicServers[name]; exists {
		result := mcp.NewToolResultError(fmt.Sprintf("Server '%s' already exists", name))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}

	// Parse command
	parts := strings.Fields(command)
	if len(parts) == 0 {
		result := mcp.NewToolResultError("Invalid command")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}
	
	// Create server config
	serverConfig := config.ServerConfig{
		Name:      name,
		Prefix:    name,
		Transport: "stdio",
		Command:   parts[0],
		Args:      parts[1:],
		Timeout:   "30s",
	}
	
	// Create and connect client
	stdioClient := client.NewStdioClient(name, serverConfig.Command, serverConfig.Args)

	// Use default inheritance (tier1 or proxy defaults)
	inheritCfg := serverConfig.ResolveInheritConfig(w.proxyServer.config.Inherit)
	stdioClient.SetInheritConfig(inheritCfg)

	if err := stdioClient.Connect(ctx); err != nil {
		result := mcp.NewToolResultError(fmt.Sprintf("Failed to connect: %v", err))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}

	if _, err := stdioClient.Initialize(ctx); err != nil {
		stdioClient.Close()
		result := mcp.NewToolResultError(fmt.Sprintf("Failed to initialize: %v", err))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}

	// List tools
	tools, err := stdioClient.ListTools(ctx)
	if err != nil {
		stdioClient.Close()
		result := mcp.NewToolResultError(fmt.Sprintf("Failed to list tools: %v", err))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_add", "proxy", result)
		return result, nil
	}
	
	// Store server info
	serverInfo := &DynamicServerInfo{
		Name:        name,
		Client:      stdioClient,
		Config:      serverConfig,
		Tools:       make([]string, 0, len(tools)),
		IsConnected: true,
	}
	
	// Register tools with proxy
	registeredCount := 0
	for _, tool := range tools {
		// Create discovered tool
		discoveredTool := discovery.RemoteTool{
			OriginalName: tool.Name,
			PrefixedName: fmt.Sprintf("%s_%s", name, tool.Name),
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			ServerName:   name,
		}
		
		// Register with proxy registry
		w.proxyServer.registry.RegisterTool(discoveredTool, stdioClient)
		
		// Create MCP tool
		mcpTool := w.proxyServer.createMCPTool(discoveredTool)
		
		// Create proxy handler with disconnect checking
		handler := w.createDynamicProxyHandler(name, discoveredTool.OriginalName)
		
		// Add to MCP server
		w.baseServer.AddTool(mcpTool, handler)
		
		serverInfo.Tools = append(serverInfo.Tools, discoveredTool.PrefixedName)
		registeredCount++
		log.Printf("Dynamically registered tool: %s", discoveredTool.PrefixedName)
	}
	
	// Store server info
	w.dynamicServers[name] = serverInfo
	
	// Also add to proxy server's client list
	w.proxyServer.clients = append(w.proxyServer.clients, stdioClient)
	
	result := fmt.Sprintf("Added server '%s' with command: %s %s\nRegistered %d tools successfully.",
		name, serverConfig.Command, strings.Join(serverConfig.Args, " "), registeredCount)

	toolResult := mcp.NewToolResultText(result)
	toolResult = w.addRecordingMetadata(toolResult)
	w.recordMessage("response", "tool_call", "server_add", "proxy", toolResult)
	return toolResult, nil
}

func (w *DynamicWrapper) handleServerRemove(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record the request
	w.recordMessage("request", "tool_call", "server_remove", "proxy", request)

	name, err := request.RequireString("name")
	if err != nil {
		result := mcp.NewToolResultError("name is required")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_remove", "proxy", result)
		return result, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	serverInfo, exists := w.dynamicServers[name]
	if !exists {
		result := mcp.NewToolResultError(fmt.Sprintf("Server '%s' not found", name))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_remove", "proxy", result)
		return result, nil
	}
	
	// Note: We can't actually remove tools from mark3labs/mcp-go at runtime
	// But we can close the connection and mark them as unavailable
	
	// Close client
	if err := serverInfo.Client.Close(); err != nil {
		log.Printf("Error closing client %s: %v", name, err)
	}
	
	// Remove from maps
	delete(w.dynamicServers, name)
	
	// Remove from proxy server's client list
	newClients := make([]client.MCPClient, 0, len(w.proxyServer.clients)-1)
	for _, c := range w.proxyServer.clients {
		if c != serverInfo.Client {
			newClients = append(newClients, c)
		}
	}
	w.proxyServer.clients = newClients
	
	result := fmt.Sprintf("Removed server '%s'. Note: %d tools remain registered but are now unavailable.",
		name, len(serverInfo.Tools))

	toolResult := mcp.NewToolResultText(result)
	toolResult = w.addRecordingMetadata(toolResult)
	w.recordMessage("response", "tool_call", "server_remove", "proxy", toolResult)
	return toolResult, nil
}

func (w *DynamicWrapper) handleServerList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record the request
	w.recordMessage("request", "tool_call", "server_list", "proxy", request)

	w.mu.RLock()
	defer w.mu.RUnlock()
	
	var result strings.Builder
	result.WriteString("Connected MCP Servers:\n")
	result.WriteString("=====================\n\n")
	
	// List static servers from initial config
	staticCount := len(w.proxyServer.config.Servers)
	if staticCount > 0 {
		result.WriteString("Static servers (from config):\n")
		for _, server := range w.proxyServer.config.Servers {
			result.WriteString(fmt.Sprintf("- %s [static]\n", server.Name))
		}
		result.WriteString("\n")
	}
	
	// List dynamic servers
	if len(w.dynamicServers) == 0 && staticCount == 0 {
		result.WriteString("No servers connected.\n")
	} else if len(w.dynamicServers) > 0 {
		result.WriteString("Dynamic servers:\n")
		for name, info := range w.dynamicServers {
			status := "connected"
			if !info.IsConnected {
				status = "disconnected"
				if info.ErrorMessage != "" {
					status = fmt.Sprintf("disconnected (%s)", info.ErrorMessage)
				}
			}
			result.WriteString(fmt.Sprintf("- %s [%s] - %d tools\n", name, status, len(info.Tools)))
			
			// List first few tools
			if len(info.Tools) > 0 && len(info.Tools) <= 5 {
				for _, tool := range info.Tools {
					result.WriteString(fmt.Sprintf("  • %s\n", tool))
				}
			} else if len(info.Tools) > 5 {
				for i := 0; i < 3; i++ {
					result.WriteString(fmt.Sprintf("  • %s\n", info.Tools[i]))
				}
				result.WriteString(fmt.Sprintf("  • ... and %d more\n", len(info.Tools)-3))
			}
		}
	}
	
	totalServers := staticCount + len(w.dynamicServers)
	result.WriteString(fmt.Sprintf("\nTotal servers: %d (static: %d, dynamic: %d)\n",
		totalServers, staticCount, len(w.dynamicServers)))

	toolResult := mcp.NewToolResultText(result.String())
	toolResult = w.addRecordingMetadata(toolResult)
	w.recordMessage("response", "tool_call", "server_list", "proxy", toolResult)
	return toolResult, nil
}

func (w *DynamicWrapper) handleServerDisconnect(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record the request
	w.recordMessage("request", "tool_call", "server_disconnect", "proxy", request)

	name, err := request.RequireString("name")
	if err != nil {
		result := mcp.NewToolResultError("name is required")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_disconnect", "proxy", result)
		return result, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	serverInfo, exists := w.dynamicServers[name]
	if !exists {
		result := mcp.NewToolResultError(fmt.Sprintf("Server '%s' not found", name))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_disconnect", "proxy", result)
		return result, nil
	}
	
	if !serverInfo.IsConnected {
		toolResult := mcp.NewToolResultText(fmt.Sprintf("Server '%s' is already disconnected", name))
		toolResult = w.addRecordingMetadata(toolResult)
		w.recordMessage("response", "tool_call", "server_disconnect", "proxy", toolResult)
		return toolResult, nil
	}
	
	log.Printf("Disconnecting server '%s'", name)
	
	// Close client and terminate process
	if serverInfo.Client != nil {
		log.Printf("Terminating process for server '%s'", name)
		if err := serverInfo.Client.Close(); err != nil {
			log.Printf("Error closing client %s: %v", name, err)
		}

		// Remove from proxy server's client list to prevent stale references
		w.proxyServer.mu.Lock()
		newClients := make([]client.MCPClient, 0, len(w.proxyServer.clients)-1)
		for _, c := range w.proxyServer.clients {
			if c.ServerName() != name {
				newClients = append(newClients, c)
			}
		}
		w.proxyServer.clients = newClients
		w.proxyServer.mu.Unlock()
		log.Printf("Removed client '%s' from proxy server's client list", name)
	}

	// Mark as disconnected but keep tools registered
	serverInfo.IsConnected = false
	serverInfo.ErrorMessage = "Server disconnected by user"
	serverInfo.Client = nil
	
	result := fmt.Sprintf("Disconnected server '%s'. Tools remain registered but will return errors.\\nUse server_reconnect to restore with new binary/command.", name)
	toolResult := mcp.NewToolResultText(result)
	toolResult = w.addRecordingMetadata(toolResult)
	w.recordMessage("response", "tool_call", "server_disconnect", "proxy", toolResult)
	return toolResult, nil
}

func (w *DynamicWrapper) handleServerReconnect(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record the request
	w.recordMessage("request", "tool_call", "server_reconnect", "proxy", request)

	name, err := request.RequireString("name")
	if err != nil {
		result := mcp.NewToolResultError("name is required")
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", result)
		return result, nil
	}

	// Get command (optional now)
	commandStr := request.GetString("command", "")

	w.mu.Lock()
	defer w.mu.Unlock()

	serverInfo, exists := w.dynamicServers[name]
	if !exists {
		result := mcp.NewToolResultError(fmt.Sprintf("Server '%s' not found", name))
		result = w.addRecordingMetadata(result)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", result)
		return result, nil
	}

	if serverInfo.IsConnected {
		toolResult := mcp.NewToolResultError(fmt.Sprintf("Server '%s' is still connected. Use server_disconnect first.", name))
		toolResult = w.addRecordingMetadata(toolResult)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
		return toolResult, nil
	}

	var serverConfig config.ServerConfig

	if commandStr != "" {
		// Command provided: parse and create new config
		log.Printf("Reconnecting server '%s' with NEW command: %s", name, commandStr)

		parts := strings.Fields(commandStr)
		if len(parts) == 0 {
			result := mcp.NewToolResultError("Invalid command")
			result = w.addRecordingMetadata(result)
			w.recordMessage("response", "tool_call", "server_reconnect", "proxy", result)
			return result, nil
		}

		// Create new config (preserves name/prefix, but loses env vars)
		serverConfig = config.ServerConfig{
			Name:      name,
			Prefix:    serverInfo.Config.Prefix,
			Transport: "stdio",
			Command:   parts[0],
			Args:      parts[1:],
			Timeout:   "30s",
		}
	} else {
		// Command omitted: use stored config
		log.Printf("Reconnecting server '%s' with STORED configuration", name)

		if serverInfo.Config.Command == "" {
			toolResult := mcp.NewToolResultError("Stored config has no command. Please provide command parameter.")
			toolResult = w.addRecordingMetadata(toolResult)
			w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
			return toolResult, nil
		}

		// Use stored config as-is (preserves env, inherit, timeout, etc.)
		serverConfig = serverInfo.Config
	}

	// Create and connect new client
	stdioClient := client.NewStdioClient(serverConfig.Name, serverConfig.Command, serverConfig.Args)

	// Apply inheritance config from stored ServerConfig
	inheritCfg := serverConfig.ResolveInheritConfig(w.proxyServer.config.Inherit)
	stdioClient.SetInheritConfig(inheritCfg)

	// Apply environment variables from stored ServerConfig
	if len(serverConfig.Env) > 0 {
		var env []string
		for key, value := range serverConfig.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		stdioClient.SetEnvironment(env)
	}

	if err := stdioClient.Connect(ctx); err != nil {
		// Mark as disconnected but keep tools registered
		serverInfo.IsConnected = false
		serverInfo.ErrorMessage = fmt.Sprintf("Failed to connect: %v", err)
		serverInfo.Config = serverConfig
		toolResult := mcp.NewToolResultError(fmt.Sprintf("Failed to connect: %v", err))
		toolResult = w.addRecordingMetadata(toolResult)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
		return toolResult, nil
	}

	if _, err := stdioClient.Initialize(ctx); err != nil {
		stdioClient.Close()
		serverInfo.IsConnected = false
		serverInfo.ErrorMessage = fmt.Sprintf("Failed to initialize: %v", err)
		serverInfo.Config = serverConfig
		toolResult := mcp.NewToolResultError(fmt.Sprintf("Failed to initialize: %v", err))
		toolResult = w.addRecordingMetadata(toolResult)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
		return toolResult, nil
	}

	// List tools from new server
	tools, err := stdioClient.ListTools(ctx)
	if err != nil {
		stdioClient.Close()
		serverInfo.IsConnected = false
		serverInfo.ErrorMessage = fmt.Sprintf("Failed to list tools: %v", err)
		serverInfo.Config = serverConfig
		toolResult := mcp.NewToolResultError(fmt.Sprintf("Failed to list tools: %v", err))
		toolResult = w.addRecordingMetadata(toolResult)
		w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
		return toolResult, nil
	}
	
	// Update server info (but NOT IsConnected yet - defer until all state updated)
	serverInfo.Client = stdioClient
	serverInfo.Config = serverConfig
	serverInfo.ErrorMessage = ""

	// Update proxy server's client list with proper mutex protection
	w.proxyServer.mu.Lock()
	clientFound := false
	for i, c := range w.proxyServer.clients {
		if c.ServerName() == name {
			w.proxyServer.clients[i] = stdioClient
			clientFound = true
			break
		}
	}
	if !clientFound {
		// Client not in list (was removed by disconnect), append it
		w.proxyServer.clients = append(w.proxyServer.clients, stdioClient)
		log.Printf("Added client '%s' to proxy server's client list", name)
	} else {
		log.Printf("Updated client '%s' in proxy server's client list", name)
	}
	w.proxyServer.mu.Unlock()

	// Update registry with new client (tools keep same names)
	for _, tool := range tools {
		prefixedName := fmt.Sprintf("%s_%s", name, tool.Name)

		// Check if this tool name exists in our registered tools
		found := false
		for _, registeredTool := range serverInfo.Tools {
			if registeredTool == prefixedName {
				found = true
				break
			}
		}

		if found {
			// Update registry with new client
			discoveredTool := discovery.RemoteTool{
				OriginalName: tool.Name,
				PrefixedName: prefixedName,
				Description:  tool.Description,
				InputSchema:  tool.InputSchema,
				ServerName:   name,
			}
			w.proxyServer.registry.RegisterTool(discoveredTool, stdioClient)
			log.Printf("Updated tool registration: %s", prefixedName)
		}
	}

	// NOW mark as connected (atomic state transition after all updates complete)
	serverInfo.IsConnected = true
	log.Printf("Server '%s' marked as connected", name)

	// Build result message based on how we reconnected
	var resultMsg string
	if commandStr != "" {
		resultMsg = fmt.Sprintf("Reconnected server '%s' with NEW command: %s %s\nServer now connected and tools updated.",
			name, serverConfig.Command, strings.Join(serverConfig.Args, " "))
	} else {
		resultMsg = fmt.Sprintf("Reconnected server '%s' using STORED configuration\nServer now connected and tools updated.", name)
	}

	toolResult := mcp.NewToolResultText(resultMsg)
	toolResult = w.addRecordingMetadata(toolResult)
	w.recordMessage("response", "tool_call", "server_reconnect", "proxy", toolResult)
	return toolResult, nil
}

// createDynamicProxyHandler creates a handler that checks connection status
func (w *DynamicWrapper) createDynamicProxyHandler(serverName, originalToolName string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Record the tool call request
		prefixedToolName := fmt.Sprintf("%s_%s", serverName, originalToolName)
		w.recordMessage("request", "tool_call", prefixedToolName, serverName, request)

		// Copy client reference while holding lock to prevent use-after-free
		w.mu.RLock()
		serverInfo, exists := w.dynamicServers[serverName]
		var client client.MCPClient
		if exists && serverInfo.IsConnected {
			client = serverInfo.Client  // Copy reference
		}
		w.mu.RUnlock()

		if !exists {
			result := mcp.NewToolResultError(fmt.Sprintf("Server '%s' not found", serverName))
			result = w.addRecordingMetadata(result)
			w.recordMessage("response", "tool_call", prefixedToolName, serverName, result)
			return result, nil
		}

		if client == nil {
			// Server disconnected
			errorMsg := fmt.Sprintf("Server '%s' is disconnected", serverName)
			if serverInfo.ErrorMessage != "" {
				errorMsg += fmt.Sprintf(": %s", serverInfo.ErrorMessage)
			}
			errorMsg += "\nUse server_reconnect to restore connection."
			result := mcp.NewToolResultError(errorMsg)
			result = w.addRecordingMetadata(result)
			w.recordMessage("response", "tool_call", prefixedToolName, serverName, result)
			return result, nil
		}

		// Extract arguments from the request
		args := request.GetArguments()
		argsMap := make(map[string]interface{})
		for key, value := range args {
			argsMap[key] = value
		}

		// Forward the call to the remote server using copied client reference
		// (safe from concurrent disconnect)
		result, err := client.CallTool(ctx, originalToolName, argsMap)
		if err != nil {
			// Mark server as disconnected on connection errors
			if isConnectionError(err) {
				w.mu.Lock()
				serverInfo.IsConnected = false
				serverInfo.ErrorMessage = err.Error()
				w.mu.Unlock()

				errorMsg := fmt.Sprintf("Server '%s' connection failed: %v\nUse server_reconnect to restore connection.", serverName, err)
				result := mcp.NewToolResultError(errorMsg)
				result = w.addRecordingMetadata(result)
				w.recordMessage("response", "tool_call", prefixedToolName, serverName, result)
				return result, nil
			}
			
			// Wrap error with server context
			errorMsg := fmt.Sprintf("[%s] %v", serverName, err)
			result := mcp.NewToolResultError(errorMsg)
			result = w.addRecordingMetadata(result)
			w.recordMessage("response", "tool_call", prefixedToolName, serverName, result)
			return result, nil
		}
		
		// Transform the result back to MCP format
		var finalResult *mcp.CallToolResult
		if result.IsError {
			if len(result.Content) > 0 {
				finalResult = mcp.NewToolResultError(result.Content[0].Text)
			} else {
				finalResult = mcp.NewToolResultError("Tool execution failed")
			}
		} else {
			// For successful results, convert content to text
			if len(result.Content) > 0 {
				var text string
				for i, content := range result.Content {
					if i > 0 {
						text += "\n"
					}
					text += content.Text
				}
				finalResult = mcp.NewToolResultText(text)
			} else {
				finalResult = mcp.NewToolResultText("Tool executed successfully")
			}
		}

		finalResult = w.addRecordingMetadata(finalResult)
		w.recordMessage("response", "tool_call", prefixedToolName, serverName, finalResult)
		return finalResult, nil
	}
}

// isConnectionError checks if an error indicates a connection problem
func isConnectionError(err error) bool {
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "timeout")
}

// Initialize initializes the proxy with static servers
func (w *DynamicWrapper) Initialize(ctx context.Context) error {
	// Initialize the proxy server with static servers
	if err := w.proxyServer.Initialize(ctx); err != nil {
		return err
	}

	// Populate dynamicServers map with static servers
	if err := w.populateStaticServers(); err != nil {
		return err
	}

	return nil
}

// populateStaticServers adds static servers from config to dynamicServers map
func (w *DynamicWrapper) populateStaticServers() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Iterate through static server configs
	for _, serverConfig := range w.proxyServer.config.Servers {
		// Find matching client by ServerName
		var matchingClient client.MCPClient
		for _, c := range w.proxyServer.clients {
			if c.ServerName() == serverConfig.Name {
				matchingClient = c
				break
			}
		}

		if matchingClient != nil {
			// SUCCESS: Server connected, add with tools
			allTools := w.proxyServer.registry.GetAllTools()
			var serverTools []string
			for _, tool := range allTools {
				if tool.ServerName == serverConfig.Name {
					serverTools = append(serverTools, tool.PrefixedName)
				}
			}

			serverInfo := &DynamicServerInfo{
				Name:         serverConfig.Name,
				Client:       matchingClient,
				Config:       serverConfig,
				Tools:        serverTools,
				IsConnected:  true,
				ErrorMessage: "",
			}
			w.dynamicServers[serverConfig.Name] = serverInfo
			log.Printf("Added static server '%s' to dynamic management with %d tools",
				serverConfig.Name, len(serverTools))
		} else {
			// FAILED: No client, but still add to enable reconnect
			var errorMsg string
			for _, result := range w.proxyServer.discoveryResults {
				if result.ServerName == serverConfig.Name && result.Error != nil {
					errorMsg = result.Error.Error()
					break
				}
			}
			if errorMsg == "" {
				errorMsg = "Failed to connect during initialization"
			}

			serverInfo := &DynamicServerInfo{
				Name:         serverConfig.Name,
				Client:       nil,
				Config:       serverConfig,  // Store config for reconnect
				Tools:        []string{},
				IsConnected:  false,
				ErrorMessage: errorMsg,
			}
			w.dynamicServers[serverConfig.Name] = serverInfo
			log.Printf("Added static server '%s' to dynamic management (disconnected: %s)",
				serverConfig.Name, errorMsg)
		}
	}

	return nil
}

// Start starts the MCP server
func (w *DynamicWrapper) Start() error {
	log.Println("Starting Dynamic MCP Proxy Server with management tools...")
	return server.ServeStdio(w.baseServer)
}