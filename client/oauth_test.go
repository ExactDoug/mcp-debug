package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcp-debug/config"
)

func TestGenerateCodeVerifier(t *testing.T) {
	v1, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v1) < 43 {
		t.Errorf("verifier too short: %d chars", len(v1))
	}

	// Should be unique
	v2, _ := generateCodeVerifier()
	if v1 == v2 {
		t.Error("verifiers should be unique")
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := generateCodeChallenge(verifier)

	if challenge == "" {
		t.Error("challenge should not be empty")
	}
	if challenge == verifier {
		t.Error("challenge should differ from verifier")
	}
	// S256 challenge should be base64url encoded
	if len(challenge) != 43 { // SHA256 = 32 bytes → 43 base64url chars
		t.Errorf("expected 43 char challenge, got %d", len(challenge))
	}
}

func TestParseResourceMetadataURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard format",
			input:    `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`,
			expected: "https://mcp.example.com/.well-known/oauth-protected-resource",
		},
		{
			name:     "with additional params",
			input:    `Bearer realm="mcp", resource_metadata="https://auth.example.com/.well-known/oauth-protected-resource"`,
			expected: "https://auth.example.com/.well-known/oauth-protected-resource",
		},
		{
			name:     "empty header",
			input:    "",
			expected: "",
		},
		{
			name:     "no resource_metadata",
			input:    "Bearer",
			expected: "",
		},
		{
			name:     "bearer only",
			input:    `Bearer realm="example"`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseResourceMetadataURL(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildAuthorizationURL(t *testing.T) {
	url := buildAuthorizationURL(
		"https://login.example.com/authorize",
		"my-client-id",
		"http://localhost:8100/callback",
		"openid profile",
		"random-state",
		"challenge-value",
	)

	// Verify required parameters are present
	if url == "" {
		t.Fatal("URL should not be empty")
	}
	for _, param := range []string{
		"response_type=code",
		"client_id=my-client-id",
		"state=random-state",
		"code_challenge=challenge-value",
		"code_challenge_method=S256",
		"scope=openid+profile",
	} {
		if !containsParam(url, param) {
			t.Errorf("URL missing parameter: %s", param)
		}
	}
}

func containsParam(url, param string) bool {
	return len(url) > 0 && (contains(url, param) || contains(url, param))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGetBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://mcp.example.com/mcp", "https://mcp.example.com"},
		{"http://localhost:8080/api/mcp", "http://localhost:8080"},
		{"https://host.com:443/path/to/mcp", "https://host.com:443"},
	}

	for _, tt := range tests {
		result, err := getBaseURL(tt.input)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", tt.input, err)
		}
		if result != tt.expected {
			t.Errorf("for %s: expected %s, got %s", tt.input, tt.expected, result)
		}
	}
}

func TestNewOAuthProviderFromConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		auth := &config.AuthConfig{
			Type:         "oauth",
			ClientID:     "my-client",
			ClientSecret: "my-secret",
			Scopes:       "openid",
			TokenFile:    "/tmp/test-tokens.json",
			RedirectPort: 9090,
		}

		provider, err := NewOAuthProviderFromConfig(auth, "https://mcp.example.com/mcp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		if provider.clientID != "my-client" {
			t.Errorf("expected clientID my-client, got %s", provider.clientID)
		}
		if provider.redirectPort != 9090 {
			t.Errorf("expected redirect port 9090, got %d", provider.redirectPort)
		}
	})

	t.Run("no client_id uses dynamic registration", func(t *testing.T) {
		auth := &config.AuthConfig{
			Type:      "oauth",
			TokenFile: "/tmp/test-dyn-reg.json",
		}

		provider, err := NewOAuthProviderFromConfig(auth, "https://example.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		// client_id should be empty — will be set during dynamic registration
		if provider.clientID != "" {
			t.Errorf("expected empty clientID, got %s", provider.clientID)
		}
	})

	t.Run("default redirect port", func(t *testing.T) {
		auth := &config.AuthConfig{
			Type:     "oauth",
			ClientID: "test",
		}

		provider, _ := NewOAuthProviderFromConfig(auth, "https://example.com")
		if provider.redirectPort != 8100 {
			t.Errorf("expected default port 8100, got %d", provider.redirectPort)
		}
	})
}

