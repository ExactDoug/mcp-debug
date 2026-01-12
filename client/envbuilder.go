package client

import (
	"os"
	"runtime"
	"strings"

	"mcp-debug/config"
)

// Tier1Vars are baseline environment variables that most processes need.
// These are always inherited unless explicitly denied.
var Tier1Vars = []string{
	"PATH",
	"HOME",
	"USER",
	"SHELL",
	"LANG",
	"LC_ALL",
	"TZ",
	"TMPDIR",
	"TEMP",
	"TMP",
}

// Tier2Vars are network and TLS-related variables.
// These are inherited when TLS inheritance is enabled.
var Tier2Vars = []string{
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
	"REQUESTS_CA_BUNDLE",
	"CURL_CA_BUNDLE",
	"NODE_EXTRA_CA_CERTS",
}

// ImplicitDenylist contains variables that should never be inherited
// without explicit configuration, as they can cause unexpected behavior.
var ImplicitDenylist = []string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"http_proxy",
	"https_proxy",
	"NO_PROXY",
	"no_proxy",
}

// BuildEnvironment constructs the environment for an MCP server based on
// tier-based inheritance rules and configuration overrides.
//
// Inheritance tiers:
//   - Tier 1 (baseline): Always inherited unless explicitly denied
//   - Tier 2 (network/TLS): Inherited when TLS inheritance enabled
//   - Implicit denylist: Blocked by default (e.g., HTTP_PROXY)
//   - Extra variables: Additional variables specified in config
//   - Prefix matching: Variables matching configured prefixes
//
// Configuration precedence (highest to lowest):
//   1. Explicit env overrides in server config
//   2. Explicit deny rules (server and proxy level)
//   3. Tier 1 variables (unless denied)
//   4. Tier 2 variables (if TLS enabled, unless denied)
//   5. Extra variables from config (unless denied)
//   6. Prefix-matched variables (unless denied)
//
// Parameters:
//   - serverConfig: The server configuration containing env overrides and inheritance rules
//   - proxyInherit: Proxy-level inheritance configuration (may be nil)
//
// Returns:
//   - []string: Environment in "KEY=value" format for exec.Cmd.Env
func BuildEnvironment(serverConfig *config.ServerConfig, proxyInherit *config.InheritConfig) []string {
	isWindows := runtime.GOOS == "windows"

	// Build combined deny map (normalized keys)
	denyMap := buildDenyMap(serverConfig, proxyInherit, isWindows)

	// Build parent environment map (normalized lookup keys)
	parentMap := buildParentMap()

	// Result map: normalized_key -> (original_key, value)
	envMap := make(map[string]struct {
		key   string
		value string
	})

	// Helper to add variable if not denied
	// explicitExtra indicates if this is from the Extra list (bypasses implicit deny)
	addVar := func(key string, explicitExtra bool) {
		lookupKey := normalizeKey(key, isWindows)

		// Check if denied
		if denyMap[lookupKey] {
			// If this is from Extra list and AllowDeniedIfExplicit is true, allow it
			if explicitExtra {
				if serverConfig.Inherit != nil && serverConfig.Inherit.AllowDeniedIfExplicit {
					// Allow this variable even though it's denied
				} else if proxyInherit != nil && proxyInherit.AllowDeniedIfExplicit {
					// Allow this variable even though it's denied
				} else {
					return // Denied and not explicitly allowed
				}
			} else {
				return // Explicitly denied
			}
		}

		if val, exists := parentMap[lookupKey]; exists {
			envMap[lookupKey] = struct {
				key   string
				value string
			}{key, val}
		}
	}

	// Step 1: Add Tier 1 (baseline) variables
	for _, key := range Tier1Vars {
		addVar(key, false)
	}

	// Step 2: Add Tier 2 (network/TLS) variables if tier1+tier2 or all mode enabled
	tier2Enabled := false
	if serverConfig.Inherit != nil {
		if serverConfig.Inherit.Mode == config.InheritTier1Tier2 || serverConfig.Inherit.Mode == config.InheritAll {
			tier2Enabled = true
		}
	}
	if !tier2Enabled && proxyInherit != nil {
		if proxyInherit.Mode == config.InheritTier1Tier2 || proxyInherit.Mode == config.InheritAll {
			tier2Enabled = true
		}
	}
	if tier2Enabled {
		for _, key := range Tier2Vars {
			addVar(key, false)
		}
	}

	// Step 3: Add extra variables from config (server level, then proxy level)
	if serverConfig.Inherit != nil {
		for _, key := range serverConfig.Inherit.Extra {
			addVar(key, true) // Mark as explicit extra
		}
	}
	if proxyInherit != nil {
		for _, key := range proxyInherit.Extra {
			addVar(key, true) // Mark as explicit extra
		}
	}

	// Step 4: Add prefix-matched variables (server level, then proxy level)
	prefixes := []string{}
	if serverConfig.Inherit != nil {
		prefixes = append(prefixes, serverConfig.Inherit.Prefix...)
	}
	if proxyInherit != nil {
		prefixes = append(prefixes, proxyInherit.Prefix...)
	}

	for lookupKey, val := range parentMap {
		if denyMap[lookupKey] {
			continue // Already denied
		}
		// Check if any prefix matches
		for _, prefix := range prefixes {
			normalizedPrefix := normalizeKey(prefix, isWindows)
			if strings.HasPrefix(lookupKey, normalizedPrefix) {
				// Find original key from parent environment
				originalKey := ""
				for _, entry := range os.Environ() {
					k, v := splitEnvEntry(entry)
					if normalizeKey(k, isWindows) == lookupKey && v == val {
						originalKey = k
						break
					}
				}
				if originalKey != "" {
					envMap[lookupKey] = struct {
						key   string
						value string
					}{originalKey, val}
				}
				break
			}
		}
	}

	// Step 5: Apply explicit environment overrides from server config
	// These override everything and ignore deny rules
	for key, value := range serverConfig.Env {
		lookupKey := normalizeKey(key, isWindows)
		envMap[lookupKey] = struct {
			key   string
			value string
		}{key, value}
	}

	// Build final result
	result := make([]string, 0, len(envMap))
	for _, entry := range envMap {
		result = append(result, entry.key+"="+entry.value)
	}

	return result
}

// buildDenyMap creates a normalized map of denied variable names.
// Includes implicit denylist plus any explicit deny rules from config.
func buildDenyMap(serverConfig *config.ServerConfig, proxyInherit *config.InheritConfig, isWindows bool) map[string]bool {
	denyMap := make(map[string]bool)

	// Add implicit denylist
	for _, key := range ImplicitDenylist {
		denyMap[normalizeKey(key, isWindows)] = true
	}

	// Add server-level deny rules
	if serverConfig.Inherit != nil {
		for _, key := range serverConfig.Inherit.Deny {
			denyMap[normalizeKey(key, isWindows)] = true
		}
	}

	// Add proxy-level deny rules
	if proxyInherit != nil {
		for _, key := range proxyInherit.Deny {
			denyMap[normalizeKey(key, isWindows)] = true
		}
	}

	return denyMap
}

// buildParentMap creates a normalized map of parent environment variables.
// Returns: map[normalized_key]value
func buildParentMap() map[string]string {
	isWindows := runtime.GOOS == "windows"
	parentMap := make(map[string]string)

	for _, entry := range os.Environ() {
		key, value := splitEnvEntry(entry)
		if key == "" {
			continue
		}
		lookupKey := normalizeKey(key, isWindows)
		parentMap[lookupKey] = value
	}

	return parentMap
}
