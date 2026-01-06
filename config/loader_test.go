package config

import (
	"os"
	"testing"
)

func TestLoadConfigFromString(t *testing.T) {
	yamlData := `
servers:
  - name: "test-server"
    prefix: "test"
    transport: "stdio"
    command: "/usr/bin/test-server"
    args: ["--arg1", "value1"]
    timeout: "30s"

proxy:
  healthCheckInterval: "30s"
  connectionTimeout: "10s"
  maxRetries: 3
`

	cfg, err := LoadConfigFromString(yamlData)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(cfg.Servers))
	}

	server := cfg.Servers[0]
	if server.Name != "test-server" {
		t.Errorf("expected name 'test-server', got '%s'", server.Name)
	}

	if server.Prefix != "test" {
		t.Errorf("expected prefix 'test', got '%s'", server.Prefix)
	}

	if server.Transport != "stdio" {
		t.Errorf("expected transport 'stdio', got '%s'", server.Transport)
	}

	if server.Command != "/usr/bin/test-server" {
		t.Errorf("expected command '/usr/bin/test-server', got '%s'", server.Command)
	}

	if len(server.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(server.Args))
	}

	if server.Timeout != "30s" {
		t.Errorf("expected timeout '30s', got '%s'", server.Timeout)
	}
}

func TestLoadConfigEmptyServers(t *testing.T) {
	yamlData := `
servers: []

proxy:
  healthCheckInterval: "30s"
  connectionTimeout: "10s"
  maxRetries: 3
`

	cfg, err := LoadConfigFromString(yamlData)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.Servers))
	}
}

func TestLoadConfigHTTPTransport(t *testing.T) {
	yamlData := `
servers:
  - name: "http-server"
    prefix: "http"
    transport: "http"
    url: "http://localhost:8080"
    auth:
      type: "bearer"
      token: "test-token"

proxy:
  healthCheckInterval: "30s"
  connectionTimeout: "10s"
  maxRetries: 3
`

	cfg, err := LoadConfigFromString(yamlData)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	server := cfg.Servers[0]
	if server.Transport != "http" {
		t.Errorf("expected transport 'http', got '%s'", server.Transport)
	}

	if server.URL != "http://localhost:8080" {
		t.Errorf("expected url 'http://localhost:8080', got '%s'", server.URL)
	}

	if server.Auth == nil {
		t.Fatal("expected auth to be set")
	}

	if server.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", server.Auth.Type)
	}

	if server.Auth.Token != "test-token" {
		t.Errorf("expected auth token 'test-token', got '%s'", server.Auth.Token)
	}
}

func TestLoadConfigValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		yamlData string
		errMatch string
	}{
		{
			name: "missing server name",
			yamlData: `
servers:
  - prefix: "test"
    transport: "stdio"
    command: "/usr/bin/test"
`,
			errMatch: "name is required",
		},
		{
			name: "missing server prefix",
			yamlData: `
servers:
  - name: "test"
    transport: "stdio"
    command: "/usr/bin/test"
`,
			errMatch: "prefix is required",
		},
		{
			name: "invalid transport",
			yamlData: `
servers:
  - name: "test"
    prefix: "test"
    transport: "invalid"
    command: "/usr/bin/test"
`,
			errMatch: "transport must be 'stdio' or 'http'",
		},
		{
			name: "stdio without command",
			yamlData: `
servers:
  - name: "test"
    prefix: "test"
    transport: "stdio"
`,
			errMatch: "command is required for stdio transport",
		},
		{
			name: "http without url",
			yamlData: `
servers:
  - name: "test"
    prefix: "test"
    transport: "http"
`,
			errMatch: "url is required for http transport",
		},
		{
			name: "duplicate server names",
			yamlData: `
servers:
  - name: "test"
    prefix: "test1"
    transport: "stdio"
    command: "/usr/bin/test1"
  - name: "test"
    prefix: "test2"
    transport: "stdio"
    command: "/usr/bin/test2"
`,
			errMatch: "duplicate server name",
		},
		{
			name: "duplicate prefixes",
			yamlData: `
servers:
  - name: "test1"
    prefix: "test"
    transport: "stdio"
    command: "/usr/bin/test1"
  - name: "test2"
    prefix: "test"
    transport: "stdio"
    command: "/usr/bin/test2"
`,
			errMatch: "duplicate server prefix",
		},
		{
			name: "invalid timeout format",
			yamlData: `
servers:
  - name: "test"
    prefix: "test"
    transport: "stdio"
    command: "/usr/bin/test"
    timeout: "invalid"
`,
			errMatch: "invalid timeout format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfigFromString(tt.yamlData)
			if err == nil {
				t.Fatal("expected error but got none")
			}

			if tt.errMatch != "" {
				if !containsString(err.Error(), tt.errMatch) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errMatch, err.Error())
				}
			}
		})
	}
}

func TestExpandEnvVars(t *testing.T) {
	os.Setenv("TEST_COMMAND", "/usr/bin/from-env")
	os.Setenv("TEST_TOKEN", "secret-token")
	defer os.Unsetenv("TEST_COMMAND")
	defer os.Unsetenv("TEST_TOKEN")

	yamlData := `
servers:
  - name: "test"
    prefix: "test"
    transport: "stdio"
    command: "${TEST_COMMAND}"
    args: ["--token", "${TEST_TOKEN}"]
`

	cfg, err := LoadConfigFromString(yamlData)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	server := cfg.Servers[0]
	if server.Command != "/usr/bin/from-env" {
		t.Errorf("expected command '/usr/bin/from-env', got '%s'", server.Command)
	}

	if len(server.Args) != 2 || server.Args[1] != "secret-token" {
		t.Errorf("expected arg 'secret-token', got '%v'", server.Args)
	}
}

func TestGetServerTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected string
	}{
		{"default timeout", "", "30s"},
		{"custom timeout", "60s", "1m0s"},
		{"invalid timeout", "invalid", "30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := ServerConfig{Timeout: tt.timeout}
			duration := server.GetServerTimeout()
			if duration.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, duration.String())
			}
		})
	}
}

func TestGetProxySettings(t *testing.T) {
	cfg := &ProxyConfig{}
	settings := cfg.GetProxySettings()

	if settings.HealthCheckInterval != "30s" {
		t.Errorf("expected default healthCheckInterval '30s', got '%s'", settings.HealthCheckInterval)
	}

	if settings.ConnectionTimeout != "10s" {
		t.Errorf("expected default connectionTimeout '10s', got '%s'", settings.ConnectionTimeout)
	}

	if settings.MaxRetries != 3 {
		t.Errorf("expected default maxRetries 3, got %d", settings.MaxRetries)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s[1:], substr) || s[:len(substr)] == substr)
}
