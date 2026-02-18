# Web Dashboard

MCP Debug includes a persistent local web dashboard for monitoring proxy activity and managing OAuth authentication for HTTP-stream upstream servers.

## Overview

### The Problem

mcp-debug runs as a background STDIO child process — typically spawned by Claude Desktop or another MCP client. It has no visible terminal. When connecting to remote HTTP-stream MCP servers that require OAuth 2.1 authentication:

- Browser launch attempts are invisible (no terminal to show prompts)
- Log messages go to files the user may never check
- Authentication failures surface as generic tool call errors
- There's no way to pre-authenticate before making tool calls

### The Solution

The dashboard provides a persistent web interface at `http://localhost:8100` that:

- Shows real-time connection and authentication status for all upstream servers
- Enables **pre-flight authentication** — complete OAuth flows before the first tool call
- Streams live events (tool calls, auth changes, errors) via Server-Sent Events
- Serves as the permanent OAuth callback endpoint, replacing the unreliable ephemeral callback server

### Design Principles

**Supplementary, not primary.** The dashboard is a convenience layer. Every piece of information shown on the dashboard is also available through the STDIO MCP tool interface (`server_list`). Headless and CI environments work without it.

**Dual-path authentication.** Two independent paths to authenticate, both storing tokens in the same persistent token store:

- **Path A (STDIO-triggered):** Tool call → 401 response → automatic OAuth flow → browser launch → callback → retry. This is the existing flow and remains unchanged.
- **Path B (Dashboard-triggered):** User opens dashboard → sees "Needs Auth" → clicks Authenticate → completes OAuth in browser → token stored → future tool calls just work.

If the user pre-authenticates via Path B, Path A's 401 flow is never triggered. If tokens expire and refresh fails, Path A kicks in as fallback.

## Quick Start

```bash
# Dashboard starts automatically with proxy mode
uvx mcp-debug --proxy --config config.yaml

# Open in your browser
open http://localhost:8100
```

The dashboard starts before any upstream server connections are established, ensuring the OAuth callback endpoint is ready from the beginning.

## Server Status Panel

The dashboard displays a card for each upstream server configured in your `config.yaml`:

| Field | Description |
|-------|-------------|
| **Name** | Server name from config (e.g., "datto-rmm") |
| **Transport** | `stdio` or `http` |
| **URL** | Remote server URL (HTTP servers only) |
| **Connection** | `Connected` (green) or `Disconnected` (red) |
| **Tools** | Number of tools registered from this server |
| **Auth Status** | Authentication state (see below) |

### Auth Status Badges

| Badge | Meaning |
|-------|---------|
| **Authenticated** (green) | Valid token, shows minutes until expiry |
| **Token Expired** (amber) | Token expired, refresh needed |
| **Needs Auth** (amber) | No token stored, OAuth flow required |
| *No badge* | STDIO server or no auth configured |

### Action Buttons

- **Authenticate** — Appears when status is "Needs Auth". Initiates OAuth flow.
- **Re-authenticate** — Appears when status is "Authenticated" or "Token Expired". Starts a fresh OAuth flow.
- **Revoke** — Appears when a token exists. Deletes the stored token after confirmation.

## Authentication Management

### Pre-flight Authentication

The recommended workflow for HTTP servers with OAuth:

1. Start mcp-debug with your config
2. Open `http://localhost:8100` in your browser
3. For each server showing "Needs Auth", click **Authenticate**
4. Complete the OAuth login in the browser tab that opens
5. Return to the dashboard — status should show "Authenticated"
6. Start using tools in your MCP client — no 401 interruptions

### OAuth Flow (Dashboard-Triggered)

```
User clicks "Authenticate"
    │
    ▼
Dashboard POSTs to /api/auth/{server}
    │
    ▼
Server prepares OAuth 2.1 PKCE flow:
  - Discovers authorization server metadata (RFC 8414)
  - Generates code_verifier + code_challenge (S256)
  - Registers pending state in callback registry
  - Optionally performs dynamic client registration (RFC 7591)
    │
    ▼
Returns auth_url to dashboard
    │
    ▼
Dashboard opens auth_url in new browser tab
    │
    ▼
User logs in at authorization server
    │
    ▼
Authorization server redirects to http://localhost:8100/callback?code=...&state=...
    │
    ▼
Callback handler matches state, sends code to waiting goroutine
    │
    ▼
Token exchange completes (PKCE code_verifier proves possession)
    │
    ▼
Token stored in persistent TokenStore
    │
    ▼
Dashboard SSE event: "OAuth flow completed"
```

