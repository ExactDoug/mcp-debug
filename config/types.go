package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// InheritMode defines how environment variables are inherited
type InheritMode string

const (
	InheritNone        InheritMode = "none"
	InheritTier1       InheritMode = "tier1"
	InheritTier1Tier2  InheritMode = "tier1+tier2"
	InheritAll         InheritMode = "all"
)

// InheritConfig controls which environment variables are inherited
type InheritConfig struct {
	Mode                    InheritMode `yaml:"mode,omitempty"`
	Extra                   []string    `yaml:"extra,omitempty"`
	Prefix                  []string    `yaml:"prefix,omitempty"`
	Deny                    []string    `yaml:"deny,omitempty"`
	AllowDeniedIfExplicit   bool        `yaml:"allow_denied_if_explicit,omitempty"`
}

// ProxyConfig represents the main configuration for the proxy server
type ProxyConfig struct {
	Servers []ServerConfig `yaml:"servers"`
	Proxy   ProxySettings  `yaml:"proxy"`
	Inherit *InheritConfig `yaml:"inherit,omitempty"`  // NEW: proxy-level defaults
}

// ServerConfig represents configuration for a remote MCP server
type ServerConfig struct {
	Name      string            `yaml:"name"`
	Prefix    string            `yaml:"prefix"`
	Transport string            `yaml:"transport"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Inherit   *InheritConfig    `yaml:"inherit,omitempty"`  // NEW: per-server inheritance
	URL       string            `yaml:"url,omitempty"`
	Auth      *AuthConfig       `yaml:"auth,omitempty"`
	Timeout   string            `yaml:"timeout,omitempty"`
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	Type     string `yaml:"type"`
	Token    string `yaml:"token,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// ProxySettings represents proxy-level settings
type ProxySettings struct {
	HealthCheckInterval string `yaml:"healthCheckInterval"`
	ConnectionTimeout   string `yaml:"connectionTimeout"`
	MaxRetries          int    `yaml:"maxRetries"`
}

// Validate validates the configuration
func (c *ProxyConfig) Validate() error {
	// Allow empty server lists for dynamic proxies
	if len(c.Servers) == 0 {
		return nil
	}
	
	// Check for unique server names and prefixes
	names := make(map[string]bool)
	prefixes := make(map[string]bool)
	
	for i, server := range c.Servers {
		// Validate server name
		if server.Name == "" {
			return fmt.Errorf("server %d: name is required", i)
		}
		if names[server.Name] {
			return fmt.Errorf("duplicate server name: %s", server.Name)
		}
		names[server.Name] = true
		
		// Validate prefix
		if server.Prefix == "" {
			return fmt.Errorf("server %s: prefix is required", server.Name)
		}
		if prefixes[server.Prefix] {
			return fmt.Errorf("duplicate server prefix: %s", server.Prefix)
		}
		prefixes[server.Prefix] = true
		
		// Validate transport
		if server.Transport != "stdio" && server.Transport != "http" {
			return fmt.Errorf("server %s: transport must be 'stdio' or 'http'", server.Name)
		}
		
		// Validate transport-specific fields
		if server.Transport == "stdio" {
			if server.Command == "" {
				return fmt.Errorf("server %s: command is required for stdio transport", server.Name)
			}
		} else if server.Transport == "http" {
			if server.URL == "" {
				return fmt.Errorf("server %s: url is required for http transport", server.Name)
			}
		}
		
		// Validate timeout format if specified
		if server.Timeout != "" {
			if _, err := time.ParseDuration(server.Timeout); err != nil {
				return fmt.Errorf("server %s: invalid timeout format: %w", server.Name, err)
			}
		}

		// Validate server-level inherit config
		if server.Inherit != nil {
			if err := server.Inherit.Validate(); err != nil {
				return fmt.Errorf("server %s: inherit: %w", server.Name, err)
			}
		}
	}

	// Validate proxy settings
	if c.Proxy.HealthCheckInterval != "" {
		if _, err := time.ParseDuration(c.Proxy.HealthCheckInterval); err != nil {
			return fmt.Errorf("invalid healthCheckInterval format: %w", err)
		}
	}
	
	if c.Proxy.ConnectionTimeout != "" {
		if _, err := time.ParseDuration(c.Proxy.ConnectionTimeout); err != nil {
			return fmt.Errorf("invalid connectionTimeout format: %w", err)
		}
	}

	// Validate proxy-level inherit config
	if c.Inherit != nil {
		if err := c.Inherit.Validate(); err != nil {
			return fmt.Errorf("proxy.inherit: %w", err)
		}
	}

	return nil
}

// ExpandEnvVars expands environment variables in configuration values
func (c *ProxyConfig) ExpandEnvVars() {
	// Expand proxy-level inheritance config
	expandInheritConfig(c.Inherit)

	for i := range c.Servers {
		server := &c.Servers[i]

		// Expand command
		server.Command = expandEnvVar(server.Command)

		// Expand args
		for j := range server.Args {
			server.Args[j] = expandEnvVar(server.Args[j])
		}

		// Expand environment variables
		for key, value := range server.Env {
			server.Env[key] = expandEnvVar(value)
		}

		// Expand URL
		server.URL = expandEnvVar(server.URL)

		// Expand auth fields
		if server.Auth != nil {
			server.Auth.Token = expandEnvVar(server.Auth.Token)
			server.Auth.Username = expandEnvVar(server.Auth.Username)
			server.Auth.Password = expandEnvVar(server.Auth.Password)
		}

		// Expand server-level inheritance config
		expandInheritConfig(server.Inherit)
	}
}

// expandInheritConfig expands environment variables in InheritConfig fields
func expandInheritConfig(ic *InheritConfig) {
	if ic == nil {
		return
	}

	for i := range ic.Extra {
		ic.Extra[i] = expandEnvVar(ic.Extra[i])
	}

	for i := range ic.Prefix {
		ic.Prefix[i] = expandEnvVar(ic.Prefix[i])
	}

	for i := range ic.Deny {
		ic.Deny[i] = expandEnvVar(ic.Deny[i])
	}
}

// expandEnvVar expands environment variables in the format ${VAR}
func expandEnvVar(value string) string {
	if value == "" {
		return value
	}
	
	// Simple expansion of ${VAR} format
	if strings.Contains(value, "${") {
		return os.ExpandEnv(value)
	}
	
	return value
}

// GetServerTimeout returns the timeout duration for a server, with default
func (s *ServerConfig) GetServerTimeout() time.Duration {
	if s.Timeout == "" {
		return 30 * time.Second // default timeout
	}
	
	duration, err := time.ParseDuration(s.Timeout)
	if err != nil {
		return 30 * time.Second // fallback to default
	}
	
	return duration
}

// GetProxySettings returns proxy settings with defaults
func (c *ProxyConfig) GetProxySettings() ProxySettings {
	settings := c.Proxy

	// Apply defaults
	if settings.HealthCheckInterval == "" {
		settings.HealthCheckInterval = "30s"
	}
	if settings.ConnectionTimeout == "" {
		settings.ConnectionTimeout = "10s"
	}
	if settings.MaxRetries == 0 {
		settings.MaxRetries = 3
	}

	return settings
}

// ResolveInheritConfig returns the effective inheritance config for a server.
// Server-level config overrides proxy-level defaults.
func (s *ServerConfig) ResolveInheritConfig(proxyDefault *InheritConfig) *InheritConfig {
	if s.Inherit != nil {
		return s.Inherit
	}
	if proxyDefault != nil {
		return proxyDefault
	}
	// Hardcoded default: tier1 mode
	return &InheritConfig{
		Mode: InheritTier1,
	}
}

// Validate checks that the inheritance configuration is valid
func (ic *InheritConfig) Validate() error {
	// Validate mode
	switch ic.Mode {
	case "", InheritNone, InheritTier1, InheritTier1Tier2, InheritAll:
		// Valid modes (empty defaults to tier1)
	default:
		return fmt.Errorf("invalid mode %q: must be one of: none, tier1, tier1+tier2, all", ic.Mode)
	}

	// Note: mode=none with extras/prefix is valid (inherit nothing except explicitly requested vars)

	return nil
}