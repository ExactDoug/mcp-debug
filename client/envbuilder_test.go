package client

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"mcp-debug/config"
)

// TestBuildEnvironment_ModeNone tests that only explicit overrides are included
func TestBuildEnvironment_ModeNone(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("SECRET_KEY", "should-not-inherit")

	// Create server config with mode=none and explicit overrides
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritNone,
		},
		Env: map[string]string{
			"CUSTOM": "value",
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify override is present
	if resultMap["CUSTOM"] != "value" {
		t.Error("CUSTOM override should be present")
	}

	// Verify Tier1 vars ARE inherited (Tier1 is baseline, always inherited unless denied)
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited (Tier1 baseline)")
	}
	if _, ok := resultMap["PATH"]; !ok {
		t.Error("PATH should be inherited (Tier1 baseline)")
	}

	// Verify non-tier1 var is NOT present
	if _, ok := resultMap["SECRET_KEY"]; ok {
		t.Error("SECRET_KEY should NOT be inherited in mode=none")
	}
}

// TestBuildEnvironment_ModeTier1 tests that only Tier1 vars are inherited
func TestBuildEnvironment_ModeTier1(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("USER", "testuser")
	os.Setenv("SHELL", "/bin/bash")
	os.Setenv("SECRET_KEY", "should-not-inherit")
	os.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")

	// Create server config with tier1 mode and explicit overrides
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritTier1,
		},
		Env: map[string]string{
			"CUSTOM": "value",
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify tier1 vars are present
	tier1Expected := []string{"HOME", "PATH", "USER", "SHELL"}
	for _, varName := range tier1Expected {
		if _, ok := resultMap[varName]; !ok {
			t.Errorf("%s should be inherited (tier1 var)", varName)
		}
	}

	// Verify non-tier1 vars are NOT present
	if _, ok := resultMap["SECRET_KEY"]; ok {
		t.Error("SECRET_KEY should NOT be inherited (not in tier1)")
	}
	if _, ok := resultMap["SSH_AUTH_SOCK"]; ok {
		t.Error("SSH_AUTH_SOCK should NOT be inherited (not in tier1)")
	}

	// Verify explicit override is present
	if resultMap["CUSTOM"] != "value" {
		t.Error("CUSTOM override should be present")
	}
}

// TestBuildEnvironment_ModeTier1Tier2 tests that Tier1 + Tier2 vars are inherited
func TestBuildEnvironment_ModeTier1Tier2(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("SSL_CERT_FILE", "/etc/ssl/cert.pem")
	os.Setenv("CURL_CA_BUNDLE", "/etc/ssl/ca-bundle.crt")
	os.Setenv("SECRET_KEY", "should-not-inherit")

	// Create server config with tier1+tier2 mode
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritTier1Tier2,
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify tier1 vars are present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited (tier1 var)")
	}
	if _, ok := resultMap["PATH"]; !ok {
		t.Error("PATH should be inherited (tier1 var)")
	}

	// Verify tier2 vars are present
	if _, ok := resultMap["SSL_CERT_FILE"]; !ok {
		t.Error("SSL_CERT_FILE should be inherited (tier2 var)")
	}
	if _, ok := resultMap["CURL_CA_BUNDLE"]; !ok {
		t.Error("CURL_CA_BUNDLE should be inherited (tier2 var)")
	}

	// Verify non-tier vars are NOT present
	if _, ok := resultMap["SECRET_KEY"]; ok {
		t.Error("SECRET_KEY should NOT be inherited (not in any tier)")
	}
}

// TestBuildEnvironment_ModeAll tests that mode=all includes Tier1+Tier2 and can use Extra for additional vars
func TestBuildEnvironment_ModeAll(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("SSL_CERT_FILE", "/etc/ssl/cert.pem")
	os.Setenv("CUSTOM_VAR", "custom-value")
	os.Setenv("SECRET_KEY", "secret123")

	// Create server config with mode=all + extra vars
	// Note: mode=all means Tier1+Tier2, not "inherit everything"
	// To inherit additional vars, use Extra list
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode:  config.InheritAll,
			Extra: []string{"CUSTOM_VAR", "SECRET_KEY"},
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify Tier1 vars are present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited (tier1 var)")
	}
	if _, ok := resultMap["PATH"]; !ok {
		t.Error("PATH should be inherited (tier1 var)")
	}

	// Verify Tier2 vars are present
	if _, ok := resultMap["SSL_CERT_FILE"]; !ok {
		t.Error("SSL_CERT_FILE should be inherited (tier2 var)")
	}

	// Verify extra vars are present
	if resultMap["CUSTOM_VAR"] != "custom-value" {
		t.Error("CUSTOM_VAR should be inherited (extra var)")
	}
	if resultMap["SECRET_KEY"] != "secret123" {
		t.Error("SECRET_KEY should be inherited (extra var)")
	}
}

