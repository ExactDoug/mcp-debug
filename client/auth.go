package client

import (
	"fmt"
	"net/http"

	"mcp-debug/config"
)

// AuthProvider is an interface for applying authentication to HTTP requests.
// Implementations include BearerTokenProvider and OAuthProvider (Phase 3).
type AuthProvider interface {
	// ApplyAuth adds authentication headers to an HTTP request
	ApplyAuth(req *http.Request) error
}

// BearerTokenProvider implements AuthProvider using a static bearer token.
type BearerTokenProvider struct {
	Token string
}

// ApplyAuth sets the Authorization header with the bearer token.
func (p *BearerTokenProvider) ApplyAuth(req *http.Request) error {
	if p.Token == "" {
		return fmt.Errorf("bearer token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	return nil
}

// NewAuthProviderFromConfig creates an AuthProvider from the config.AuthConfig.
// Returns nil if no auth is configured. For OAuth providers, use
// NewAuthProviderFromConfigWithURL which requires the server URL.
func NewAuthProviderFromConfig(auth *config.AuthConfig) (AuthProvider, error) {
	return NewAuthProviderFromConfigWithURL(auth, "")
}

// NewAuthProviderFromConfigWithURL creates an AuthProvider with the server URL context.
// The URL is needed for OAuth metadata discovery.
func NewAuthProviderFromConfigWithURL(auth *config.AuthConfig, serverURL string) (AuthProvider, error) {
	if auth == nil {
		return nil, nil
	}

	switch auth.Type {
	case "bearer":
		if auth.Token == "" {
			return nil, fmt.Errorf("bearer auth requires a token")
		}
		return &BearerTokenProvider{Token: auth.Token}, nil
	case "oauth":
		return NewOAuthProviderFromConfig(auth, serverURL)
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
}
