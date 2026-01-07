# MCP Debug

<!-- mcp-name: io.github.standardbeagle/mcp-debug -->

A debugging and development tool for [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers.

## Installation

```bash
# Run directly with uvx (recommended)
uvx mcp-debug --help

# Or install with pip
pip install mcp-debug
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
uvx mcp-debug --proxy --config config.yaml

# Record a session
uvx mcp-debug --proxy --config config.yaml --record session.jsonl

# Playback recorded requests
uvx mcp-debug --playback-client session.jsonl | ./your-mcp-server
```

## Documentation

See the [GitHub repository](https://github.com/standardbeagle/mcp-debug) for full documentation.

## License

MIT License
