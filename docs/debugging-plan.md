# Debugging Plan: "client not connected" After server_reconnect

## Problem Statement

After calling `server_reconnect`, the proxy says "Server now connected and tools updated", but subsequent tool calls fail with "[datto-rmm] client not connected" error.

## Evidence from Logs

**Timeline from mcp-session.jsonl**:
```
04:14:32.190 - server_reconnect REQUEST
04:14:33.083 - server_reconnect RESPONSE: "Reconnected... Server now connected"
04:14:36.557 - drmm_account_devices REQUEST (3 seconds later)
04:14:36.562 - drmm_account_devices RESPONSE: ERROR "[datto-rmm] client not connected"
```

**Timeline from mcp-debug.log**:
```
04:14:32.196 - Reconnecting server 'datto-rmm' with STORED configuration
04:14:33.082 - Added client 'datto-rmm' to proxy server's client list
04:14:33.083 - Server 'datto-rmm' marked as connected
```

## Root Cause Hypothesis

The `server_reconnect` handler says the server is connected, but the StdioClient's internal `c.connected` flag is `false`. This suggests:

1. **Race condition**: The `c.connected` flag is being set/read without proper synchronization
2. **State inconsistency**: `serverInfo.IsConnected` (wrapper) is true but `client.connected` (internal) is false
3. **Client replacement issue**: The new client isn't properly initialized

## Bug Found: CallTool Doesn't Hold Mutex

**Location**: `client/stdio_client.go:169`

```go
func (c *StdioClient) CallTool(...) (*CallToolResult, error) {
    if !c.connected {  // ❌ NO MUTEX - reading without lock!
        return nil, fmt.Errorf("client not connected")
    }
    ...
}
```

**Problem**: `CallTool` checks `c.connected` without holding `c.mu`, so it can read stale/incorrect values.

**Compare with Initialize()** (line 117):
```go
func (c *StdioClient) Initialize(...) (*InitializeResult, error) {
    if !c.connected {  // ❌ ALSO NO MUTEX
        return nil, fmt.Errorf("client not connected")
    }
    ...
}
```

Both `CallTool` and `Initialize` check `connected` without the mutex, while:
- `Connect()` sets `connected=true` under `c.mu.Lock()` (line 111)
- `Close()` sets `connected=false` under `c.mu.Lock()` (line 235)
- `sendRequest()` checks `connected` under `c.mu.Lock()` (lines 249-253)

## Implementation Plan

### Step 1: Add Debug Logging (IN PROGRESS)

Add logging to track `connected` flag state transitions:

**File**: `client/stdio_client.go`

1. ✅ Log when Connect() sets connected=true (line 111)
2. ✅ Log when CallTool checks connected (line 169)
3. ⏳ Log when Close() sets connected=false (line 235)

### Step 2: Fix CallTool Mutex Bug (✅ DONE)

Fixed CallTool to read `connected` under mutex protection (matching the pattern in sendRequest).

### Step 3: Fix Initialize Mutex Bug (PENDING)

Need to also fix Initialize() to read `connected` under mutex:

```go
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
    // Check connected state with proper mutex
    c.mu.Lock()
    connected := c.connected
    c.mu.Unlock()

    if !connected {
        return nil, fmt.Errorf("client not connected")
    }
    ...
}
```

### Step 4: Fix ListTools Mutex Bug (PENDING)

Check if ListTools also has the same issue (line 141):

```go
func (c *StdioClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
    if !c.connected {  // ❌ PROBABLY NO MUTEX
        return nil, fmt.Errorf("client not connected")
    }
    ...
}
```

## Testing Plan

### Test 1: Add Debug Logging and Rebuild

```bash
# Build with debug logging
go build -o ./bin/mcp-debug .

# Kill existing instances
pkill -f mcp-debug

# Start new instance (Claude Code will auto-restart)
# Then test disconnect/reconnect cycle

# Watch logs in real-time
tail -f /mnt/c/dev/projects/github/datto_rmm_smart_mcp/mcp-debug.log
```

**Expected output** from debug logs:
```
[DEBUG] StdioClient.Connect() SUCCESS: datto-rmm - connected=true
[DEBUG] CallTool(datto-rmm, drmm_account_devices): connected=true
```

