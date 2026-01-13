# Environment Variable Inheritance (DRAFT)

> **⚠️ DRAFT DOCUMENTATION**: This feature is fully implemented and tested but has not yet been validated with real-world MCP servers. Documentation may be updated based on real-world testing feedback.

**Status**: Feature implemented in commit 49f5581
**Branch**: feature/env-selective-inheritance
**Last Updated**: 2026-01-13

---

## Overview

mcp-debug implements a **tier-based environment variable inheritance system** that provides fine-grained control over which environment variables are passed from the parent process to MCP server child processes.

This feature addresses a critical security concern: preventing accidental leakage of sensitive environment variables (credentials, tokens, SSH agent sockets, API keys) to upstream MCP servers, especially experimental or third-party servers that may not have been thoroughly vetted.

### Why This Matters

When running MCP servers as child processes, the default behavior in most systems is to inherit all environment variables from the parent process. This can inadvertently expose:

- **Cloud provider credentials** (AWS_ACCESS_KEY_ID, AZURE_CLIENT_SECRET, etc.)
- **Authentication tokens** (GITHUB_TOKEN, ANTHROPIC_API_KEY, etc.)
- **SSH agent sockets** (SSH_AUTH_SOCK)
- **Development secrets** (.env file variables)
- **Corporate credentials** loaded into your shell

With selective inheritance, you explicitly control what gets shared, following the principle of least privilege.

---

## Security Rationale

### The Problem

The traditional "inherit everything" approach using `os.Environ()` is convenient but dangerous:

```yaml
servers:
  - name: experimental-server
    command: python3
    args: ["-m", "untrusted_mcp_server"]
    # This server now has access to ALL your environment variables!
```

### The Solution

Tier-based inheritance with explicit control:

```yaml
servers:
  - name: experimental-server
    command: python3
    args: ["-m", "untrusted_mcp_server"]
    inherit:
      mode: tier1  # Only baseline variables (default)
      extra: ["PYTHONPATH"]  # Explicitly add what's needed
      deny: ["SSH_AUTH_SOCK"]  # Explicitly block sensitive vars
```

### Security Benefits

1. **Default-secure**: By default, only Tier 1 baseline variables are inherited
2. **Tier 1 baseline always available**: Prevents broken environments (PATH, HOME always available unless explicitly denied)
3. **Explicit opt-in for additional vars**: Variables beyond Tier 1 must be explicitly requested via `extra` or `prefix`
4. **Auditable**: Configuration files show exactly what each server receives
5. **Defense in depth**: Multiple layers (tiers, deny lists, implicit blocks)
6. **httpoxy mitigation**: HTTP_PROXY (uppercase) blocked by default to prevent httpoxy attacks
7. **No mode to inherit everything**: Even `mode: all` only inherits Tier 1 + Tier 2, not all parent variables

---

## Tier System

The inheritance system is organized into two tiers plus an implicit denylist.

### Tier 1: Baseline Variables (Always Inherited)

These are minimal essential variables that most programs need to function correctly. **Always inherited unless explicitly denied.**

| Variable | Purpose |
|----------|---------|
| `PATH` | Executable search path |
| `HOME` | User home directory |
| `USER` | Current username |
| `SHELL` | User's shell |
| `LANG` | Primary locale setting |
| `LC_ALL` | Locale override |
| `TZ` | Timezone |
| `TMPDIR` | Temporary directory (Unix) |
| `TEMP` | Temporary directory (Windows) |
| `TMP` | Temporary directory (Windows) |

**Rationale**: These variables are required for basic process functionality and rarely contain secrets. Excluding them would break most servers. By making Tier 1 the guaranteed baseline, we prevent accidentally creating broken environments while still maintaining security.

**Blocking Tier 1 variables**: If you need to block a Tier 1 variable (e.g., for maximum isolation testing), use the `deny:` list.

### Tier 2: Network and TLS Variables

These are helpful for servers that make network connections or need TLS certificate validation. Inherited when `mode: tier1+tier2` or `mode: all` is set.

| Variable | Purpose |
|----------|---------|
| `SSL_CERT_FILE` | Path to TLS certificate bundle |
| `SSL_CERT_DIR` | Directory containing TLS certificates |
| `REQUESTS_CA_BUNDLE` | Python requests library CA bundle |
| `CURL_CA_BUNDLE` | curl CA bundle path |
| `NODE_EXTRA_CA_CERTS` | Node.js additional CA certificates |

**Rationale**: Enterprise environments often require custom CA bundles for TLS inspection/interception. These variables enable servers to validate certificates in corporate networks.

**Note**: Proxy variables (http_proxy, https_proxy) are in the **implicit denylist** by default due to security concerns (see below).

### Implicit Denylist

These variables are **blocked by default** and require explicit opt-in via `extra` + `allow_denied_if_explicit: true`.