// TestBuildEnvironment_ExtraVariables tests that explicitly requested vars are added
func TestBuildEnvironment_ExtraVariables(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("PYTHONPATH", "/opt/python/lib")
	os.Setenv("VIRTUAL_ENV", "/opt/venv")

	// Create server config with tier1 + extras
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode:  config.InheritTier1,
			Extra: []string{"PYTHONPATH", "VIRTUAL_ENV"},
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify tier1 vars are present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited (tier1 var)")
	}

	// Verify extra vars are present
	if resultMap["PYTHONPATH"] != "/opt/python/lib" {
		t.Error("PYTHONPATH should be inherited (extra var)")
	}
	if resultMap["VIRTUAL_ENV"] != "/opt/venv" {
		t.Error("VIRTUAL_ENV should be inherited (extra var)")
	}
}

// TestBuildEnvironment_PrefixMatching tests that variables matching prefixes are added
func TestBuildEnvironment_PrefixMatching(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("DATTO_API_KEY", "key123")
	os.Setenv("DATTO_API_URL", "https://api.datto.com")
	os.Setenv("DATTO_DEBUG", "true")
	os.Setenv("OTHER_VAR", "should-not-match")

	// Create server config with tier1 + DATTO_ prefix
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode:   config.InheritTier1,
			Prefix: []string{"DATTO_"},
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify tier1 vars are present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited (tier1 var)")
	}

	// Verify all DATTO_ prefixed vars are present
	if resultMap["DATTO_API_KEY"] != "key123" {
		t.Error("DATTO_API_KEY should be inherited (prefix match)")
	}
	if resultMap["DATTO_API_URL"] != "https://api.datto.com" {
		t.Error("DATTO_API_URL should be inherited (prefix match)")
	}
	if resultMap["DATTO_DEBUG"] != "true" {
		t.Error("DATTO_DEBUG should be inherited (prefix match)")
	}

	// Verify non-matching vars are NOT present
	if _, ok := resultMap["OTHER_VAR"]; ok {
		t.Error("OTHER_VAR should NOT be inherited (no prefix match)")
	}
}

// TestBuildEnvironment_Denylist tests that denied vars are blocked
func TestBuildEnvironment_Denylist(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")

	// Create server config with mode=all + deny SSH_AUTH_SOCK
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritAll,
			Deny: []string{"SSH_AUTH_SOCK"},
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify allowed vars are present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited")
	}
	if _, ok := resultMap["PATH"]; !ok {
		t.Error("PATH should be inherited")
	}

	// Verify denied var is NOT present
	if _, ok := resultMap["SSH_AUTH_SOCK"]; ok {
		t.Error("SSH_AUTH_SOCK should be denied")
	}
}

// TestBuildEnvironment_AllowDeniedIfExplicit tests that explicitly requested vars override denylist
func TestBuildEnvironment_AllowDeniedIfExplicit(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")
	os.Setenv("HTTP_PROXY", "http://proxy:8080")

	// Create server config with deny + allow_denied_if_explicit + extra
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode:                  config.InheritTier1,
			Extra:                 []string{"SSH_AUTH_SOCK", "HTTP_PROXY"},
			Deny:                  []string{"SSH_AUTH_SOCK"},
			AllowDeniedIfExplicit: true,
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify denied but explicitly requested var IS present (because allow_denied_if_explicit=true)
	if resultMap["SSH_AUTH_SOCK"] != "/tmp/ssh-agent" {
		t.Error("SSH_AUTH_SOCK should be allowed (explicitly requested + allow_denied_if_explicit)")
	}

	// Verify HTTP_PROXY is also allowed (in implicit denylist + Extra list + allow_denied_if_explicit)
	if resultMap["HTTP_PROXY"] != "http://proxy:8080" {
		t.Error("HTTP_PROXY should be allowed (implicit denylist but in Extra + allow_denied_if_explicit)")
	}
}

// TestBuildEnvironment_HTTPProxyBlocked tests that uppercase HTTP_PROXY is blocked by default
func TestBuildEnvironment_HTTPProxyBlocked(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("HTTP_PROXY", "http://proxy:8080")
	os.Setenv("HTTPS_PROXY", "https://proxy:8080")
	os.Setenv("http_proxy", "http://proxy:8080")
	os.Setenv("https_proxy", "https://proxy:8080")

	// Create server config with mode=all (should still block HTTP_PROXY variants)
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritAll,
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify HTTP_PROXY variants are blocked
	proxyVars := []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"}
	for _, varName := range proxyVars {
		if _, ok := resultMap[varName]; ok {
			t.Errorf("%s should be blocked (implicit denylist)", varName)
		}
	}

	// Verify other vars still work
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited")
	}
}

