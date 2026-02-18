package client

import (
	"fmt"
	"net/http"

	"mcp-debug/config"
)

// AuthProvider is an interface for applying authentication to HTTP requests.
type AuthProvider interface {
	ApplyAuth(req *http.Request) error
}

// CallbackHandler handles OAuth callbacks externally (e.g., via dashboard).
// When set on OAuthProvider, replaces the ephemeral callback server.
type CallbackHandler interface {
	RegisterPending(state, serverName string) (callbackURL string, codeCh <-chan string, errCh <-chan error)
	UnregisterPending(state string)
}

// TokenStatus represents the current auth token status.
type TokenStatus struct {
	Status       string // "authenticated", "expired", "needs_auth", "none"
	ExpiresInMin int
	Scopes       string
	ClientID     string
}

// TokenStatusProvider can report its current token status.
type TokenStatusProvider interface {
	GetTokenStatus() *TokenStatus
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

// GetTokenStatus returns the status of the bearer token.
func (p *BearerTokenProvider) GetTokenStatus() *TokenStatus {
	if p.Token == "" {
		return &TokenStatus{Status: "needs_auth"}
	}
	return &TokenStatus{Status: "authenticated"}
}

// NewAuthProviderFromConfig creates an AuthProvider from the config.AuthConfig.
func NewAuthProviderFromConfig(auth *config.AuthConfig) (AuthProvider, error) {
	return NewAuthProviderFromConfigWithURL(auth, "")
}

// NewAuthProviderFromConfigWithURL creates an AuthProvider with the server URL context.
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
