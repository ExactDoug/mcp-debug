# MCP Debug Test Suite

This directory contains all tests for the MCP Debug project.

## Directory Structure

- `integration/` - Integration tests that test the full system
  - `test-proxy-calls.py` - Tests proxy server tool calls
  - `test-dynamic-registration.py` - Tests dynamic tool registration
  - `test-lifecycle.py` - Tests server lifecycle management
  - `test-simple-dynamic.py` - Simple dynamic registration test
  - `test-updated-tools.py` - Tests tool updates

- `config-fixtures/` - Test configuration files
  - `test-config.yaml` - Basic test configuration
  - `test-multi-config.yaml` - Multi-server configuration
  - `test-lifecycle-config.yaml` - Lifecycle testing configuration
  - `test-updated-config.yaml` - Updated server configuration
  - `test-dynamic-config.yaml` - Dynamic registration configuration
  - `test-empty-config.yaml` - Empty configuration for testing
  - `test-filesystem-config.yaml` - Filesystem server configuration

- `scripts/` - Test utility scripts
  - `test-playback.sh` - Playback testing script

## Running Tests

### Integration Tests

```bash
# Run all integration tests
./run-all-tests.sh

# Run individual tests
cd integration
python3 test-proxy-calls.py
python3 test-dynamic-registration.py
python3 test-lifecycle.py
```

### Go Unit Tests

```bash
# Run all Go tests
go test ./...

# Run with verbose output
go test -v ./...
```

## Test Servers

The project includes test servers in `/test-servers/`:
- `math-server` - Simple math operations
- `file-server` - File operations
- `lifecycle-server-v1/v2` - For testing server upgrades

Build test servers before running tests:
```bash
cd test-servers
go build -o math-server math-server.go
go build -o file-server file-server.go
```