### Token Revocation

Clicking **Revoke** sends a DELETE request to `/api/auth/{server}`, which clears the stored token. The server will require re-authentication on the next tool call.

## Live Event Feed

The dashboard streams real-time events via Server-Sent Events (SSE):

| Event Type | Color | Examples |
|-----------|-------|---------|
| `tool_call` | Blue | Tool invoked, tool response received |
| `auth` | Amber | OAuth flow started, token refreshed, token revoked |
| `connection` | Green | Server connected, server disconnected, server reconnected |
| `error` | Red | Connection failed, auth failed, tool call error |

### Auto-Reconnect

The SSE connection automatically reconnects after 3 seconds if the connection drops. A status indicator in the dashboard header shows:

- **Green dot** — Connected to event stream
- **Yellow dot** — Connecting / reconnecting
- **Red dot** — Disconnected

### Event Capacity

The event feed displays the most recent 200 events, with newest events at the top. Older events are automatically removed.

## API Reference

All endpoints are served from `http://localhost:{port}` (default port: 8100).

### GET /

Serves the dashboard single-page application (embedded HTML with inline CSS/JS).

### GET /api/servers

Returns all upstream servers with connection and auth status.

**Response:**
```json
{
  "servers": [
    {
      "name": "remote-api",
      "prefix": "api",
      "transport": "http",
      "url": "https://example.com/mcp",
      "connected": true,
      "tools": 12,
      "auth": {
        "type": "oauth",
        "status": "authenticated",
        "token_expires_in_minutes": 42,
        "scopes": "read write",
        "client_id": "abc123"
      }
    },
    {
      "name": "local-fs",
      "prefix": "fs",
      "transport": "stdio",
      "connected": true,
      "tools": 5,
      "auth": null
    }
  ]
}
```

### POST /api/auth/{server}

Initiates an OAuth flow for the named server. Returns the authorization URL.

**Headers:** `X-Requested-With: mcp-debug` (required, CSRF protection)

**Response:**
```json
{
  "auth_url": "https://auth.example.com/authorize?client_id=...&code_challenge=...",
  "server": "remote-api"
}
```

### DELETE /api/auth/{server}

Revokes (deletes) the stored token for the named server.

**Headers:** `X-Requested-With: mcp-debug` (required, CSRF protection)

**Response:**
```json
{
  "ok": "true",
  "server": "remote-api"
}
```

### GET /api/tokens/{server}

Returns token metadata for the named server. Never returns raw token values.

**Response:**
```json
{
  "server": "remote-api",
  "auth": {
    "type": "oauth",
    "status": "authenticated",
    "token_expires_in_minutes": 42,
    "scopes": "read write",
    "client_id": "abc123"
  }
}
```

### GET /api/events

Server-Sent Events stream. Sends real-time events as they occur.

**Content-Type:** `text/event-stream`

**Event format:**
```
data: {"type":"tool_call","timestamp":"2026-02-18T13:45:00Z","server":"remote-api","message":"Tool api_query invoked"}

data: {"type":"auth","timestamp":"2026-02-18T13:45:01Z","server":"remote-api","message":"OAuth flow started"}
```

An initial `{"type":"connected"}` event is sent immediately upon connection.

### GET /callback

OAuth callback endpoint. Receives the authorization code from the OAuth provider's redirect. This endpoint is shared between dashboard-triggered and STDIO-triggered flows.

**Query parameters:** `code` (authorization code), `state` (CSRF/flow identifier)

Returns an HTML page confirming success or failure.

## Configuration

### config.yaml

```yaml
dashboard:
  enabled: true   # default: true (dashboard starts with proxy)
  port: 8100      # default: 8100
```

