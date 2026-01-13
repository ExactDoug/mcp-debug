package client

import (
	"os"
	"runtime"
	"strings"
)

// MergeEnvironment merges the parent process environment with overrides.
// The parent environment is obtained via os.Environ() and then override
// values are applied on top. On Windows, environment variable names are
// case-insensitive and normalized to uppercase for proper deduplication.
//
// Returns a []string in "KEY=value" format suitable for exec.Cmd.Env.
func MergeEnvironment(overrides map[string]string) []string {
	isWindows := runtime.GOOS == "windows"

	// Maps for tracking environment variables
	// envMap: normalized_key -> value
	// keyMap: normalized_key -> original_key (preserves casing)
	envMap := make(map[string]string)
	keyMap := make(map[string]string)

	// Load parent environment
	for _, entry := range os.Environ() {
		key, value := splitEnvEntry(entry)
		if key == "" {
			continue // Skip malformed entries
		}

		lookupKey := normalizeKey(key, isWindows)
		envMap[lookupKey] = value
		keyMap[lookupKey] = key
	}

	// Apply overrides (last wins)
	for key, value := range overrides {
		lookupKey := normalizeKey(key, isWindows)
		envMap[lookupKey] = value
		keyMap[lookupKey] = key // Use override's exact casing
	}

	// Build result slice
	result := make([]string, 0, len(envMap))
	for lookupKey, value := range envMap {
		originalKey := keyMap[lookupKey]
		result = append(result, originalKey+"="+value)
	}

	return result
}

// normalizeKey normalizes environment variable keys for comparison.
// On Windows, converts to uppercase for case-insensitive comparison.
// On other platforms, returns the key unchanged.
func normalizeKey(key string, isWindows bool) string {
	if isWindows {
		return strings.ToUpper(key)
	}
	return key
}

// splitEnvEntry splits an environment entry "KEY=value" into key and value.
// Handles values containing "=" by only splitting on the first occurrence.
// Returns empty string for key if the entry is malformed.
func splitEnvEntry(entry string) (key, value string) {
	idx := strings.Index(entry, "=")
	if idx <= 0 {
		return "", "" // Malformed entry
	}
	return entry[:idx], entry[idx+1:]
}
