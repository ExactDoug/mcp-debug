# MCP Debug

A debugging and development tool for [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers.

## Installation

```bash
# Run directly with npx (recommended)
npx mcp-server-debug --help

# Or install globally
npm install -g mcp-server-debug
mcp-debug --help
```

## Features

- **Hot-Swap Development** - Replace server binaries without disconnecting MCP clients
- **Session Recording & Playback** - Record JSON-RPC traffic for debugging and testing
- **Development Proxy** - Multi-server aggregation with tool prefixing
- **Dynamic Server Management** - Add/remove servers at runtime

## Quick Start

```bash
# Start proxy with config
npx mcp-server-debug --proxy --config config.yaml

# Record a session
npx mcp-server-debug --proxy --config config.yaml --record session.jsonl

# Playback recorded requests
npx mcp-server-debug --playback-client session.jsonl | ./your-mcp-server
```

## Programmatic Usage

```javascript
const { binaryPath } = require("mcp-debug");
const { spawn } = require("child_process");

const child = spawn(binaryPath, ["--proxy", "--config", "config.yaml"]);
```

## Documentation

See the [GitHub repository](https://github.com/standardbeagle/mcp-debug) for full documentation.

## License

MIT License
