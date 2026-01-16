# MCP Debug

A debugging and development tool for [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers.

[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![MCP Spec](https://img.shields.io/badge/MCP-2025--06--18-green.svg)](https://modelcontextprotocol.io/specification/2025-06-18)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![PyPI](https://img.shields.io/pypi/v/mcp-debug.svg)](https://pypi.org/project/mcp-debug/)
[![npm](https://img.shields.io/npm/v/mcp-debug.svg)](https://www.npmjs.com/package/mcp-debug)

MCP Debug enables rapid development and testing of MCP servers with hot-swapping, session recording, and automated playback testing.

## Features

### Hot-Swap Development
- Replace server binaries without disconnecting MCP clients
- Add/remove servers dynamically during development
- Tool name preservation - same interface, new implementation
- Graceful disconnect/reconnect workflow for binary replacement

### Session Recording & Playback
- Record JSON-RPC traffic for debugging and documentation
- Records all tool calls (static and dynamic servers)
- Records management operations (server_add, etc.)
- Tool responses include recording metadata when active
- Playback client mode - replay requests to test servers
- Playback server mode - replay responses to test clients
- Regression testing with recorded sessions
- **[📖 Recording Documentation](docs/RECORDING.md)** - Complete recording guide

### Development Proxy
- Multi-server aggregation with tool prefixing
- Real-time connection monitoring
- Management API for server lifecycle control
- Comprehensive logging

## Installation

```bash
# Using uvx (Python - recommended)
uvx mcp-debug --help

# Using npx (Node.js)
npx @standardbeagle/mcp-debug --help

# Or install globally
pip install mcp-debug              # Python
npm install -g @standardbeagle/mcp-debug  # Node.js

# Or build from source
go install github.com/standardbeagle/mcp-debug@latest
```

## Quick Start

```bash
# Start proxy with a config file
uvx mcp-debug --proxy --config config.yaml

# Or with mcp-tui for interactive testing
mcp-tui uvx mcp-debug --proxy --config config.yaml
```

## Usage

### Proxy Mode

```bash
# Basic proxy
uvx mcp-debug --proxy --config config.yaml

# With recording
uvx mcp-debug --proxy --config config.yaml --record session.jsonl

# With custom log file
uvx mcp-debug --proxy --config config.yaml --log /tmp/debug.log
```

**Management Tools:**
- `server_add` - Add a server: `{name: "fs", command: "npx -y @mcp/filesystem /path"}`
- `server_remove` - Remove server completely
- `server_disconnect` - Disconnect server (tools return errors)
- `server_reconnect` - Reconnect with optional new command (preserves config if omitted)
- `server_list` - Show all servers and status

### Playback Modes

```bash
# Replay recorded requests to test a server
uvx mcp-debug --playback-client session.jsonl | ./your-mcp-server

# Replay recorded responses to test a client
mcp-tui uvx mcp-debug --playback-server session.jsonl
```

**See [Recording Documentation](docs/RECORDING.md) for detailed recording format, workflows, and examples.**

## Configuration

```yaml
# config.yaml
servers:
  - name: "filesystem"
    prefix: "fs"
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/filesystem", "/home/user"]
    timeout: "30s"

proxy:
  healthCheckInterval: "30s"
  connectionTimeout: "10s"
  maxRetries: 3
```

### Environment Variables

```bash
MCP_LOG_FILE="/tmp/mcp-debug.log"  # Log location
MCP_DEBUG=1                         # Enable debug logging
MCP_RECORD_FILE="session.jsonl"     # Auto-record sessions
MCP_CONFIG_PATH="./config.yaml"     # Default config
```

## Environment Variable Inheritance (DRAFT)

> **⚠️ DRAFT**: This feature is fully implemented but not yet validated with real-world MCP servers. See [DRAFT_ENV_INHERITANCE.md](DRAFT_ENV_INHERITANCE.md) for complete documentation.

mcp-debug implements a tier-based environment variable inheritance system to prevent accidental leakage of sensitive values (credentials, tokens, SSH agent sockets) to upstream MCP servers.

### Security-First Design

By default, only **Tier 1 baseline variables** (PATH, HOME, USER, SHELL, locale, TMPDIR) are inherited. This prevents inadvertent exposure of:
- Cloud provider credentials (AWS_ACCESS_KEY_ID, AZURE_CLIENT_SECRET)
- Authentication tokens (GITHUB_TOKEN, ANTHROPIC_API_KEY)
- SSH agent sockets (SSH_AUTH_SOCK)
- Development secrets and corporate credentials

You can control inheritance behavior per-server or set proxy-wide defaults.

### Quick Example

```yaml
# Secure by default - only baseline variables
servers:
  - name: untrusted-server
    transport: stdio
    command: python3
    args: ["-m", "experimental_server"]
    # No inherit block = tier1 mode (secure default)

# Explicit control for trusted servers
  - name: trusted-server
    transport: stdio
    command: python3
    args: ["-m", "my_server"]
    inherit:
      mode: tier1+tier2  # Add network/TLS variables
      extra: ["PYTHONPATH", "VIRTUAL_ENV"]  # Explicitly add needed vars
      prefix: ["MY_APP_"]  # Include all MY_APP_* variables
      deny: ["SSH_AUTH_SOCK"]  # Block specific variables
```

### Inheritance Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `none` | No inheritance (only explicit `env:` values) | Maximum isolation |
| `tier1` | Baseline variables only (DEFAULT) | Most servers |
| `tier1+tier2` | Baseline + network/TLS variables | Servers making HTTPS requests |
| `all` | Inherit everything (with optional deny list) | Fully trusted servers |

### Tier Definitions

**Tier 1 (Baseline)**: PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR, TEMP, TMP

**Tier 2 (Network/TLS)**: SSL_CERT_FILE, SSL_CERT_DIR, REQUESTS_CA_BUNDLE, CURL_CA_BUNDLE, NODE_EXTRA_CA_CERTS

**Implicit Denylist**: HTTP_PROXY, HTTPS_PROXY, http_proxy, https_proxy, NO_PROXY, no_proxy (httpoxy mitigation)

### Complete Documentation

For complete documentation including all configuration options, security rationale, troubleshooting, and advanced use cases, see [DRAFT_ENV_INHERITANCE.md](DRAFT_ENV_INHERITANCE.md).

## Development Workflow

### Dynamic Servers

```bash
# 1. Start with empty config
mcp-tui uvx mcp-debug --proxy --config empty-config.yaml

# 2. Add your server dynamically
server_add: {name: myserver, command: ./my-server-v1}

# 3. Test tools: myserver_read_file, myserver_process, etc.

# 4. Make changes and rebuild
go build -o my-server-v2

# 5. Hot-swap the server
server_disconnect: {name: myserver}
server_reconnect: {name: myserver, command: ./my-server-v2}

# 6. Same tools work immediately with new implementation!
```

### Static Servers (from config.yaml)

**NEW:** Static servers can now be hot-swapped while preserving all configuration including environment variables!

```bash
# config.yaml
servers:
  - name: "my-server"
    command: "python3"
    args: ["-m", "my_server"]
    env:
      API_KEY: "${MY_API_KEY}"
      API_SECRET: "${MY_SECRET}"
    timeout: "300s"

# 1. Start proxy with config
mcp-tui uvx mcp-debug --proxy --config config.yaml

# 2. Make changes to server code

# 3. Hot-swap WITHOUT exposing secrets
server_disconnect: {name: "my-server"}
server_reconnect: {name: "my-server"}  # No command = uses stored config

# 4. Server reconnects with ALL env vars preserved!
```

**Key Benefits:**
- Environment variables (including secrets) never exposed to MCP client
- Inheritance settings preserved
- Timeout and other config preserved
- Same tools work immediately with new implementation

**Alternative:** Provide new command to replace configuration:
```bash
server_reconnect: {name: "my-server", command: "./new-server"}
# Warning: This loses env vars and other config from config.yaml
```

## CLI Commands

```bash
uvx mcp-debug --help              # Show help
uvx mcp-debug --version           # Show version
uvx mcp-debug config init         # Create default config
uvx mcp-debug config show         # Show current config
uvx mcp-debug config validate     # Validate config file
uvx mcp-debug env list            # List environment variables
uvx mcp-debug env check           # Check required env vars
uvx mcp-debug tools list          # List tools with details
```

## Project Structure

```
mcp-debug/
├── main.go              # CLI entry point
├── config/              # Configuration loading
├── client/              # MCP client implementation
├── integration/         # Proxy server and wrapper
├── discovery/           # Tool discovery
├── proxy/               # Request forwarding
├── playback/            # Recording and playback
└── test-servers/        # Example MCP servers
```

## Building

```bash
# Development build
go build -o mcp-debug .

# Production build with version info
go build -ldflags "-X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.GitCommit=$(git rev-parse HEAD)" -o mcp-debug .

# Run tests
go test ./...
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