**If bug is confirmed**, we'll see:
```
[DEBUG] StdioClient.Connect() SUCCESS: datto-rmm - connected=true
[DEBUG] CallTool(datto-rmm, drmm_account_devices): connected=false  ← WRONG!
```

### Test 2: After Fixing Mutexes

```bash
# Test disconnect/reconnect cycle
1. Make tool call - should work
2. server_disconnect
3. server_reconnect
4. Make tool call - should work WITHOUT errors
5. Repeat 10 times
```

### Test 3: Race Detection

```bash
# Run with race detector
go test -race ./...
```

## Success Criteria

- ✅ No "[datto-rmm] client not connected" errors after successful reconnect
- ✅ Debug logs show connected=true when CallTool is invoked
- ✅ No data races detected by race detector
- ✅ Disconnect/reconnect cycle works reliably

## Files to Modify

1. `client/stdio_client.go`:
   - ✅ Add mutex protection to CallTool (line 169)
   - ⏳ Add mutex protection to Initialize (line 117)
   - ⏳ Add mutex protection to ListTools (line 141)
   - ⏳ Add debug logging to Connect, CallTool, Close

## Root Cause Analysis

The fundamental issue is **inconsistent mutex usage** for the `connected` field:

- **Protected** (correct): Connect(), Close(), sendRequest()
- **Unprotected** (BUG): CallTool(), Initialize(), ListTools()

This creates race conditions where:
1. Thread A: Connect() sets connected=true, releases mutex
2. Thread B: CallTool() reads connected (no mutex) → may read stale false value
3. Tool call fails even though client is actually connected

The fix is to make ALL accesses to `connected` use the mutex, matching the pattern already used in sendRequest().

## UPDATE: Mutex Fixes Completed But Issue Persists

**Status**: All mutex protection and debug logging has been implemented (commit 81c9f04), but tool calls still fail after server_reconnect.

**New Debug Evidence** (from user testing after mutex fixes):
```
04:33:54 - [DEBUG] StdioClient.Connect() SUCCESS: datto-rmm - connected=true  ← NEW CLIENT
04:34:57 - [DEBUG] CallTool(datto-rmm, account_devices): connected=false      ← OLD CLIENT
```

**Critical Discovery**: The debug logs prove that CallTool() is being invoked on the OLD disconnected client, not the NEW connected client created by server_reconnect.

## Actual Root Cause: Handler Closure Captures Stale Client Reference

The mutex hypothesis was incorrect. The real bug is in how tool handlers are registered:

### Static Servers (BROKEN)
Static servers from config.yaml use `proxy.CreateProxyHandler()` which captures the mcpClient reference in a closure at initialization time:

**File**: `integration/proxy_server.go` lines 102-115
```go
for _, tool := range result.Tools {
    // Create proxy handler - CAPTURES mcpClient IN CLOSURE
    handler := proxy.CreateProxyHandler(mcpClient, tool, ...)
    p.mcpServer.AddTool(mcpTool, handler)
}
```

**File**: `proxy/handler.go` line 20
```go
func CreateProxyHandler(mcpClient client.MCPClient, ...) {
    return func(...) {
        // mcpClient is CAPTURED in closure - IMMUTABLE
        result, err := mcpClient.CallTool(...)
    }
}
```

When server_reconnect creates a new client, the handler closure still references the old captured client.

### Dynamic Servers (WORKING)
Dynamic servers from server_add use `createDynamicProxyHandler()` which looks up the current client at call time:

**File**: `integration/dynamic_wrapper.go` lines 768-780
```go
func (w *DynamicWrapper) createDynamicProxyHandler(serverName, originalToolName string) {
    return func(...) {
        // Looks up CURRENT client at call time
        w.mu.RLock()
        serverInfo := w.dynamicServers[serverName]
        client := serverInfo.Client  // Gets current client
        w.mu.RUnlock()

        client.CallTool(...)  // Uses current client
    }
}
```

This pattern doesn't capture the client, so reconnect automatically provides the new client.

## Solution

Make static servers use the dynamic handler pattern:
1. Remove static handler creation from proxy_server.go (lines 102-115)
2. Add dynamic handler creation for all tools in dynamic_wrapper.go Start()
3. All handlers will look up current client from dynamicServers map at call time

This eliminates closure-captured client references and enables hot-swapping for all servers.

**Next Commit**: Implement handler pattern fix (see plan in .claude/plans/quizzical-questing-phoenix.md)
