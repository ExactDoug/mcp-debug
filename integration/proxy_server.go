package integration

import (
	"context"
	"fmt"
	"log"
	"sync"
	
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	
	"mcp-debug/client"
	"mcp-debug/config"
	"mcp-debug/discovery"
	"mcp-debug/proxy"
)

// ProxyServer manages the complete MCP proxy server
type ProxyServer struct {
	config           *config.ProxyConfig
	mcpServer        *server.MCPServer
	registry         *proxy.ToolRegistry
	clients          []client.MCPClient
	discoverer       *discovery.Discoverer
	discoveryResults []*discovery.DiscoveryResult // Store for populateStaticServers access
	recorderFunc     proxy.RecorderFunc // Optional recorder for tool call traffic
	metadataFunc     func(*mcp.CallToolResult) *mcp.CallToolResult // Optional metadata injector

	mu           sync.RWMutex
	initialized  bool
}

// NewProxyServer creates a new proxy server with the given configuration
func NewProxyServer(cfg *config.ProxyConfig) *ProxyServer {
	return &ProxyServer{
		config:     cfg,
		registry:   proxy.NewToolRegistry(),
		discoverer: discovery.NewDiscoverer(cfg),
		clients:    make([]client.MCPClient, 0),
	}
}

// Initialize sets up the proxy server by connecting to all remote servers and discovering tools
func (p *ProxyServer) Initialize(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.initialized {
		return nil
	}
	
	log.Println("Initializing Dynamic MCP Proxy Server...")

	// Create MCP server instance ONLY if one doesn't exist
	// (DynamicWrapper pre-assigns this before calling Initialize)
	if p.mcpServer == nil {
		p.mcpServer = server.NewMCPServer(
			"Dynamic MCP Proxy",
			"1.0.0",
			server.WithToolCapabilities(true),
		)
	}
	
	// Discover tools from all configured servers
	log.Println("Discovering tools from remote servers...")
	results, err := p.discoverer.DiscoverAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover tools: %w", err)
	}

	// Store results for populateStaticServers to access
	p.discoveryResults = results

	// Process discovery results
	successfulResults := discovery.GetSuccessfulResults(results)
	failedResults := discovery.GetFailedResults(results)
	
	// Log discovery summary
	log.Printf("Discovery complete: %d successful, %d failed", len(successfulResults), len(failedResults))
	
	// Report failed discoveries
	for _, result := range failedResults {
		log.Printf("Failed to discover tools from %s: %v", result.ServerName, result.Error)
	}
	
	// Process successful discoveries
	totalTools := 0
	for _, result := range successfulResults {
		log.Printf("Discovered %d tools from %s in %v", result.ToolCount(), result.ServerName, result.Duration)
		totalTools += result.ToolCount()
		
		// Connect to the server and keep client alive
		mcpClient, err := p.createAndConnectClient(ctx, result.ServerName)
		if err != nil {
			log.Printf("Warning: Failed to create persistent client for %s: %v", result.ServerName, err)
			continue
		}
		
		p.clients = append(p.clients, mcpClient)
		
		// Register tools in registry
		for _, tool := range result.Tools {
			p.registry.RegisterTool(tool, mcpClient)

			// Note: Handlers will be created by DynamicWrapper using dynamic lookup pattern
			// This allows hot-swapping to work correctly for static servers
			log.Printf("Registered tool in registry (handler to be created by wrapper): %s", tool.PrefixedName)
		}
	}
	
	log.Printf("Successfully registered %d tools from %d servers", totalTools, len(successfulResults))
	
	// Allow starting with zero tools for dynamic management
	if totalTools == 0 {
		log.Printf("Starting with no tools - use server_add to add MCP servers dynamically")
	}
	
	p.initialized = true
	return nil
}

// Start starts the MCP proxy server
func (p *ProxyServer) Start() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if !p.initialized {
		return fmt.Errorf("server not initialized - call Initialize() first")
	}
	
	log.Println("Starting MCP proxy server...")
	
	// Start the MCP server (this blocks)
	return server.ServeStdio(p.mcpServer)
}

// Shutdown gracefully shuts down the proxy server
func (p *ProxyServer) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	log.Println("Shutting down proxy server...")
	
	var errors []error
	
	// Close all client connections
	for _, client := range p.clients {
		if err := client.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close client %s: %w", client.ServerName(), err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}
	
	log.Println("Proxy server shutdown complete")
	return nil
}

// createAndConnectClient creates and connects a client for persistent use
func (p *ProxyServer) createAndConnectClient(ctx context.Context, serverName string) (client.MCPClient, error) {
	// Find server config
	var serverConfig *config.ServerConfig
	for _, cfg := range p.config.Servers {
		if cfg.Name == serverName {
			serverConfig = &cfg
			break
		}
	}
	
	if serverConfig == nil {
		return nil, fmt.Errorf("server config not found: %s", serverName)
	}
	
	// Create client based on transport
	var mcpClient client.MCPClient
	
	switch serverConfig.Transport {
	case "stdio":
		stdioClient := client.NewStdioClient(serverConfig.Name, serverConfig.Command, serverConfig.Args)

		// Set inheritance config
		inheritCfg := serverConfig.ResolveInheritConfig(p.config.Inherit)
		stdioClient.SetInheritConfig(inheritCfg)

		// Set environment variables if specified
		if len(serverConfig.Env) > 0 {
			var env []string
			for key, value := range serverConfig.Env {
				env = append(env, fmt.Sprintf("%s=%s", key, value))
			}
			stdioClient.SetEnvironment(env)
		}

		mcpClient = stdioClient
	default:
		return nil, fmt.Errorf("unsupported transport: %s", serverConfig.Transport)
	}
	
	// Connect and initialize
	if err := mcpClient.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	
	if _, err := mcpClient.Initialize(ctx); err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}
	
	return mcpClient, nil
}

// createMCPTool creates an mcp.Tool from a RemoteTool
func (p *ProxyServer) createMCPTool(remoteTool discovery.RemoteTool) mcp.Tool {
	// For now, create a simple tool with basic parameters
	// In a full implementation, we would parse the InputSchema to create proper parameter definitions
	
	return mcp.NewTool(remoteTool.PrefixedName,
		mcp.WithDescription(fmt.Sprintf("[%s] %s", remoteTool.ServerName, remoteTool.Description)),
	)
}

// GetRegisteredTools returns all registered tools for debugging/info
func (p *ProxyServer) GetRegisteredTools() []discovery.RemoteTool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return p.registry.GetAllTools()
}

// IsInitialized returns true if the server has been initialized
func (p *ProxyServer) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return p.initialized
}