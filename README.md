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
- Playback client mode - replay requests to test servers
- Playback server mode - replay responses to test clients
- Regression testing with recorded sessions

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
npx mcp-server-debug --help

# Or install globally
pip install mcp-debug         # Python
npm install -g mcp-server-debug # Node.js

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
- `server_reconnect` - Reconnect with new command
- `server_list` - Show all servers and status

### Playback Modes

```bash
# Replay recorded requests to test a server
uvx mcp-debug --playback-client session.jsonl | ./your-mcp-server

# Replay recorded responses to test a client
mcp-tui uvx mcp-debug --playback-server session.jsonl
```

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

## Development Workflow

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