| Variable | Reason |
|----------|--------|
| `HTTP_PROXY` | httpoxy vulnerability (uppercase variant) |
| `HTTPS_PROXY` | httpoxy vulnerability (uppercase variant) |
| `http_proxy` | Potential security risk |
| `https_proxy` | Potential security risk |
| `NO_PROXY` | Can leak internal network topology |
| `no_proxy` | Can leak internal network topology |

**httpoxy vulnerability**: The uppercase `HTTP_PROXY` variable can be set by attackers via HTTP headers in CGI-like environments, causing the application to proxy requests through attacker-controlled servers. See [httpoxy.org](https://httpoxy.org/) for details.

**Overriding the denylist**: If you genuinely need proxy variables, you must explicitly request them:

```yaml
inherit:
  mode: tier1+tier2
  extra: ["http_proxy", "https_proxy"]
  allow_denied_if_explicit: true
```

---

## Inheritance Modes

The `mode` setting controls the base set of variables to inherit. **All modes inherit at least Tier 1 baseline variables** (unless explicitly denied).

### `mode: none` or `mode: tier1` (DEFAULT)

**Inherit Tier 1 baseline variables only**, plus any variables from `extra` and `prefix`.

```yaml
servers:
  - name: python-server
    command: python3
    args: ["-m", "my_server"]
    inherit:
      mode: tier1  # Can be omitted (it's the default)
      extra: ["PYTHONPATH", "VIRTUAL_ENV"]
```

**Inherited**: PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR, TEMP, TMP

**Use cases**:
- Default for most servers
- Good balance of functionality and security
- Prevents most secret leakage

**Note**: `mode: none` and `mode: tier1` behave identically - both inherit Tier 1 baseline. The "none" naming is kept for backward compatibility but is somewhat misleading. To achieve true isolation (no automatic inheritance), use `mode: tier1` with an explicit `deny:` list containing all Tier 1 variables.

### `mode: tier1+tier2`

**Inherit Tier 1 + Tier 2 variables**, plus any from `extra` and `prefix`.

```yaml
servers:
  - name: api-server
    command: node
    args: ["server.js"]
    inherit:
      mode: tier1+tier2
      extra: ["NODE_ENV"]
```

**Inherited**: All Tier 1 variables + SSL_CERT_FILE, SSL_CERT_DIR, REQUESTS_CA_BUNDLE, CURL_CA_BUNDLE, NODE_EXTRA_CA_CERTS

**Use cases**:
- Servers making HTTPS requests
- Enterprise environments with custom CA bundles
- Servers needing TLS certificate validation

### `mode: all`

**Same as `mode: tier1+tier2`**. Inherits Tier 1 + Tier 2 variables, plus any from `extra` and `prefix`.

```yaml
servers:
  - name: trusted-server
    command: ./my-trusted-app
    inherit:
      mode: all
      extra: ["MY_APP_CONFIG", "DATABASE_URL"]
```

**Inherited**: Tier 1 + Tier 2 variables, plus anything in `extra` or matching `prefix` patterns

**Use cases**:
- Servers needing network/TLS capabilities
- When you want a recognizable name that implies "maximum compatibility"
- Backward compatibility with configurations expecting "all" to mean "Tier 1 + Tier 2"

**⚠️ Important**: Despite the name, `mode: all` does **NOT** inherit all parent environment variables. It's equivalent to `tier1+tier2`. This is a security-first design decision to prevent accidental secret leakage. To inherit additional variables, you must explicitly list them in `extra:` or `prefix:`.

---

## Configuration Options

### Complete Schema

```yaml
# Proxy-level defaults (optional)
proxy:
  healthCheckInterval: "30s"
  connectionTimeout: "10s"

inherit:  # Applied to all servers unless overridden
  mode: "tier1"                      # none | tier1 | tier1+tier2 | all
  extra: []                          # Additional variable names
  prefix: []                         # Variable name prefixes to match
  deny: []                           # Variables to block
  allow_denied_if_explicit: false    # Allow denied vars if in 'extra'

servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "my_mcp_server"]

    # Server-specific inheritance (overrides proxy defaults)
    inherit:
      mode: "tier1+tier2"
      extra: ["PYTHONPATH", "VIRTUAL_ENV"]
      prefix: ["MY_APP_", "DATTO_"]
      deny: ["SSH_AUTH_SOCK"]
      allow_denied_if_explicit: true

    # Explicit overrides (always applied, never denied)
    env:
      MY_CONFIG: "production"
      API_KEY: "${MY_API_KEY}"  # Expanded from parent env
```

### Field Reference

#### `mode` (string)

Controls base inheritance behavior.

- **Type**: String enum
- **Values**: `none`, `tier1`, `tier1+tier2`, `all`
- **Default**: `tier1` (if not specified)
- **Example**: `mode: "tier1+tier2"`
- **Note**: `none` and `tier1` are equivalent; `all` is equivalent to `tier1+tier2`

#### `extra` (array of strings)

Additional variable names to inherit beyond the tier definition.

- **Type**: Array of strings
- **Default**: Empty array
- **Case-sensitive**: Variable names are matched case-sensitively on Unix, case-insensitively on Windows
- **Example**: `extra: ["PYTHONPATH", "VIRTUAL_ENV", "NODE_ENV"]`

Variables listed in `extra` can bypass the implicit denylist if `allow_denied_if_explicit: true` is set.

#### `prefix` (array of strings)

Inherit all variables whose names start with these prefixes.

- **Type**: Array of strings
- **Default**: Empty array
- **Case-sensitive**: Prefix matching follows platform conventions
- **Example**: `prefix: ["MY_APP_", "DATTO_", "CUSTOM_"]`

Useful for inheriting groups of related variables (e.g., all configuration for a specific application).

#### `deny` (array of strings)

Variables to explicitly block from inheritance, including Tier 1 baseline variables.

- **Type**: Array of strings
- **Default**: Empty array
- **Combines with implicit denylist**: Both are applied
- **Example**: `deny: ["SSH_AUTH_SOCK", "AWS_SECRET_ACCESS_KEY", "PATH"]`

Use this to block sensitive variables or to achieve maximum isolation by denying Tier 1 variables.

#### `allow_denied_if_explicit` (boolean)

Allow variables from the implicit denylist (and explicit `deny` list) if they're in `extra`.

- **Type**: Boolean
- **Default**: `false`
- **Example**: `allow_denied_if_explicit: true`

When `false`: Denied variables are always blocked, even if in `extra`.
When `true`: Variables in `extra` bypass both implicit and explicit deny lists.

**Security note**: Only enable this if you understand the risks (e.g., httpoxy).

---

## Configuration Examples

### Example 1: Basic Python Server (Default - Most Secure)

**Scenario**: Python MCP server needs Python-specific variables.

```yaml
servers:
  - name: python-mcp
    transport: stdio
    command: python3
    args: ["-m", "my_python_server"]
    inherit:
      mode: tier1  # Optional - this is the default
      extra: ["PYTHONPATH", "VIRTUAL_ENV", "PYTHONHOME"]
```

**Inherited**:
- Tier 1: PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR
- Extra: PYTHONPATH, VIRTUAL_ENV, PYTHONHOME

### Example 2: Node.js Server with TLS

**Scenario**: Node.js server making HTTPS API calls in corporate environment.

```yaml
servers:
  - name: node-api-server
    transport: stdio
    command: node
    args: ["server.js"]
    inherit:
      mode: tier1+tier2
      extra: ["NODE_ENV", "NODE_OPTIONS"]
```

**Inherited**:
- Tier 1: PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR
- Tier 2: SSL_CERT_FILE, SSL_CERT_DIR, REQUESTS_CA_BUNDLE, CURL_CA_BUNDLE, NODE_EXTRA_CA_CERTS
- Extra: NODE_ENV, NODE_OPTIONS

### Example 3: Application-Specific Variables

**Scenario**: Server needs all variables prefixed with `DATTO_` and `RMM_`.

```yaml
servers:
  - name: datto-rmm
    transport: stdio
    command: python3
    args: ["-m", "datto_rmm.server"]
    inherit:
      mode: tier1
      prefix: ["DATTO_", "RMM_"]
      extra: ["PYTHONPATH"]
```

**Inherited**:
- Tier 1: PATH, HOME, USER, SHELL, etc.
- All variables starting with `DATTO_` (e.g., DATTO_API_KEY, DATTO_URL)
- All variables starting with `RMM_`
- PYTHONPATH

### Example 4: Maximum Security (Untrusted Server)

**Scenario**: Untrusted experimental server, minimal exposure.

```yaml
servers:
  - name: experimental
    transport: stdio
    command: python3
    args: ["-m", "untrusted_server"]
    inherit:
      mode: tier1  # Only baseline variables
      deny: ["SSH_AUTH_SOCK"]  # Extra paranoia (though not in Tier 1 anyway)
```

**Inherited**: Only Tier 1 baseline variables (PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR, TEMP, TMP)

### Example 5: Maximum Isolation (Testing)

**Scenario**: Complete isolation for testing, no automatic inheritance.

```yaml
servers:
  - name: isolated-test
    transport: stdio
    command: python3
    args: ["-m", "test_server"]
    inherit:
      mode: tier1
      deny: ["PATH", "HOME", "USER", "SHELL", "LANG", "LC_ALL", "TZ", "TMPDIR", "TEMP", "TMP"]
    env:
      # Only these exact variables will be set
      PYTHONPATH: "/opt/myapp"
      MY_CONFIG: "test"
```

**Inherited**: None (all Tier 1 variables explicitly denied)
**Set**: Only PYTHONPATH and MY_CONFIG from `env:` block

**Note**: This will likely break most servers. Only use for controlled testing.

### Example 6: Proxy with Corporate Proxy Variables

**Scenario**: Enterprise environment requiring lowercase proxy variables.

```yaml
servers:
  - name: enterprise-server
    transport: stdio
    command: node
    args: ["server.js"]
    inherit:
      mode: tier1+tier2
      extra: ["http_proxy", "https_proxy", "no_proxy"]
      allow_denied_if_explicit: true  # Override implicit denylist
```

**Inherited**:
- Tier 1 + Tier 2 variables
- http_proxy, https_proxy, no_proxy (despite being in implicit denylist)

**⚠️ Security Note**: Only use this configuration if you control the server code and understand the httpoxy risk.

### Example 7: "All" Mode (Tier 1 + Tier 2 + Extras)

**Scenario**: Server needing network capabilities plus custom variables.

```yaml
servers:
  - name: feature-rich-server
    transport: stdio
    command: ./my-server
    inherit:
      mode: all  # Same as tier1+tier2
      extra: ["DATABASE_URL", "REDIS_URL", "API_TOKEN"]
      deny: ["AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN"]
```

**Inherited**:
- Tier 1 + Tier 2 variables
- DATABASE_URL, REDIS_URL, API_TOKEN

**Blocked**:
- AWS_SECRET_ACCESS_KEY, GITHUB_TOKEN (explicitly denied)
- All other parent environment variables not in Tier 1, Tier 2, or `extra` list

### Example 8: Proxy-Level Defaults

**Scenario**: Set defaults for all servers, override for specific ones.

```yaml
# Proxy-level defaults
proxy:
  healthCheckInterval: "30s"

inherit:
  mode: tier1
  extra: ["LANG", "LC_ALL"]

servers:
  - name: basic-server
    transport: stdio
    command: python3
    args: ["-m", "basic_server"]
    # Inherits proxy defaults (tier1 + LANG + LC_ALL)

  - name: special-server
    transport: stdio
    command: python3
    args: ["-m", "special_server"]
    inherit:
      mode: tier1+tier2  # Override: needs TLS
      extra: ["PYTHONPATH", "API_KEY"]
```

---

## Default Behavior

If you don't specify any inheritance configuration, the system uses secure defaults.

### When No `inherit` Block Exists

```yaml
servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "server"]
    # No inherit block specified
```

**Behavior**: Defaults to `mode: tier1` with no extras, prefixes, or deny rules.

**Inherited**: PATH, HOME, USER, SHELL, LANG, LC_ALL, TZ, TMPDIR, TEMP, TMP (Tier 1 baseline)

### Explicit Overrides Always Win

Even with `mode: tier1` and `deny` lists, explicit overrides in the `env:` block are always applied:

```yaml
servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "server"]
    inherit:
      mode: tier1
      deny: ["PATH"]  # Deny PATH from inheritance
    env:
      PATH: "/custom/path"  # Always set (explicit override)
      CUSTOM_VAR: "value"  # Always set
      API_KEY: "${PARENT_API_KEY}"  # Expanded from parent
```

**Result**: PATH is set to "/custom/path" (not inherited from parent), CUSTOM_VAR is set, API_KEY is expanded from parent environment.

---

## Resolution Order

Understanding precedence is crucial for debugging configuration issues.

### Inheritance Resolution (Processing Order)

1. **Implicit denylist** - HTTP_PROXY, HTTPS_PROXY, etc. blocked by default
2. **Explicit deny lists** - Variables in `deny:` arrays (proxy and server level)
3. **Tier 1 variables** - Always inherited unless denied
4. **Tier 2 variables** - Inherited if mode includes tier2
5. **Extra variables** - From `extra:` lists (proxy and server level)
6. **Prefix-matched variables** - Variables matching `prefix:` patterns
7. **Explicit `env:` overrides** - Always applied, never denied

### Deny Resolution

A variable is blocked if:
- It's in the **implicit denylist** AND not in `extra` with `allow_denied_if_explicit: true`
- It's in the **proxy-level deny list** AND not in `extra` with `allow_denied_if_explicit: true`
- It's in the **server-level deny list** AND not in `extra` with `allow_denied_if_explicit: true`

Variables in the `env:` block are NEVER blocked, regardless of deny lists.

### Example Resolution

```yaml
proxy:
  inherit:
    mode: tier1
    extra: ["PROXY_VAR"]
    deny: ["BLOCKED_VAR"]

servers:
  - name: my-server
    inherit:
      mode: tier1+tier2
      extra: ["SERVER_VAR"]
      # deny: []  (not specified, so proxy deny list applies)
    env:
      EXPLICIT_VAR: "value"
      BLOCKED_VAR: "allowed-via-override"
```

**Resolution**:
1. Implicit denylist: HTTP_PROXY, HTTPS_PROXY, etc. blocked
2. Proxy deny: BLOCKED_VAR blocked (from inheritance)
3. Tier 1: PATH, HOME, USER, SHELL, etc. inherited
4. Tier 2: SSL_CERT_FILE, etc. inherited (server mode is tier1+tier2)
5. Proxy extra: PROXY_VAR inherited
6. Server extra: SERVER_VAR inherited
7. Explicit env: EXPLICIT_VAR set, BLOCKED_VAR set (overrides deny)

---

## Implicit Denylist Details

### Why Block HTTP_PROXY?

The `HTTP_PROXY` environment variable (uppercase) is vulnerable to the **httpoxy** attack in CGI-like environments:

1. Attacker sends HTTP request with `Proxy: evil.com:8080` header
2. CGI environment sets `HTTP_PROXY=evil.com:8080`
3. Application uses `HTTP_PROXY` to proxy all outbound requests
4. Attacker intercepts all traffic, stealing credentials and data

**References**:
- [httpoxy.org](https://httpoxy.org/)
- [CVE-2016-5385](https://nvd.nist.gov/vuln/detail/CVE-2016-5385)

### Lowercase vs Uppercase

- **Lowercase** (`http_proxy`, `https_proxy`) - Standard libcurl convention, generally safer
- **Uppercase** (`HTTP_PROXY`, `HTTPS_PROXY`) - Vulnerable to httpoxy in CGI environments

The implicit denylist blocks **both** out of an abundance of caution. If you need proxy support:

```yaml
inherit:
  extra: ["http_proxy", "https_proxy"]  # Use lowercase variants
  allow_denied_if_explicit: true
```

### Full Implicit Denylist

```
HTTP_PROXY
HTTPS_PROXY
http_proxy
https_proxy
NO_PROXY
no_proxy
```

---

## Troubleshooting

### Server Can't Find Executables

**Symptom**: Server fails with "command not found" errors.

**Cause**: `PATH` denied (only possible with explicit `deny: ["PATH"]`).

**Solution**: Don't deny PATH, or set it explicitly:

```yaml
inherit:
  mode: tier1  # PATH included by default
# OR if you denied it:
env:
  PATH: "/usr/local/bin:/usr/bin:/bin"
```

### Server Can't Validate TLS Certificates

**Symptom**: SSL certificate verification failures in corporate environment.

**Cause**: Missing CA bundle environment variables.

**Solution**: Use `tier1+tier2` mode or add SSL variables explicitly:

```yaml
inherit:
  mode: tier1+tier2  # Includes SSL_CERT_FILE, etc.
# OR
inherit:
  mode: tier1
  extra: ["SSL_CERT_FILE", "REQUESTS_CA_BUNDLE"]
```

### Proxy Variables Not Working

**Symptom**: Server can't connect through corporate proxy.

**Cause**: Proxy variables in implicit denylist.

**Solution**: Explicitly allow lowercase proxy variables:

```yaml
inherit:
  extra: ["http_proxy", "https_proxy", "no_proxy"]
  allow_denied_if_explicit: true
```

### Server Missing Application-Specific Variables

**Symptom**: Server errors about missing configuration.

**Cause**: Variables not in tier definitions.

**Solution**: Use `extra` or `prefix`:

```yaml
inherit:
  mode: tier1
  extra: ["MY_API_KEY", "MY_CONFIG"]
# OR
inherit:
  mode: tier1
  prefix: ["MY_APP_"]  # Inherits MY_APP_KEY, MY_APP_URL, etc.
```

### "Mode All" Doesn't Inherit Everything

**Symptom**: Variables are missing despite using `mode: all`.

**Cause**: `mode: all` is equivalent to `tier1+tier2`, not "inherit everything".

**Solution**: This is intentional for security. Explicitly add needed variables:

```yaml
inherit:
  mode: all  # tier1+tier2
  extra: ["MY_VAR", "ANOTHER_VAR", "SECRET_KEY"]
```

### Need True Isolation (No Tier 1)

**Symptom**: Want to block all automatic inheritance, including Tier 1.

**Cause**: Tier 1 is always inherited unless explicitly denied.

**Solution**: Explicitly deny all Tier 1 variables:

```yaml
inherit:
  mode: tier1
  deny: ["PATH", "HOME", "USER", "SHELL", "LANG", "LC_ALL", "TZ", "TMPDIR", "TEMP", "TMP"]
env:
  # Only set what you need
  CUSTOM_VAR: "value"
```

**Warning**: This will likely break most servers. Only use for controlled testing.

### Case Sensitivity Issues (Windows)

**Symptom**: Environment variables not being inherited on Windows.

**Cause**: Case mismatch between config and actual variable names.

**Solution**: On Windows, environment variables are case-insensitive. The system will normalize keys automatically, but for consistency, match the case used in your environment:

```yaml
# Windows: both work
inherit:
  extra: ["PATH"]  # Standard
  extra: ["Path"]  # Also works
```

### Checking What's Actually Inherited

**Debug technique**: Create a test server that prints its environment:

```yaml
servers:
  - name: env-test
    transport: stdio
    command: /usr/bin/env  # Unix
    # command: cmd  # Windows
    # args: ["/c", "set"]  # Windows
    inherit:
      mode: tier1
      extra: ["TEST_VAR"]
```

Run discovery or proxy mode and examine the output to see exactly what variables were inherited.

---

## Validation

mcp-debug validates inheritance configuration at startup.

### Valid Modes

```yaml
inherit:
  mode: "none"         # ✓ Valid (same as tier1)
  mode: "tier1"        # ✓ Valid
  mode: "tier1+tier2"  # ✓ Valid
  mode: "all"          # ✓ Valid (same as tier1+tier2)
  mode: ""             # ✓ Valid (defaults to tier1)
```

### Invalid Modes

```yaml
inherit:
  mode: "tier2"        # ✗ Invalid (no tier2-only mode)
  mode: "tier1,tier2"  # ✗ Invalid (use "tier1+tier2")
  mode: "everything"   # ✗ Invalid (not a defined mode)
```

### Validation Errors

**Invalid mode**:
```
Error: server 'my-server': inherit: invalid mode "tier2": must be one of: none, tier1, tier1+tier2, all
```

**Solution**: Fix the mode value in your configuration.

### Running Validation

```bash
# Validate config file
uvx mcp-debug config validate --config config.yaml

# Test server startup
uvx mcp-debug --proxy --config config.yaml --log /tmp/debug.log
```

Check the log file for validation errors or warnings.

---

## Platform Differences

### Windows vs Unix

**Environment Variable Names**:
- **Unix/Linux/macOS**: Case-sensitive (`PATH` ≠ `path`)
- **Windows**: Case-insensitive (`PATH` = `Path` = `path`)

**Temporary Directories**:
- **Unix**: `TMPDIR`
- **Windows**: `TEMP`, `TMP`

The tier system includes all variants for cross-platform compatibility.

**File Paths in Values**:
- **Unix**: `/home/user/.config`
- **Windows**: `C:\Users\User\AppData\Roaming`

Environment variable expansion respects platform path conventions.

### XDG Base Directory Specification

On Unix-like systems, Tier 1 includes XDG Base Directory variables per the [freedesktop.org specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html):

- `XDG_CONFIG_HOME` - User configuration files
- `XDG_CACHE_HOME` - User cache data
- `XDG_DATA_HOME` - User data files
- `XDG_STATE_HOME` - User state data
- `XDG_RUNTIME_DIR` - Runtime files and sockets

These are not applicable on Windows.

---

## Advanced Use Cases

### Multi-Tenant Isolation

**Scenario**: Running multiple MCP servers for different customers, ensuring complete isolation.

```yaml
servers:
  - name: customer-a
    transport: stdio
    command: python3
    args: ["-m", "mcp_server"]
    inherit:
      mode: tier1
      deny: ["HOME", "USER"]  # Block even Tier 1 vars for extra isolation
    env:
      CUSTOMER_ID: "customer-a"
      DB_URL: "postgresql://db-a/data"
      HOME: "/var/lib/customer-a"  # Custom HOME

  - name: customer-b
    transport: stdio
    command: python3
    args: ["-m", "mcp_server"]
    inherit:
      mode: tier1
      deny: ["HOME", "USER"]
    env:
      CUSTOMER_ID: "customer-b"
      DB_URL: "postgresql://db-b/data"
      HOME: "/var/lib/customer-b"  # Custom HOME
```

Each server has isolated environment with custom values.

### Development vs Production

**Scenario**: Different inheritance rules for dev and production.

```yaml
# development-config.yaml
proxy:
  inherit:
    mode: tier1+tier2  # More permissive for development
    extra: ["DEBUG", "DEV_TOKEN"]

servers:
  - name: dev-server
    transport: stdio
    command: ./dev-server
```

```yaml
# production-config.yaml
proxy:
  inherit:
    mode: tier1  # Strict for production
    deny: ["SSH_AUTH_SOCK", "AWS_SESSION_TOKEN"]

servers:
  - name: prod-server
    transport: stdio
    command: ./prod-server
```

### Dynamic Environment Variables

**Scenario**: Pass current timestamp or dynamic values to servers.

```bash
# Set variable in parent shell
export BUILD_ID="$(date +%s)"
export DEPLOY_VERSION="v1.2.3"
```

```yaml
servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "server"]
    inherit:
      mode: tier1
      extra: ["BUILD_ID", "DEPLOY_VERSION"]
```

The server receives the current values of these variables.

### Testing Different Inheritance Modes

**Scenario**: A/B testing different inheritance configurations.

Create multiple config files and test:

```bash
# Test tier1 (minimal)
uvx mcp-debug --proxy --config config-tier1.yaml

# Test tier1+tier2 (with network)
uvx mcp-debug --proxy --config config-tier2.yaml

# Test with extras
uvx mcp-debug --proxy --config config-extras.yaml
```

Compare behavior and choose the most secure option that works.

---

## Migration Guide

### From No Configuration

If you previously ran mcp-debug without any inheritance configuration:

**Old behavior**: Depended on implementation details (may have varied)

**New behavior**: Defaults to `mode: tier1` (secure by default)

**Migration path**:
1. Test with default `tier1` mode
2. If servers break, identify missing variables via logs
3. Add missing variables to `extra` list
4. OR switch to `mode: tier1+tier2` if TLS variables are needed

### From Expecting "All Variables"

If you expected all parent environment variables to be inherited:

**Old expectation**: All variables from parent process

**New behavior**: Only Tier 1 (or Tier 1 + Tier 2 with `mode: all`)

**Migration path**:

**Step 1**: Identify which variables your server actually needs:

```yaml
inherit:
  mode: tier1  # Start minimal
  # Server will fail with clear errors about missing vars
```

**Step 2**: Add needed variables explicitly:

```yaml
inherit:
  mode: tier1+tier2  # If TLS needed
  extra: ["DATABASE_URL", "API_KEY", "CUSTOM_CONFIG"]
```

**Step 3**: Test thoroughly and add missing variables as needed.

### Adding to Existing Servers

If you have existing server configurations without inheritance:

**Before**:
```yaml
servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "server"]
```

**After (explicit tier1)**:
```yaml
servers:
  - name: my-server
    transport: stdio
    command: python3
    args: ["-m", "server"]
    inherit:
      mode: tier1  # Make it explicit
      extra: ["PYTHONPATH"]
```

The default behavior (tier1) is applied automatically if you don't add an `inherit` block.

---

## Testing Your Configuration

### Manual Testing

**Step 1**: Create a test server that prints its environment:

```yaml
# test-config.yaml
servers:
  - name: env-dump
    transport: stdio
    command: /usr/bin/env
    inherit:
      mode: tier1
      extra: ["TEST_VAR"]
```

**Step 2**: Set test variables:

```bash
export TEST_VAR="test-value"
export SECRET_VAR="should-not-appear"
```

**Step 3**: Run mcp-debug:

```bash
uvx mcp-debug --proxy --config test-config.yaml
```

**Step 4**: Check output - `TEST_VAR` should appear, `SECRET_VAR` should not.

### Automated Testing

Create a test script:

```bash
#!/bin/bash

export HOME="/home/testuser"  # Tier 1 - should inherit
export SSL_CERT_FILE="/etc/ssl/cert.pem"  # Tier 2 - only with tier2 mode
export CUSTOM_VAR="test"  # Only with extra
export SECRET_VAR="secret"  # Should NOT inherit

# Test tier1 mode
echo "Testing tier1 mode..."
uvx mcp-debug --proxy --config test-tier1.yaml 2>&1 | grep -q "HOME="
if [ $? -eq 0 ]; then echo "✓ Tier1 works"; else echo "✗ Tier1 failed"; fi

# Test tier1+tier2 mode
echo "Testing tier1+tier2 mode..."
uvx mcp-debug --proxy --config test-tier2.yaml 2>&1 | grep -q "SSL_CERT_FILE="
if [ $? -eq 0 ]; then echo "✓ Tier2 works"; else echo "✗ Tier2 failed"; fi

# Verify secret not leaked
echo "Testing secret isolation..."
uvx mcp-debug --proxy --config test-tier1.yaml 2>&1 | grep -q "SECRET_VAR="
if [ $? -eq 1 ]; then echo "✓ Secret blocked"; else echo "✗ Secret leaked!"; fi
```

### Integration Testing

Test with real MCP servers:

```yaml
servers:
  - name: real-server
    transport: stdio
    command: python3
    args: ["-m", "my_real_server"]
    inherit:
      mode: tier1
      extra: ["PYTHONPATH"]
```

Run through normal workflows and verify:
1. Server starts successfully
2. Tools work as expected
3. No errors about missing environment variables
4. Sensitive variables are not accessible to the server

---

## FAQ

### Q: What happens if I don't specify an `inherit` block?

**A**: Defaults to `mode: tier1` (secure by default). Only Tier 1 baseline variables are inherited.

### Q: What's the difference between `mode: none` and `mode: tier1`?

**A**: They're identical in the current implementation. Both inherit Tier 1 baseline variables. The "none" naming is kept for backward compatibility.

### Q: How do I achieve true isolation (no automatic inheritance)?

**A**: Use `mode: tier1` with an explicit `deny:` list containing all Tier 1 variables:

```yaml
inherit:
  mode: tier1
  deny: ["PATH", "HOME", "USER", "SHELL", "LANG", "LC_ALL", "TZ", "TMPDIR", "TEMP", "TMP"]
env:
  # Only set what you need
  CUSTOM_VAR: "value"
```

### Q: Why doesn't `mode: all` inherit ALL variables?

**A**: Security-first design. `mode: all` is equivalent to `tier1+tier2`. Inheriting all parent variables would risk leaking credentials and secrets. To inherit additional variables, use the `extra:` list.

### Q: Can I use `mode: tier2` without `tier1`?

**A**: No. The only modes are `none` (=tier1), `tier1`, `tier1+tier2`, and `all` (=tier1+tier2). Tier 2 always includes Tier 1.

### Q: Why are proxy variables blocked by default?

**A**: To prevent httpoxy attacks. The uppercase `HTTP_PROXY` variable can be set by attackers via HTTP headers in CGI environments. We block both uppercase and lowercase variants out of caution.

### Q: How do I enable proxy variables safely?

**A**: Use lowercase variants with explicit opt-in:

```yaml
inherit:
  mode: tier1+tier2
  extra: ["http_proxy", "https_proxy", "no_proxy"]
  allow_denied_if_explicit: true
```

### Q: Does `env:` override the deny list?

**A**: Yes. Explicit `env:` overrides are ALWAYS applied, regardless of deny lists.

```yaml
inherit:
  deny: ["BLOCKED_VAR"]
env:
  BLOCKED_VAR: "this-will-be-set"  # Always wins
```

### Q: Can I mix `prefix` and `extra`?

**A**: Yes. They're additive:

```yaml
inherit:
  extra: ["CUSTOM1", "CUSTOM2"]
  prefix: ["MY_APP_", "CONFIG_"]
```

This inherits: Tier 1 + CUSTOM1 + CUSTOM2 + any variables starting with MY_APP_ or CONFIG_.

### Q: Are environment variable names case-sensitive?

**A**: On Unix: yes (`PATH` ≠ `path`). On Windows: no (`PATH` = `Path` = `path`).

### Q: Can I set proxy-level defaults and override per-server?

**A**: Yes:

```yaml
proxy:
  inherit:
    mode: tier1  # Default for all servers

servers:
  - name: server1
    inherit:
      mode: tier1+tier2  # Override for this server
```

### Q: What if a variable is in both `extra` and `deny`?

**A**: Depends on `allow_denied_if_explicit`:
- `false` (default): Variable is blocked
- `true`: Variable is allowed (because it's in `extra`)

### Q: How do I debug what's being inherited?

**A**: Use a test server that prints its environment:

```yaml
servers:
  - name: env-test
    command: /usr/bin/env
    inherit:
      mode: tier1
```

### Q: Can I use environment variable expansion in the `inherit` block?

**A**: Yes, for the lists:

```yaml
inherit:
  extra: ["${MY_VAR_NAME}"]  # Expands at config load time
```

But this is rarely useful. More commonly, you'd use expansion in `env:`:

```yaml
env:
  API_KEY: "${PARENT_API_KEY}"  # Gets value from parent
```

### Q: Does this work with SSE (Server-Sent Events) transport?

**A**: The inheritance system only applies to `stdio` transport (local child processes). SSE and HTTP transports don't have environment inheritance because they're remote connections.

### Q: Why is Tier 1 always inherited?

**A**: Security AND functionality. Tier 1 variables (PATH, HOME, etc.) are required for basic process operation and rarely contain secrets. Making them the guaranteed baseline prevents accidentally creating broken environments while maintaining security. You can still block them with `deny:` if needed for testing.

---

## See Also

- [Main README](README.md) - mcp-debug overview
- [Configuration Examples](examples/) - Sample config files
- [MCP Specification](https://modelcontextprotocol.io/) - Model Context Protocol docs
- [httpoxy.org](https://httpoxy.org/) - httpoxy vulnerability details
- [XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html) - XDG spec

---

## Feedback and Updates

This is DRAFT documentation for a newly implemented feature. As we gather real-world usage data, we may:

- Adjust tier definitions based on common requirements
- Add new configuration options
- Update security recommendations
- Add more examples and troubleshooting guidance

**Report issues or suggest improvements**:
- GitHub Issues: [mcp-debug issues](https://github.com/standardbeagle/mcp-debug/issues)
- Discussion: Include "[env-inheritance]" in the title

**Last Updated**: 2026-01-13
**Implementation Commit**: 49f5581
**Branch**: feature/env-selective-inheritance
