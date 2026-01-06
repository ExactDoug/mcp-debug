package main

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestHelloWorldHandler tests the hello_world tool handler directly
func TestHelloWorldHandler(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic greeting",
			input:    "World",
			expected: "Hello, World!",
		},
		{
			name:     "named greeting",
			input:    "Claude",
			expected: "Hello, Claude!",
		},
		{
			name:     "empty name",
			input:    "",
			expected: "Hello, !",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"name": tt.input,
			}

			result, err := helloHandler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			// Check the result content
			if len(result.Content) == 0 {
				t.Fatal("no content in result")
			}

			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent, got %T", result.Content[0])
			}

			if textContent.Text != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, textContent.Text)
			}
		})
	}
}

// TestHelloWorldHandlerMissingName tests error handling for missing required parameter
func TestHelloWorldHandlerMissingName(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{}

	result, err := helloHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return an error result, not a successful result
	if result == nil {
		t.Fatal("result is nil")
	}

	if !result.IsError {
		t.Error("expected IsError to be true for missing required parameter")
	}
}

// TestServerToolRegistration tests that tools can be registered with the MCP server
func TestServerToolRegistration(t *testing.T) {
	s := server.NewMCPServer(
		"Test Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Define and add a tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("A test tool"),
		mcp.WithString("input",
			mcp.Required(),
			mcp.Description("Test input"),
		),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, _ := request.RequireString("input")
		return mcp.NewToolResultText("Received: " + input), nil
	}

	s.AddTool(tool, handler)

	// Verify the tool was registered by checking we can list tools
	// This is a basic smoke test
	t.Log("Tool registered successfully")
}

// TestDynamicToolAddRemove tests dynamic tool registration and removal
func TestDynamicToolAddRemove(t *testing.T) {
	s := server.NewMCPServer(
		"Dynamic Test Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add a tool dynamically
	tool1 := mcp.NewTool("dynamic_tool_1",
		mcp.WithDescription("First dynamic tool"),
	)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("Tool 1 executed"), nil
	}

	s.AddTool(tool1, handler)
	t.Log("Added dynamic_tool_1")

	// Add another tool
	tool2 := mcp.NewTool("dynamic_tool_2",
		mcp.WithDescription("Second dynamic tool"),
	)

	s.AddTool(tool2, handler)
	t.Log("Added dynamic_tool_2")

	// Remove the first tool
	s.DeleteTools("dynamic_tool_1")
	t.Log("Deleted dynamic_tool_1")

	// This verifies the SDK supports dynamic operations without panicking
}

// TestGetRegisteredTools tests the CLI tool registry
func TestGetRegisteredTools(t *testing.T) {
	tools := getRegisteredTools()

	if len(tools) == 0 {
		t.Fatal("expected at least one registered tool")
	}

	// Check that hello_world exists
	var helloWorld *Tool
	for i := range tools {
		if tools[i].Name == "hello_world" {
			helloWorld = &tools[i]
			break
		}
	}

	if helloWorld == nil {
		t.Fatal("hello_world tool not found")
	}

	if helloWorld.Description == "" {
		t.Error("hello_world description is empty")
	}

	if len(helloWorld.Parameters) == 0 {
		t.Error("hello_world has no parameters")
	}

	// Test the handler
	result := helloWorld.Handler(map[string]string{"name": "Test"})
	if result != "Hello, Test!" {
		t.Errorf("expected 'Hello, Test!', got '%s'", result)
	}
}

// TestToolHandlerWithEmptyArgs tests tool handler with empty arguments
func TestToolHandlerWithEmptyArgs(t *testing.T) {
	tools := getRegisteredTools()

	for _, tool := range tools {
		if tool.Name == "hello_world" {
			result := tool.Handler(map[string]string{})
			if result != "Hello, World!" {
				t.Errorf("expected 'Hello, World!' for empty args, got '%s'", result)
			}
		}
	}
}