Both fields are optional. If the `dashboard:` section is omitted entirely, the dashboard starts on port 8100 by default.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_DEBUG_NO_DASHBOARD` | *(unset)* | Set to `true` to disable the dashboard entirely |

### Disabling the Dashboard

Three ways to disable the dashboard:

```yaml
# 1. In config.yaml
dashboard:
  enabled: false
```

```bash
# 2. Via environment variable
export MCP_DEBUG_NO_DASHBOARD=true
```

```yaml
# 3. Omit HTTP servers entirely (dashboard still starts but has no auth functionality)
```

When disabled, the STDIO-triggered OAuth flow (Path A) continues to work using an ephemeral callback server.

## Security Model

### Localhost-Only Binding

The dashboard binds to `127.0.0.1:{port}`, never `0.0.0.0`. It is only accessible from the local machine.

### No Dashboard Authentication

The dashboard itself requires no login. This is intentional — it runs on localhost for the local user only. Requiring authentication on the authentication management tool would defeat its purpose.

### CSRF Protection

All mutation endpoints (`POST`, `DELETE`) require the `X-Requested-With: mcp-debug` header. Requests without this header receive a `403 Forbidden` response. This prevents cross-origin requests from malicious websites.

### Token Security

- The `/api/tokens/{server}` endpoint returns only metadata (status, expiry, scopes, client_id)
- Raw access tokens and refresh tokens are **never** exposed through the API
- Tokens are stored in the persistent TokenStore with `0600` file permissions

## Headless / CI Environments

For environments without a browser or display:

1. **Disable the dashboard:** Set `MCP_DEBUG_NO_DASHBOARD=true`
2. **Pre-configure tokens:** Use the `token_file` auth config option to provide pre-obtained tokens
3. **Bearer token auth:** Use static bearer tokens instead of OAuth flows

```yaml
# CI-friendly config — no browser needed
servers:
  - name: "api-server"
    prefix: "api"
    transport: "http"
    url: "https://example.com/mcp"
    auth:
      type: "bearer"
      token: "${CI_MCP_TOKEN}"
```

The STDIO-only OAuth flow (Path A) still functions without the dashboard, using an ephemeral callback server. However, this is unreliable in headless environments where `openBrowser()` will fail silently.

## Information Parity

Every piece of information on the dashboard is also available through the STDIO MCP tool interface:

| Information | MCP Tool | Dashboard |
|------------|----------|-----------|
| Server connection status | `server_list` response | Server Connection Panel |
| Auth status & token expiry | `server_list` → `auth` field | Auth status badge |
| Auth needed indicator | Tool call error message | "Needs Auth" badge + button |
| Auth events | Tool call error messages | Live Event Feed |
| Tool call activity | Tool call results (primary) | Live Event Feed (supplementary) |

The dashboard provides a more user-friendly view of this same data, but does not hold any information that isn't also available to the connected MCP client.

## Troubleshooting

### Port Already in Use

```
dashboard server failed to bind to 127.0.0.1:8100: listen tcp 127.0.0.1:8100: bind: address already in use
```

Another process (possibly a previous mcp-debug instance) is using port 8100. Either stop the other process or configure a different port:

```yaml
dashboard:
  port: 8200
```

### Dashboard Not Loading

1. Verify the proxy is running: `ps aux | grep mcp-debug`
2. Check logs for "Dashboard available at http://localhost:8100"
3. Ensure you're accessing `http://` not `https://` (the dashboard does not use TLS)
4. Try a different browser or clear cache

### OAuth Callback Not Received

If clicking "Authenticate" opens the browser but the dashboard never updates:

1. Check that the OAuth provider's redirect URI matches `http://localhost:{port}/callback`
2. Verify the authorization server allows `http://localhost` redirect URIs
3. Check for browser popup blockers preventing the OAuth window
4. Look for errors in the Live Event Feed

### SSE Connection Dropping

The SSE connection should auto-reconnect after 3 seconds. If events stop appearing:

1. Check the SSE status indicator (colored dot in the header)
2. Refresh the page to re-establish the connection
3. Verify the proxy process is still running

## Related Documentation

- [README.md](../README.md) — Main project documentation
- [Recording Documentation](RECORDING.md) — Session recording and playback
- [Environment Variable Inheritance](../DRAFT_ENV_INHERITANCE.md) — Env var security for upstream servers
