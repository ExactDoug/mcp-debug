# Session Recording & Playback

MCP Debug provides comprehensive recording of JSON-RPC traffic in proxy mode, enabling debugging, documentation, and regression testing workflows.

## Overview

When running in proxy mode with the `--record` flag, mcp-debug captures:
- All tool call requests and responses
- Management tool operations (server_add, server_remove, etc.)
- Tool calls to all servers (both static and dynamically added)
- Error responses and connection failures
- Complete JSON-RPC message payloads

## Quick Start

```bash
# Start proxy with recording enabled
mcp-debug --proxy --config config.yaml --record session.jsonl

# Use the proxy normally - all traffic is recorded
# Stop the proxy when done (Ctrl+C)

# Examine the recording
cat session.jsonl
```

## Recording Format

Recordings use **JSONL** (JSON Lines) format - one JSON object per line:

```jsonl
# MCP Recording Session
# Started: 2026-01-12T23:44:33-07:00
{"start_time":"2026-01-12T23:44:33.862903809-07:00","server_info":"Dynamic MCP Proxy v1.0.0","messages":[]}
{"timestamp":"2026-01-12T23:45:42.940680618-07:00","direction":"request","message_type":"tool_call","tool_name":"fs_read_file","server_name":"filesystem","message":{...}}
{"timestamp":"2026-01-12T23:45:43.123456789-07:00","direction":"response","message_type":"tool_call","tool_name":"fs_read_file","server_name":"filesystem","message":{...}}
```

### File Structure

1. **Comment Lines** (lines 1-2): Human-readable session metadata
2. **Session Header** (line 3): JSON object with session information
3. **Message Lines** (line 4+): One JSON object per message

### Session Header

```json
{
  "start_time": "2026-01-12T23:44:33.862903809-07:00",
  "server_info": "Dynamic MCP Proxy v1.0.0",
  "messages": []
}
```

Fields:
- `start_time`: ISO 8601 timestamp when recording started
- `server_info`: Proxy version information
- `messages`: Always empty array (messages stored as separate lines)

### Message Format

Each recorded message is a JSON object with these fields:

```json
{
  "timestamp": "2026-01-12T23:45:42.940680618-07:00",
  "direction": "request",
  "message_type": "tool_call",
  "tool_name": "fs_read_file",
  "server_name": "filesystem",
  "message": {
    "method": "tools/call",
    "params": {
      "name": "fs_read_file",
      "arguments": {
        "path": "/etc/hosts"
      }
    }
  }
}
```

Fields:
- `timestamp`: ISO 8601 timestamp when message was captured
- `direction`: Either `"request"` or `"response"`
- `message_type`: Type of message (currently always `"tool_call"`)
- `tool_name`: Prefixed tool name (e.g., `fs_read_file`, `math_calculate`)
- `server_name`: Name of the upstream MCP server
- `message`: Complete JSON-RPC message payload

## What Gets Recorded

### Static Servers (from config.yaml)

All tool calls to servers defined in your configuration are recorded:

```yaml
servers:
  - name: "filesystem"
    prefix: "fs"
    command: "npx -y @mcp/filesystem /path"
```

Tool calls like `fs_read_file`, `fs_write_file`, etc. are captured.

### Dynamic Servers (via server_add)

Servers added at runtime are also recorded:

```json
// server_add request is recorded
{
  "direction": "request",
  "tool_name": "server_add",
  "server_name": "proxy",
  "message": {
    "name": "database",
    "command": "python3 db_server.py"
  }
}

// Subsequent tool calls to the new server are recorded
{
  "direction": "request",
  "tool_name": "database_query",
  "server_name": "database",
  ...
}
```

### Management Tools

All proxy management operations are recorded:
- `server_add` - Adding new servers
- `server_remove` - Removing servers
- `server_disconnect` - Disconnecting servers
- `server_reconnect` - Reconnecting with new commands
- `server_list` - Listing server status

### Error Responses

Failed requests and error responses are recorded:

```json
{
  "direction": "response",
  "tool_name": "fs_read_file",
  "message": {
    "content": [{
      "type": "text",
      "text": "Error: File not found"
    }],
    "isError": true
  }
}
```

## Playback Modes

### Client Mode

Replay recorded requests to test a server:

```bash
# Replay requests from recording to a server
mcp-debug --playback-client session.jsonl | ./your-mcp-server

# The server receives the recorded requests and can respond
```

**Use Cases**:
- Testing server implementations against real traffic
- Regression testing (compare responses to recorded ones)
- Debugging server behavior with specific requests

### Server Mode

Replay recorded responses to test a client:

```bash
# Start playback server (use with mcp-tui or other clients)
mcp-tui mcp-debug --playback-server session.jsonl
```

**Use Cases**:
- Testing client behavior with known responses
- Simulating server responses without running real servers
- UI/integration testing

## Common Workflows

### Debugging Tool Calls

1. Record a session where you encounter an issue:
   ```bash
   mcp-debug --proxy --config config.yaml --record debug-session.jsonl
   ```

2. Examine the recording to see exact requests/responses:
   ```bash
   # Pretty-print messages
   grep '"direction":"request"' debug-session.jsonl | jq .

   # See all tool names used
   grep -o '"tool_name":"[^"]*"' debug-session.jsonl | sort | uniq

   # Extract specific tool's messages
   grep '"tool_name":"fs_read_file"' debug-session.jsonl | jq .
   ```