// TestBuildEnvironment_OverridePrecedence tests that explicit overrides win over inherited
func TestBuildEnvironment_OverridePrecedence(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("CUSTOM", "parent-value")

	// Create server config with tier1 mode and explicit overrides
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritTier1,
		},
		Env: map[string]string{
			"PATH":   "/custom/bin",
			"CUSTOM": "override-value",
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify overrides win
	if resultMap["PATH"] != "/custom/bin" {
		t.Errorf("PATH override should win, got %q", resultMap["PATH"])
	}
	if resultMap["CUSTOM"] != "override-value" {
		t.Errorf("CUSTOM override should win, got %q", resultMap["CUSTOM"])
	}

	// Verify overrides bypass denylist
	os.Setenv("HTTP_PROXY", "http://parent:8080")
	serverCfg.Env["HTTP_PROXY"] = "http://override:8080"

	result = BuildEnvironment(serverCfg, nil)
	resultMap = sliceToMap(result)

	if resultMap["HTTP_PROXY"] != "http://override:8080" {
		t.Errorf("HTTP_PROXY override should bypass denylist, got %q", resultMap["HTTP_PROXY"])
	}
}

// TestBuildEnvironment_Windows tests Windows case-insensitive behavior
func TestBuildEnvironment_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test")
	}

	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment
	os.Clearenv()
	os.Setenv("PATH", "C:\\Windows\\System32")
	os.Setenv("HOME", "C:\\Users\\test")

	// Create server config with case-variant override
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode: config.InheritTier1,
		},
		Env: map[string]string{
			"Path": "C:\\Custom\\Path", // Different case
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// On Windows, PATH and Path should be treated as the same variable
	pathValue := ""
	pathKey := ""
	for key, val := range resultMap {
		if strings.ToUpper(key) == "PATH" {
			pathValue = val
			pathKey = key
			break
		}
	}

	if pathValue != "C:\\Custom\\Path" {
		t.Errorf("Expected PATH to be overridden (case-insensitive), got key=%q value=%q", pathKey, pathValue)
	}

	// Verify only one PATH variant exists
	pathCount := 0
	for key := range resultMap {
		if strings.ToUpper(key) == "PATH" {
			pathCount++
		}
	}
	if pathCount != 1 {
		t.Errorf("Expected exactly 1 PATH variant, got %d: %v", pathCount, resultMap)
	}

	// HOME should still be present
	if _, ok := resultMap["HOME"]; !ok {
		t.Error("HOME should be inherited")
	}
}

// TestBuildEnvironment_LocaleVariables tests that LC_* locale vars can be inherited via prefix
func TestBuildEnvironment_LocaleVariables(t *testing.T) {
	// Save and restore environment
	oldEnv := os.Environ()
	defer restoreEnvironment(oldEnv)

	// Set up test environment with various LC_* vars
	os.Clearenv()
	os.Setenv("HOME", "/home/user")
	os.Setenv("LANG", "en_US.UTF-8")
	os.Setenv("LC_ALL", "en_US.UTF-8")
	os.Setenv("LC_TIME", "en_GB.UTF-8")
	os.Setenv("LC_NUMERIC", "de_DE.UTF-8")
	os.Setenv("LC_MONETARY", "fr_FR.UTF-8")
	os.Setenv("OTHER_VAR", "should-not-match")

	// Create server config with tier1 mode + LC_ prefix to capture all locale vars
	serverCfg := &config.ServerConfig{
		Inherit: &config.InheritConfig{
			Mode:   config.InheritTier1,
			Prefix: []string{"LC_"},
		},
	}

	// Build environment
	result := BuildEnvironment(serverCfg, nil)
	resultMap := sliceToMap(result)

	// Verify base locale vars are present (LANG and LC_ALL are in Tier1)
	if resultMap["LANG"] != "en_US.UTF-8" {
		t.Error("LANG should be inherited (tier1 var)")
	}
	if resultMap["LC_ALL"] != "en_US.UTF-8" {
		t.Error("LC_ALL should be inherited (tier1 var)")
	}

	// Verify all LC_* vars are present (via prefix matching)
	localeVars := []string{"LC_TIME", "LC_NUMERIC", "LC_MONETARY"}
	for _, varName := range localeVars {
		if _, ok := resultMap[varName]; !ok {
			t.Errorf("%s should be inherited (LC_* prefix match)", varName)
		}
	}

	// Verify non-LC_* var is NOT present
	if _, ok := resultMap["OTHER_VAR"]; ok {
		t.Error("OTHER_VAR should NOT be inherited")
	}
}

// restoreEnvironment restores the environment to a previous state
func restoreEnvironment(oldEnv []string) {
	os.Clearenv()
	for _, entry := range oldEnv {
		key, value := splitEnvEntry(entry)
		if key != "" {
			os.Setenv(key, value)
		}
	}
}
