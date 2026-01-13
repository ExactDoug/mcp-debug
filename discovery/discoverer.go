package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"
	
	"mcp-debug/client"
	"mcp-debug/config"
)

// Discoverer handles tool discovery from multiple MCP servers
type Discoverer struct {
	config *config.ProxyConfig
}

// NewDiscoverer creates a new tool discoverer
func NewDiscoverer(cfg *config.ProxyConfig) *Discoverer {
	return &Discoverer{
		config: cfg,
	}
}

// DiscoverAll discovers tools from all configured servers concurrently
func (d *Discoverer) DiscoverAll(ctx context.Context) ([]*DiscoveryResult, error) {
	results := make([]*DiscoveryResult, len(d.config.Servers))
	var wg sync.WaitGroup
	
	// Start discovery for each server concurrently
	for i, serverConfig := range d.config.Servers {
		wg.Add(1)
		go func(index int, cfg config.ServerConfig) {
			defer wg.Done()
			
			result := d.discoverServer(ctx, cfg)
			results[index] = result
		}(i, serverConfig)
	}
	
	// Wait for all discoveries to complete
	wg.Wait()
	
	return results, nil
}

// DiscoverServer discovers tools from a single server
func (d *Discoverer) DiscoverServer(ctx context.Context, serverConfig config.ServerConfig) *DiscoveryResult {
	return d.discoverServer(ctx, serverConfig)
}

// discoverServer performs the actual discovery from a single server
func (d *Discoverer) discoverServer(ctx context.Context, serverConfig config.ServerConfig) *DiscoveryResult {
	start := time.Now()
	
	result := &DiscoveryResult{
		ServerName:   serverConfig.Name,
		ServerPrefix: serverConfig.Prefix,
		Tools:        []RemoteTool{},
	}
	
	// Create client based on transport type
	var mcpClient client.MCPClient
	var err error
	
	switch serverConfig.Transport {
	case "stdio":
		mcpClient, err = d.createStdioClient(serverConfig)
	case "http":
		err = fmt.Errorf("HTTP transport not yet implemented")
	default:
		err = fmt.Errorf("unsupported transport: %s", serverConfig.Transport)
	}
	
	if err != nil {
		result.Error = fmt.Errorf("failed to create client: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	
	// Ensure client is closed when done
	defer func() {
		if closeErr := mcpClient.Close(); closeErr != nil {
			// Log error but don't override main error
			fmt.Printf("Warning: failed to close client for %s: %v\n", serverConfig.Name, closeErr)
		}
	}()
	
	// Connect to server
	if err := mcpClient.Connect(ctx); err != nil {
		result.Error = fmt.Errorf("failed to connect: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	
	// Initialize MCP protocol
	_, err = mcpClient.Initialize(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to initialize: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	
	// List tools
	toolInfos, err := mcpClient.ListTools(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to list tools: %w", err)
		result.Duration = time.Since(start)
		return result
	}
	
	// Convert to prefixed tools
	for _, toolInfo := range toolInfos {
		remoteTool := CreatePrefixedTool(serverConfig.Name, serverConfig.Prefix, ToolInfo{
			Name:        toolInfo.Name,
			Description: toolInfo.Description,
			InputSchema: toolInfo.InputSchema,
		})
		result.Tools = append(result.Tools, remoteTool)
	}
	
	result.Duration = time.Since(start)
	return result
}

// createStdioClient creates a stdio-based MCP client
func (d *Discoverer) createStdioClient(serverConfig config.ServerConfig) (client.MCPClient, error) {
	stdioClient := client.NewStdioClient(serverConfig.Name, serverConfig.Command, serverConfig.Args)

	// Set inheritance config
	inheritCfg := serverConfig.ResolveInheritConfig(d.config.Inherit)
	stdioClient.SetInheritConfig(inheritCfg)

	// Set environment variables if specified
	if len(serverConfig.Env) > 0 {
		var env []string
		for key, value := range serverConfig.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
		stdioClient.SetEnvironment(env)
	}
	
	return stdioClient, nil
}

// CreateToolMapping creates a mapping from prefixed tool names to their metadata
func CreateToolMapping(results []*DiscoveryResult) map[string]RemoteTool {
	toolMap := make(map[string]RemoteTool)
	
	for _, result := range results {
		if result.IsSuccessful() {
			for _, tool := range result.Tools {
				toolMap[tool.PrefixedName] = tool
			}
		}
	}
	
	return toolMap
}

// GetSuccessfulResults filters results to only successful discoveries
func GetSuccessfulResults(results []*DiscoveryResult) []*DiscoveryResult {
	var successful []*DiscoveryResult
	
	for _, result := range results {
		if result.IsSuccessful() {
			successful = append(successful, result)
		}
	}
	
	return successful
}

// GetFailedResults filters results to only failed discoveries
func GetFailedResults(results []*DiscoveryResult) []*DiscoveryResult {
	var failed []*DiscoveryResult
	
	for _, result := range results {
		if !result.IsSuccessful() {
			failed = append(failed, result)
		}
	}
	
	return failed
}