func TestOAuthProvider_ApplyAuth_WithCachedToken(t *testing.T) {
	provider := &OAuthProvider{
		clientID: "test",
		token: &TokenData{
			AccessToken: "cached-token-123",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		},
	}

	req, _ := http.NewRequest("POST", "http://example.com/mcp", nil)
	err := provider.ApplyAuth(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := req.Header.Get("Authorization")
	if got != "Bearer cached-token-123" {
		t.Errorf("expected 'Bearer cached-token-123', got %q", got)
	}
}

func TestOAuthProvider_ApplyAuth_NoToken(t *testing.T) {
	provider := &OAuthProvider{
		clientID: "test",
	}

	req, _ := http.NewRequest("POST", "http://example.com/mcp", nil)
	err := provider.ApplyAuth(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No token available — request should proceed without auth header
	if req.Header.Get("Authorization") != "" {
		t.Error("expected no Authorization header when no token available")
	}
}

func TestOAuthProvider_TokenExchange(t *testing.T) {
	// Mock token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("expected grant_type authorization_code, got %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "test-auth-code" {
			t.Errorf("expected code test-auth-code, got %s", r.Form.Get("code"))
		}
		if r.Form.Get("client_id") != "test-client" {
			t.Errorf("expected client_id test-client, got %s", r.Form.Get("client_id"))
		}
		if r.Form.Get("code_verifier") == "" {
			t.Error("expected non-empty code_verifier")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	provider := &OAuthProvider{
		clientID: "test-client",
	}

	tokenData, err := provider.exchangeCode(
		context.Background(),
		tokenServer.URL,
		"test-auth-code",
		"test-verifier",
		"http://localhost:8100/callback",
	)
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}

	if tokenData.AccessToken != "new-access-token" {
		t.Errorf("expected access token new-access-token, got %s", tokenData.AccessToken)
	}
	if tokenData.RefreshToken != "new-refresh-token" {
		t.Errorf("expected refresh token new-refresh-token, got %s", tokenData.RefreshToken)
	}
	if tokenData.ExpiresAt.Before(time.Now()) {
		t.Error("token should not be expired")
	}
}

func TestOAuthProvider_TokenRefreshFlow(t *testing.T) {
	// Mock authorization server with metadata and token endpoints
	var metadataRequests int
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			metadataRequests++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": "https://unused/authorize",
				"token_endpoint":         "http://" + r.Host + "/token",
			})
		case "/token":
			r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" {
				t.Errorf("expected grant_type refresh_token, got %s", r.Form.Get("grant_type"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "refreshed-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer authServer.Close()

	// Mock resource metadata server
	resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authorization_servers": []string{authServer.URL},
			"resource":             "https://mcp.example.com",
		})
	}))
	defer resourceServer.Close()

	provider := &OAuthProvider{
		clientID:  "test-client",
		serverURL: "https://mcp.example.com/mcp",
		token: &TokenData{
			AccessToken:  "expired-token",
			RefreshToken: "valid-refresh-token",
			TokenType:    "Bearer",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // expired
		},
	}

	// Build WWW-Authenticate header pointing to our mock resource server
	wwwAuth := `Bearer resource_metadata="` + resourceServer.URL + `"`

	err := provider.RefreshToken(context.Background(), wwwAuth)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if provider.token.AccessToken != "refreshed-token" {
		t.Errorf("expected refreshed-token, got %s", provider.token.AccessToken)
	}
}

func TestHTTPClient_401RetryWithOAuth(t *testing.T) {
	callCount := 0

	// Mock MCP server that returns 401 on first call, 200 on retry
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		callCount++

		if callCount == 1 {
			// First call: return 401
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Second call (retry): check for auth header and return success
		auth := r.Header.Get("Authorization")
		if auth != "Bearer refreshed-token" {
			t.Errorf("expected 'Bearer refreshed-token', got %q", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session")
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
		result, _ := json.Marshal(map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities":   map[string]interface{}{},
			"serverInfo":     map[string]interface{}{"name": "auth-test", "version": "1.0"},
		})
		resp.Result = result
		json.NewEncoder(w).Encode(resp)
	}))
	defer mcpServer.Close()

	// Create a mock auth provider that implements TokenRefresher
	mockAuth := &mockOAuthProvider{
		token: "refreshed-token",
	}

	c := NewHTTPClient("auth-test", mcpServer.URL)
	c.SetAuthProvider(mockAuth)

	ctx := context.Background()
	c.Connect(ctx)

	result, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if result.ServerInfo.Name != "auth-test" {
		t.Errorf("expected server name auth-test, got %s", result.ServerInfo.Name)
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", callCount)
	}
}

// mockOAuthProvider is a test double that implements both AuthProvider and TokenRefresher.
type mockOAuthProvider struct {
	token     string
	refreshed bool
}

func (m *mockOAuthProvider) ApplyAuth(req *http.Request) error {
	if m.refreshed && m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	return nil
}

func (m *mockOAuthProvider) RefreshToken(ctx context.Context, wwwAuth string) error {
	m.refreshed = true
	return nil
}
