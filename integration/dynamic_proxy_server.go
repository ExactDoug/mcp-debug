package integration

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	mcp_golang "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
	"mcp-debug/client"
	"mcp-debug/config"
)

// DiscoveredTool represents a tool discovered from a remote server
type DiscoveredTool struct {
	OriginalName string
	PrefixedName string
	Description  string
	ServerName   string
}

// DynamicProxyServer provides true dynamic MCP proxy capabilities using mcp-golang
type DynamicProxyServer struct {
	mcpServer     *mcp_golang.Server
	clients       map[string]client.MCPClient // server name -> client
	serverConfigs map[string]config.ServerConfig // server name -> config
	toolRegistry  map[string][]string // server name -> list of tool names
	config        *config.ProxyConfig // Full proxy config including Inherit settings
	mu            sync.RWMutex
}

// NewDynamicProxyServer creates a new dynamic proxy server
func NewDynamicProxyServer(cfg *config.ProxyConfig) *DynamicProxyServer {
	// Create MCP server with stdio transport
	mcpServer := mcp_golang.NewServer(
		stdio.NewStdioServerTransport(),
		mcp_golang.WithName("Dynamic MCP Proxy"),
		mcp_golang.WithVersion("1.0.0"),
		mcp_golang.WithInstructions("Dynamic MCP proxy that can connect to multiple MCP servers and expose their tools with prefixes"),
	)

	return &DynamicProxyServer{
		mcpServer:     mcpServer,
		clients:       make(map[string]client.MCPClient),
		serverConfigs: make(map[string]config.ServerConfig),
		toolRegistry:  make(map[string][]string),
		config:        cfg,
	}
}

// ConnectToServer dynamically connects to an MCP server and registers its tools
func (p *DynamicProxyServer) ConnectToServer(ctx context.Context, serverConfig config.ServerConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	serverName := serverConfig.Name
	log.Printf("Connecting to server: %s", serverName)

	// Check if already connected
	if _, exists := p.clients[serverName]; exists {
		return fmt.Errorf("server %s already connected", serverName)
	}

	// Create and connect client
	mcpClient, err := p.createAndConnectClient(ctx, serverConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to server %s: %w", serverName, err)
	}

	// Discover tools from the connected client
	tools, err := mcpClient.ListTools(ctx)
	if err != nil {
		mcpClient.Close()
		return fmt.Errorf("failed to list tools from %s: %w", serverName, err)
	}

	// Convert tools to discovery format
	var discoveredTools []*DiscoveredTool
	for _, tool := range tools {
		discoveredTool := &DiscoveredTool{
			OriginalName:  tool.Name,
			PrefixedName:  fmt.Sprintf("%s_%s", serverConfig.Prefix, tool.Name),
			Description:   tool.Description,
			ServerName:    serverName,
		}
		discoveredTools = append(discoveredTools, discoveredTool)
	}

	// Store client and config
	p.clients[serverName] = mcpClient
	p.serverConfigs[serverName] = serverConfig

	// Register all discovered tools dynamically
	var registeredTools []string
	toolCount := 0
	for _, tool := range discoveredTools {
		if err := p.registerTool(tool, mcpClient); err != nil {
			log.Printf("Warning: Failed to register tool %s: %v", tool.PrefixedName, err)
			continue
		}
		registeredTools = append(registeredTools, tool.PrefixedName)
		toolCount++
		log.Printf("Dynamically registered tool: %s", tool.PrefixedName)
	}
	
	// Track registered tools for this server
	p.toolRegistry[serverName] = registeredTools

	log.Printf("Successfully connected to %s and registered %d tools", serverName, toolCount)
	return nil
}

// DisconnectFromServer dynamically disconnects from an MCP server and removes its tools
func (p *DynamicProxyServer) DisconnectFromServer(serverName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("Disconnecting from server: %s", serverName)

	// Get client
	mcpClient, exists := p.clients[serverName]
	if !exists {
		return fmt.Errorf("server %s not connected", serverName)
	}

	// Deregister all tools for this server
	toolsDeregistered := 0
	if registeredTools, exists := p.toolRegistry[serverName]; exists {
		for _, toolName := range registeredTools {
			if err := p.mcpServer.DeregisterTool(toolName); err != nil {
				log.Printf("Warning: Failed to deregister tool %s: %v", toolName, err)
			} else {
				toolsDeregistered++
				log.Printf("Deregistered tool: %s", toolName)
			}
		}
		delete(p.toolRegistry, serverName)
	}

	// Close client connection
	if err := mcpClient.Close(); err != nil {
		log.Printf("Warning: Error closing client for %s: %v", serverName, err)
	}

	// Remove from maps
	delete(p.clients, serverName)
	delete(p.serverConfigs, serverName)

	log.Printf("Successfully disconnected from %s and removed %d tools", serverName, toolsDeregistered)
	return nil
}

