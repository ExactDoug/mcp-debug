package client

import (
	"os"
	"runtime"
	"testing"
)

// TestMergeEnvironment tests the MergeEnvironment function with various scenarios
func TestMergeEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		parent    []string
		overrides map[string]string
		expected  map[string]string
	}{
		{
			name:   "empty overrides",
			parent: []string{"PATH=/usr/bin", "HOME=/home/user"},
			overrides: map[string]string{},
			expected: map[string]string{
				"PATH": "/usr/bin",
				"HOME": "/home/user",
			},
		},
		{
			name:   "override existing variable",
			parent: []string{"PATH=/usr/bin", "HOME=/home/user"},
			overrides: map[string]string{
				"PATH": "/custom/bin",
			},
			expected: map[string]string{
				"PATH": "/custom/bin",
				"HOME": "/home/user",
			},
		},
		{
			name:   "add new variable",
			parent: []string{"PATH=/usr/bin"},
			overrides: map[string]string{
				"DEBUG": "true",
			},
			expected: map[string]string{
				"PATH":  "/usr/bin",
				"DEBUG": "true",
			},
		},
		{
			name:   "mixed override and add",
			parent: []string{"PATH=/usr/bin", "HOME=/home/user"},
			overrides: map[string]string{
				"PATH":  "/custom/bin",
				"DEBUG": "true",
			},
			expected: map[string]string{
				"PATH":  "/custom/bin",
				"HOME":  "/home/user",
				"DEBUG": "true",
			},
		},
		{
			name:   "empty string value",
			parent: []string{"PATH=/usr/bin"},
			overrides: map[string]string{
				"PATH": "",
			},
			expected: map[string]string{
				"PATH": "",
			},
		},
		{
			name:   "nil parent",
			parent: nil,
			overrides: map[string]string{
				"DEBUG": "true",
			},
			expected: map[string]string{
				"DEBUG": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Temporarily set system env to match parent for testing
			if tt.parent != nil {
				// Clear env and set to parent values for this test
				oldEnv := os.Environ()
				defer func() {
					// Restore original env
					os.Clearenv()
					for _, entry := range oldEnv {
						key, value := splitEnvEntry(entry)
						if key != "" {
							os.Setenv(key, value)
						}
					}
				}()

				os.Clearenv()
				for _, entry := range tt.parent {
					key, value := splitEnvEntry(entry)
					if key != "" {
						os.Setenv(key, value)
					}
				}
			}

			result := MergeEnvironment(tt.overrides)
			resultMap := sliceToMap(result)

			// If parent is nil, merge with system environment
			if tt.parent == nil {
				// Just check that our override is present
				if val, ok := resultMap["DEBUG"]; !ok || val != "true" {
					t.Errorf("Expected DEBUG=true in result, got %v", resultMap)
				}
				return
			}

			// Check all expected variables are present with correct values
			for key, expectedVal := range tt.expected {
				if actualVal, ok := resultMap[key]; !ok {
					t.Errorf("Expected key %q not found in result", key)
				} else if actualVal != expectedVal {
					t.Errorf("For key %q: expected %q, got %q", key, expectedVal, actualVal)
				}
			}

			// Check no unexpected variables (only for non-nil parent)
			if len(resultMap) != len(tt.expected) {
				t.Errorf("Expected %d variables, got %d: %v", len(tt.expected), len(resultMap), resultMap)
			}
		})
	}
}

// TestMergeEnvironmentWindows tests Windows-specific case-insensitive behavior
func TestMergeEnvironmentWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test")
	}

	// Save and restore environment
	defer restoreEnv("PATH")()

	// Set up test environment
	oldEnv := os.Environ()
	defer func() {
		// Restore original env
		os.Clearenv()
		for _, entry := range oldEnv {
			key, value := splitEnvEntry(entry)
			if key != "" {
				os.Setenv(key, value)
			}
		}
	}()

	os.Clearenv()
	os.Setenv("PATH", "C:\\Windows\\System32")
	os.Setenv("HOME", "C:\\Users\\test")

	overrides := map[string]string{
		"Path": "C:\\Custom\\Path", // Different case
	}

	result := MergeEnvironment(overrides)
	resultMap := sliceToMap(result)

	// On Windows, PATH and Path should be treated as the same variable
	pathValue := ""
	for key, val := range resultMap {
		if key == "PATH" || key == "Path" || key == "path" {
			pathValue = val
			break
		}
	}

	if pathValue != "C:\\Custom\\Path" {
		t.Errorf("Expected PATH to be overridden (case-insensitive), got: %v", resultMap)
	}

	// HOME should still be present
	if home, ok := resultMap["HOME"]; !ok || home != "C:\\Users\\test" {
		t.Errorf("Expected HOME=C:\\Users\\test, got: %v", resultMap)
	}
}

// restoreEnv saves the current environment variable and returns a function to restore it
func restoreEnv(key string) func() {
	original, exists := os.LookupEnv(key)
	return func() {
		if exists {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	}
}

// TestSplitEnvEntry tests parsing of environment variable entries
func TestSplitEnvEntry(t *testing.T) {
	tests := []struct {
		name      string
		entry     string
		wantKey   string
		wantValue string
	}{
		{"normal entry", "PATH=/usr/bin", "PATH", "/usr/bin"},
		{"value with equals", "URL=http://example.com?foo=bar", "URL", "http://example.com?foo=bar"},
		{"empty value", "EMPTY=", "EMPTY", ""},
		{"no equals", "INVALID", "", ""},
		{"empty key", "=value", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value := splitEnvEntry(tt.entry)
			if key != tt.wantKey || value != tt.wantValue {
				t.Errorf("splitEnvEntry(%q) = (%q, %q), want (%q, %q)",
					tt.entry, key, value, tt.wantKey, tt.wantValue)
			}
		})
	}
}

// sliceToMap converts []string environment to map[string]string for easier testing
func sliceToMap(env []string) map[string]string {
	result := make(map[string]string)
	for _, entry := range env {
		key, value := splitEnvEntry(entry)
		if key != "" {
			result[key] = value
		}
	}
	return result
}
