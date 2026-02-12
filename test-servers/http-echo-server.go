package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	addr := ":8080"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	// Create MCP server
	s := server.NewMCPServer(
		"HTTP Echo Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add echo tool — returns the input text back
	echoTool := mcp.NewTool("echo",
		mcp.WithDescription("Echo back the provided text"),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Text to echo back"),
		),
	)

	// Add upcase tool — converts text to uppercase
	upcaseTool := mcp.NewTool("upcase",
		mcp.WithDescription("Convert text to uppercase"),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Text to convert to uppercase"),
		),
	)

	// Add timestamp tool — returns current time
	timestampTool := mcp.NewTool("timestamp",
		mcp.WithDescription("Return the current server timestamp"),
	)

	// Register handlers
	s.AddTool(echoTool, echoHandler)
	s.AddTool(upcaseTool, upcaseHandler)
	s.AddTool(timestampTool, timestampHandler)

	// Create and start HTTP server
	httpServer := server.NewStreamableHTTPServer(s)

	log.Printf("HTTP Echo Server starting on %s/mcp", addr)
	log.Printf("Configure mcp-debug with:")
	log.Printf("  transport: http")
	log.Printf("  url: http://localhost%s/mcp", addr)
	if err := httpServer.Start(addr); err != nil {
		fmt.Fprintf(os.Stderr, "HTTP Echo Server error: %v\n", err)
		os.Exit(1)
	}
}

func echoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := request.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Echo: %s", text)), nil
}

func upcaseHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	text, err := request.RequireString("text")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(strings.ToUpper(text)), nil
}

func timestampHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(time.Now().UTC().Format(time.RFC3339)), nil
}
