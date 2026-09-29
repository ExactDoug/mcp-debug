package config

import (
	"fmt"
	"os"
	
	"gopkg.in/yaml.v3"
)

// LoadConfig loads and validates the proxy configuration from a file
func LoadConfig(path string) (*ProxyConfig, error) {
	// Read configuration file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing config file: return empty config (zero servers).
			// Proxy starts with management tools only (server_add, etc.).
			return &ProxyConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	// Parse YAML
	var config ProxyConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}
	
	// Expand environment variables
	config.ExpandEnvVars()
	
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return &config, nil
}

// LoadConfigFromString loads configuration from a YAML string (for testing)
func LoadConfigFromString(yamlData string) (*ProxyConfig, error) {
	var config ProxyConfig
	if err := yaml.Unmarshal([]byte(yamlData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}
	
	// Expand environment variables
	config.ExpandEnvVars()
	
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return &config, nil
}