// Serve starts the MCP server (can be called immediately, tools added dynamically)
func (p *DynamicProxyServer) Serve() error {
	// Register management tools
	p.registerManagementTools()
	
	log.Printf("Starting dynamic MCP proxy server (tools will be added as servers connect)...")
	return p.mcpServer.Serve()
}

// registerManagementTools adds tools for dynamic server management
func (p *DynamicProxyServer) registerManagementTools() {
	// server_add tool - accepts name and command/url/config
	type ServerAddArgs struct {
		Name    string                 `json:"name" jsonschema:"required,description=Name/prefix for the server"`
		Command string                 `json:"command,omitempty" jsonschema:"description=Command to run (e.g. 'npx -y @modelcontextprotocol/filesystem /path')"`
		URL     string                 `json:"url,omitempty" jsonschema:"description=URL for HTTP/WebSocket server (e.g. 'http://localhost:5001/mcp')"`
		Config  map[string]interface{} `json:"config,omitempty" jsonschema:"description=Full server configuration object"`
	}
	
	p.mcpServer.RegisterTool("server_add", "Add a new MCP server to the proxy", 
		func(args ServerAddArgs) (*mcp_golang.ToolResponse, error) {
			return p.handleServerAdd(args)
		})
	
	// server_remove tool
	type ServerRemoveArgs struct {
		Name string `json:"name" jsonschema:"required,description=Name of the server to remove"`
	}
	
	p.mcpServer.RegisterTool("server_remove", "Remove an MCP server from the proxy", 
		func(args ServerRemoveArgs) (*mcp_golang.ToolResponse, error) {
			return p.handleServerRemove(args.Name)
		})
	
	// server_list tool - no arguments
	p.mcpServer.RegisterTool("server_list", "List all connected MCP servers", 
		func() (*mcp_golang.ToolResponse, error) {
			return p.handleServerList()
		})
}

// Handler implementations for management tools

func (p *DynamicProxyServer) handleServerAdd(args interface{}) (*mcp_golang.ToolResponse, error) {
	// Type assert to get our args struct
	addArgs, ok := args.(struct {
		Name    string                 `json:"name"`
		Command string                 `json:"command,omitempty"`
		URL     string                 `json:"url,omitempty"`
		Config  map[string]interface{} `json:"config,omitempty"`
	})
	if !ok {
		return nil, fmt.Errorf("invalid arguments")
	}
	
	// Check if server already exists
	p.mu.RLock()
	_, exists := p.clients[addArgs.Name]
	p.mu.RUnlock()
	
	if exists {
		return nil, fmt.Errorf("server '%s' already exists", addArgs.Name)
	}
	
	// Create server config based on provided parameters
	var serverConfig config.ServerConfig
	serverConfig.Name = addArgs.Name
	serverConfig.Prefix = addArgs.Name
	
	// Parse based on what was provided
	if addArgs.Command != "" {
		// Parse command into command and args
		parts := strings.Fields(addArgs.Command)
		if len(parts) == 0 {
			return nil, fmt.Errorf("invalid command")
		}
		serverConfig.Transport = "stdio"
		serverConfig.Command = parts[0]
		if len(parts) > 1 {
			serverConfig.Args = parts[1:]
		}
	} else if addArgs.URL != "" {
		// HTTP/WebSocket transport
		serverConfig.Transport = "http"
		serverConfig.URL = addArgs.URL
		return nil, fmt.Errorf("URL transport not yet implemented")
	} else if addArgs.Config != nil {
		// Parse config object
		if transport, ok := addArgs.Config["transport"].(string); ok {
			serverConfig.Transport = transport
		}
		if cmd, ok := addArgs.Config["command"].(string); ok {
			serverConfig.Command = cmd
		}
		if args, ok := addArgs.Config["args"].([]interface{}); ok {
			for _, arg := range args {
				if s, ok := arg.(string); ok {
					serverConfig.Args = append(serverConfig.Args, s)
				}
			}
		}
		if timeout, ok := addArgs.Config["timeout"].(string); ok {
			serverConfig.Timeout = timeout
		}
	} else {
		return nil, fmt.Errorf("must provide either command, url, or config")
	}
	
	// Set defaults
	if serverConfig.Timeout == "" {
		serverConfig.Timeout = "10s"
	}
	
	// Connect in background
	go func() {
		ctx := context.Background()
		if err := p.ConnectToServer(ctx, serverConfig); err != nil {
			log.Printf("Failed to connect to server %s: %v", addArgs.Name, err)
		}
	}()
	
	result := fmt.Sprintf("Adding server '%s' with command: %s %s\nConnection initiated in background. Use server_list to check status.", 
		addArgs.Name, serverConfig.Command, strings.Join(serverConfig.Args, " "))
	
	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
}

func (p *DynamicProxyServer) handleServerRemove(name string) (*mcp_golang.ToolResponse, error) {
	if err := p.DisconnectFromServer(name); err != nil {
		return nil, err
	}
	
	result := fmt.Sprintf("Successfully removed server '%s'", name)
	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result)), nil
}