3. Identify the problematic request and inspect the payload

### Regression Testing

1. Record a "golden" session with correct behavior:
   ```bash
   mcp-debug --proxy --config config.yaml --record golden.jsonl
   ```

2. After making changes, record a new session:
   ```bash
   mcp-debug --proxy --config config.yaml --record test.jsonl
   ```

3. Compare recordings:
   ```bash
   # Extract and compare responses
   grep '"direction":"response"' golden.jsonl > golden-responses.jsonl
   grep '"direction":"response"' test.jsonl > test-responses.jsonl
   diff golden-responses.jsonl test-responses.jsonl
   ```

### Documentation & Examples

1. Record typical usage patterns:
   ```bash
   mcp-debug --proxy --config config.yaml --record examples.jsonl
   ```

2. Extract example requests for documentation:
   ```bash
   # Get all unique tool calls
   jq -r 'select(.direction=="request") | .tool_name' examples.jsonl | sort | uniq

   # Extract example for specific tool
   jq 'select(.tool_name=="fs_read_file" and .direction=="request") | .message.params.arguments' examples.jsonl | head -1
   ```

## Tips & Best Practices

### File Naming

Use descriptive names for recordings:
```bash
--record 2026-01-12-filesystem-operations.jsonl
--record user-authentication-flow.jsonl
--record error-case-missing-file.jsonl
```

### Filtering Messages

Use `jq` to filter and analyze recordings:

```bash
# Count messages by direction
jq -s 'group_by(.direction) | map({direction: .[0].direction, count: length})' session.jsonl

# List all servers used
jq -r '.server_name' session.jsonl | sort | uniq

# Find slow operations (>1 second between request/response)
# (requires custom scripting to correlate timestamps)

# Extract error responses
jq 'select(.message.isError == true)' session.jsonl
```

### Recording Size

Recordings can grow large with many messages or large payloads:

```bash
# Check recording size
ls -lh session.jsonl

# Count messages
grep -c '"direction":' session.jsonl

# Compress old recordings
gzip session-2026-01-01.jsonl
```

### Sensitive Data

⚠️ **Warning**: Recordings contain complete message payloads including:
- File paths and contents
- API keys or credentials passed as arguments
- Database query results
- Any data transmitted through the proxy

**Best Practices**:
- Don't commit recordings to version control unless sanitized
- Use `.gitignore` to exclude `*.jsonl` files
- Redact sensitive data before sharing recordings
- Store recordings securely

### Continuous Recording

Enable automatic recording with environment variable:

```bash
export MCP_RECORD_FILE="session.jsonl"
mcp-debug --proxy --config config.yaml
```

This records all sessions to the specified file (overwrites on each run).

## Limitations

### Current Limitations

- **MCP Protocol Messages**: Only tool calls are recorded. MCP protocol messages (initialize, tools/list, ping) are not yet captured.
- **Multiple Formats**: Playback client/server modes have separate implementations and may not support all recorded message types.
- **No Filtering**: All messages are recorded; cannot exclude specific tools or servers.
- **Overwrite Mode**: Recording always overwrites the target file; no append mode.

### Future Enhancements

Planned improvements:
- Record full MCP protocol layer (initialize, notifications, etc.)
- Selective recording (filter by server, tool, or pattern)
- Append mode for long-running sessions
- Recording analysis tools (stats, timeline, visualization)
- Built-in diff/comparison tools

## Troubleshooting

### No Messages Recorded

If recording file is created but contains no messages:

1. **Verify recording is enabled**:
   ```bash
   # Should see "Recording enabled to: session.jsonl" in logs
   mcp-debug --proxy --config config.yaml --record session.jsonl --log /dev/stdout
   ```

2. **Check if tools are registered**:
   ```bash
   # Should see "Registered tool: server_tool_name" for each tool
   ```

3. **Verify tool calls are being made**:
   - Connect with mcp-tui and try calling tools
   - Check proxy logs for tool call activity

### Invalid JSON in Recording

If parsing fails:

```bash
# Find invalid lines
jq . session.jsonl 2>&1 | grep -A2 "parse error"

# Validate each line
awk 'NR > 3' session.jsonl | while read line; do echo "$line" | jq . >/dev/null || echo "Line $NR invalid"; done
```

### Playback Failures

If playback doesn't work as expected:

1. **Verify recording format**:
   ```bash
   # Header should be single-line JSON
   sed -n '3p' session.jsonl | jq .

   # Messages should have required fields
   sed -n '4p' session.jsonl | jq '{direction, tool_name, message}'
   ```

2. **Check server compatibility**:
   - Ensure server expects JSON-RPC format
   - Verify tool names match server's registered tools

## Related Documentation

- [README.md](../README.md) - Main project documentation
- [Configuration Guide](../README.md#configuration) - Server configuration
- [Environment Variables](../README.md#environment-variables) - Recording environment settings

## Support

For issues with recording:
1. Check the [GitHub Issues](https://github.com/ExactDoug/mcp-debug/issues)
2. Enable debug logging: `MCP_DEBUG=1 mcp-debug ...`
3. Include recording file samples (with sensitive data redacted) when reporting issues
