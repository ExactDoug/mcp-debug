# Test Organization Summary

## Directory Structure

```
tests/
├── README.md              # Test documentation
├── ORGANIZATION.md        # This file
├── run-all-tests.sh       # Master test runner
├── integration/           # Integration tests
│   ├── test-proxy-calls.py
│   ├── test-dynamic-registration.py
│   ├── test-lifecycle.py
│   ├── test-simple-dynamic.py
│   └── test-updated-tools.py
├── config-fixtures/       # Test configuration files
│   ├── test-config.yaml
│   ├── test-multi-config.yaml
│   ├── test-lifecycle-config.yaml
│   ├── test-updated-config.yaml
│   ├── test-dynamic-config.yaml
│   ├── test-empty-config.yaml
│   └── test-filesystem-config.yaml
└── scripts/               # Test utility scripts
    └── test-playback.sh
```

## Test Categories

### Integration Tests (`integration/`)

Python-based integration tests that verify the full MCP Debug system:

- **test-proxy-calls.py** - Verifies tool forwarding through the proxy
- **test-dynamic-registration.py** - Tests adding/removing servers at runtime
- **test-lifecycle.py** - Tests server connect/disconnect/reconnect workflows
- **test-simple-dynamic.py** - Basic dynamic registration verification
- **test-updated-tools.py** - Tests tool updates after server changes

### Configuration Fixtures (`config-fixtures/`)

YAML configuration files used by integration tests to set up various server scenarios.

## Running Tests

From the `tests/` directory:

```bash
# Run all tests
./run-all-tests.sh

# Run specific test category
cd integration && python3 test-proxy-calls.py

# Run Go unit tests
go test ./...
```

## Notes

- The MCP SDK (mark3labs/mcp-go v0.43+) supports dynamic tool registration via `AddTool()`, `DeleteTools()`, and `SetTools()` methods
- Tests assume the main binary is built at `../../mcp-debug`
- Config fixtures use relative paths from the integration directory