func (p *DynamicProxyServer) handleServerList() (*mcp_golang.ToolResponse, error) {
	servers := p.ListConnectedServers()
	
	var result strings.Builder
	result.WriteString("Connected MCP Servers:\n")
	result.WriteString("=====================\n\n")
	
	if len(servers) == 0 {
		result.WriteString("No servers connected.\n")
	} else {
		for _, serverName := range servers {
			tools := p.GetServerTools(serverName)
			result.WriteString(fmt.Sprintf("- %s (%d tools)\n", serverName, len(tools)))
			
			// List first few tools as examples
			if len(tools) > 0 {
				result.WriteString("  Tools: ")
				toolsToShow := tools
				if len(toolsToShow) > 3 {
					toolsToShow = toolsToShow[:3]
				}
				result.WriteString(strings.Join(toolsToShow, ", "))
				if len(tools) > 3 {
					result.WriteString(fmt.Sprintf(" ... and %d more", len(tools)-3))
				}
				result.WriteString("\n")
			}
		}
	}
	
	result.WriteString(fmt.Sprintf("\nTotal servers: %d\n", len(servers)))
	
	return mcp_golang.NewToolResponse(mcp_golang.NewTextContent(result.String())), nil
}

// Helper methods

func (p *DynamicProxyServer) createAndConnectClient(ctx context.Context, serverConfig config.ServerConfig) (client.MCPClient, error) {
	switch serverConfig.Transport {
	case "stdio":
		stdioClient := client.NewStdioClient(serverConfig.Name, serverConfig.Command, serverConfig.Args)

		// Set inheritance config
		inheritCfg := serverConfig.ResolveInheritConfig(p.config.Inherit)
		stdioClient.SetInheritConfig(inheritCfg)

		if serverConfig.Env != nil {
			// Convert map[string]string to []string
			envSlice := make([]string, 0, len(serverConfig.Env))
			for key, value := range serverConfig.Env {
				envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
			}
			stdioClient.SetEnvironment(envSlice)
		}

		if err := stdioClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("failed to connect stdio client: %w", err)
		}

		if _, err := stdioClient.Initialize(ctx); err != nil {
			stdioClient.Close()
			return nil, fmt.Errorf("failed to initialize client: %w", err)
		}

		return stdioClient, nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", serverConfig.Transport)
	}
}

func (p *DynamicProxyServer) registerTool(tool *DiscoveredTool, mcpClient client.MCPClient) error {
	// Create proxy handler that forwards calls to the remote server
	// Use map[string]interface{} as generic argument type since we're proxying
	handler := func(args map[string]interface{}) (*mcp_golang.ToolResponse, error) {
		// Call the remote server with the arguments as-is
		result, err := mcpClient.CallTool(context.Background(), tool.OriginalName, args)
		if err != nil {
			return nil, fmt.Errorf("proxy call failed: %w", err)
		}

		// Convert result to mcp-golang format
		if result.IsError {
			// Extract error message from content items
			var errorMsg string
			if len(result.Content) > 0 {
				errorMsg = result.Content[0].Text
			} else {
				errorMsg = "Unknown error"
			}
			return nil, fmt.Errorf("remote tool error: %s", errorMsg)
		}

		// Convert content items to mcp-golang format
		var contents []*mcp_golang.Content
		for _, item := range result.Content {
			if item.Type == "text" {
				contents = append(contents, mcp_golang.NewTextContent(item.Text))
			}
			// Could extend to support other content types like images
		}

		if len(contents) == 0 {
			contents = append(contents, mcp_golang.NewTextContent("No content"))
		}

		return mcp_golang.NewToolResponse(contents...), nil
	}

	// Register the tool with the server
	description := fmt.Sprintf("[%s] %s", tool.ServerName, tool.Description)
	return p.mcpServer.RegisterTool(tool.PrefixedName, description, handler)
}

// ListConnectedServers returns a list of currently connected server names
func (p *DynamicProxyServer) ListConnectedServers() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	servers := make([]string, 0, len(p.clients))
	for serverName := range p.clients {
		servers = append(servers, serverName)
	}
	return servers
}

// GetServerTools returns the list of tools registered for a specific server
func (p *DynamicProxyServer) GetServerTools(serverName string) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if tools, exists := p.toolRegistry[serverName]; exists {
		result := make([]string, len(tools))
		copy(result, tools)
		return result
	}
	return nil
}

// Shutdown gracefully shuts down the proxy server
func (p *DynamicProxyServer) Shutdown() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("Shutting down dynamic proxy server...")

	// Close all client connections
	for serverName, mcpClient := range p.clients {
		if err := mcpClient.Close(); err != nil {
			log.Printf("Warning: Error closing client for %s: %v", serverName, err)
		}
	}

	// Clear maps
	p.clients = make(map[string]client.MCPClient)
	p.serverConfigs = make(map[string]config.ServerConfig)

	log.Printf("Dynamic proxy server shutdown complete")
	return nil
}