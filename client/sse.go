package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseSSEResponse reads an SSE stream and returns the first JSON-RPC response
// matching the expected request ID. MCP servers may send responses as SSE when
// the client sends Accept: text/event-stream.
//
// SSE format:
//   event: message
//   data: {"jsonrpc":"2.0","id":1,"result":{...}}
//
// We look for "event: message" lines followed by "data:" lines containing
// JSON-RPC responses. Other events (like heartbeats) are ignored.
func ParseSSEResponse(reader io.Reader, expectedID int64) (*JSONRPCResponse, error) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer for large SSE responses (e.g., tools/list with many tools)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // up to 4MB

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of event block
		if line == "" {
			currentEvent = ""
			continue
		}

		// Parse event type
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		// Parse data line
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			// Only process "message" events (MCP spec)
			if currentEvent != "message" {
				continue
			}

			// Try to parse as JSON-RPC response
			var response JSONRPCResponse
			if err := json.Unmarshal([]byte(data), &response); err != nil {
				// Skip malformed data lines
				continue
			}

			// Check if this is the response we're waiting for
			if response.ID == expectedID {
				return &response, nil
			}

			// Not our response (could be a notification or different request)
			// Continue scanning
		}

		// Ignore other lines (comments starting with :, unknown fields)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading SSE stream: %w", err)
	}

	return nil, fmt.Errorf("SSE stream ended without response for request ID %d", expectedID)
}
