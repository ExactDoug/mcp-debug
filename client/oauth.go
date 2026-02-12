package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"mcp-debug/config"
)

// TokenRefresher is optionally implemented by AuthProviders that can handle
// 401 Unauthorized responses by refreshing or re-obtaining tokens.
type TokenRefresher interface {
	// RefreshToken attempts to refresh or re-obtain the auth token.
	// wwwAuth is the WWW-Authenticate header value from the 401 response.
	RefreshToken(ctx context.Context, wwwAuth string) error
}

// OAuthProvider implements AuthProvider with full OAuth 2.1 support.
// It handles PKCE authorization code flow, token persistence, and refresh.
type OAuthProvider struct {
	clientID     string
	clientSecret string
	scopes       string
	redirectPort int
	tokenStore   *TokenStore
	serverURL    string // MCP server URL for metadata discovery

	token *TokenData
	mu    sync.Mutex
}

// OAuthConfig holds configuration for creating an OAuthProvider.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       string
	RedirectPort int
	TokenFile    string
	ServerURL    string
}

// NewOAuthProvider creates a new OAuth 2.1 provider.
func NewOAuthProvider(cfg OAuthConfig) *OAuthProvider {
	var store *TokenStore
	if cfg.TokenFile != "" {
		store = NewTokenStore(cfg.TokenFile)
	}

	port := cfg.RedirectPort
	if port == 0 {
		port = 8100 // default redirect port
	}

	return &OAuthProvider{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scopes:       cfg.Scopes,
		redirectPort: port,
		tokenStore:   store,
		serverURL:    cfg.ServerURL,
	}
}

// NewOAuthProviderFromConfig creates an OAuthProvider from config.AuthConfig.
func NewOAuthProviderFromConfig(auth *config.AuthConfig, serverURL string) (*OAuthProvider, error) {
	if auth.ClientID == "" {
		return nil, fmt.Errorf("oauth auth requires client_id")
	}

	return NewOAuthProvider(OAuthConfig{
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientSecret,
		Scopes:       auth.Scopes,
		RedirectPort: auth.RedirectPort,
		TokenFile:    auth.TokenFile,
		ServerURL:    serverURL,
	}), nil
}

// ApplyAuth sets the Authorization header with the current access token.
// If a cached token is available and not expired, it is used directly.
func (p *OAuthProvider) ApplyAuth(req *http.Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to load cached token if we don't have one
	if p.token == nil && p.tokenStore != nil {
		if cached, err := p.tokenStore.Load(); err == nil && cached != nil {
			p.token = cached
		}
	}

	// If we have a valid token, use it
	if p.token != nil && !p.token.IsExpired() {
		req.Header.Set("Authorization", "Bearer "+p.token.AccessToken)
		return nil
	}

	// If token is expired but we have a refresh token, try refresh
	if p.token != nil && p.token.RefreshToken != "" && p.token.IsExpired() {
		// Attempt refresh in background — if it fails, we'll try the request
		// without auth and let the 401 handler deal with it
		log.Printf("[DEBUG] OAuthProvider: token expired, will be refreshed on 401")
	}

	// No valid token — the request will go without auth.
	// The HTTPClient's 401 handler will call RefreshToken.
	return nil
}

// RefreshToken handles 401 responses by refreshing or re-obtaining the token.
// This implements the TokenRefresher interface.
func (p *OAuthProvider) RefreshToken(ctx context.Context, wwwAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// First try: if we have a refresh token, exchange it
	if p.token != nil && p.token.RefreshToken != "" {
		log.Printf("[DEBUG] OAuthProvider: attempting token refresh")
		if err := p.refreshAccessToken(ctx, wwwAuth); err == nil {
			return nil
		}
		log.Printf("[DEBUG] OAuthProvider: refresh failed, falling back to full auth flow")
	}

	// Full OAuth flow: discover auth server, open browser, exchange code
	return p.fullAuthorizationFlow(ctx, wwwAuth)
}

// authServerMetadata holds discovered authorization server endpoints.
type authServerMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// resourceMetadata holds protected resource metadata (RFC 9728).
type resourceMetadata struct {
	AuthorizationServers []string `json:"authorization_servers"`
	Resource             string   `json:"resource"`
}

// discoverAuthServer discovers the authorization server from the MCP server.
// Follows the MCP spec: parse WWW-Authenticate → fetch resource metadata → fetch auth server metadata.
func (p *OAuthProvider) discoverAuthServer(ctx context.Context, wwwAuth string) (*authServerMetadata, error) {
	// Parse resource_metadata URL from WWW-Authenticate header
	// Format: Bearer resource_metadata="https://..."
	resourceMetadataURL := parseResourceMetadataURL(wwwAuth)

	if resourceMetadataURL == "" {
		// Fall back to well-known endpoint on the MCP server
		serverBase, err := getBaseURL(p.serverURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse server URL: %w", err)
		}
		resourceMetadataURL = serverBase + "/.well-known/oauth-protected-resource"
	}

	// Fetch protected resource metadata
	log.Printf("[DEBUG] OAuthProvider: fetching resource metadata from %s", resourceMetadataURL)
	resMeta, err := fetchJSON[resourceMetadata](ctx, resourceMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resource metadata: %w", err)
	}

	if len(resMeta.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("resource metadata has no authorization_servers")
	}

	// Fetch authorization server metadata
	authServerURL := resMeta.AuthorizationServers[0]
	asMetadataURL := authServerURL + "/.well-known/openid-configuration"

	log.Printf("[DEBUG] OAuthProvider: fetching auth server metadata from %s", asMetadataURL)
	asMeta, err := fetchJSON[authServerMetadata](ctx, asMetadataURL)
	if err != nil {
		// Try OAuth authorization server metadata (RFC 8414) as fallback
		asMetadataURL = authServerURL + "/.well-known/oauth-authorization-server"
		log.Printf("[DEBUG] OAuthProvider: trying RFC 8414 metadata from %s", asMetadataURL)
		asMeta, err = fetchJSON[authServerMetadata](ctx, asMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch auth server metadata: %w", err)
		}
	}

	if asMeta.AuthorizationEndpoint == "" || asMeta.TokenEndpoint == "" {
		return nil, fmt.Errorf("auth server metadata missing required endpoints")
	}

	return asMeta, nil
}

// fullAuthorizationFlow performs the complete PKCE authorization code flow.
func (p *OAuthProvider) fullAuthorizationFlow(ctx context.Context, wwwAuth string) error {
	// Discover authorization server
	asMeta, err := p.discoverAuthServer(ctx, wwwAuth)
	if err != nil {
		return fmt.Errorf("auth server discovery failed: %w", err)
	}

	// Generate PKCE parameters
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Generate state parameter for CSRF protection
	state, err := generateRandomString(32)
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", p.redirectPort)

	// Start local callback server
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := p.startCallbackServer(state, codeCh, errCh)

	// Build authorization URL
	authURL := buildAuthorizationURL(asMeta.AuthorizationEndpoint, p.clientID, redirectURI, p.scopes, state, codeChallenge)

	// Open browser
	log.Printf("[DEBUG] OAuthProvider: opening browser for authorization")
	fmt.Fprintf(log.Writer(), "\n=== MCP OAuth Authentication ===\n")
	fmt.Fprintf(log.Writer(), "Opening browser for authentication...\n")
	fmt.Fprintf(log.Writer(), "If the browser doesn't open, visit:\n%s\n", authURL)
	fmt.Fprintf(log.Writer(), "================================\n\n")

	if err := openBrowser(authURL); err != nil {
		log.Printf("[DEBUG] OAuthProvider: failed to open browser: %v", err)
	}

	// Wait for callback with timeout
	var authCode string
	select {
	case authCode = <-codeCh:
		// Got the code
	case err := <-errCh:
		shutdownServer(server)
		return fmt.Errorf("callback error: %w", err)
	case <-ctx.Done():
		shutdownServer(server)
		return fmt.Errorf("authorization timed out: %w", ctx.Err())
	case <-time.After(5 * time.Minute):
		shutdownServer(server)
		return fmt.Errorf("authorization timed out after 5 minutes")
	}

	shutdownServer(server)

	// Exchange code for tokens
	log.Printf("[DEBUG] OAuthProvider: exchanging authorization code for tokens")
	tokenData, err := p.exchangeCode(ctx, asMeta.TokenEndpoint, authCode, codeVerifier, redirectURI)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	// Store token
	p.token = tokenData
	if p.tokenStore != nil {
		if err := p.tokenStore.Save(tokenData); err != nil {
			log.Printf("[DEBUG] OAuthProvider: failed to save token: %v", err)
		}
	}

	log.Printf("[DEBUG] OAuthProvider: authentication successful")
	return nil
}

// refreshAccessToken uses the refresh token to get a new access token.
func (p *OAuthProvider) refreshAccessToken(ctx context.Context, wwwAuth string) error {
	// Discover auth server to get token endpoint
	asMeta, err := p.discoverAuthServer(ctx, wwwAuth)
	if err != nil {
		return fmt.Errorf("auth server discovery for refresh failed: %w", err)
	}

	// Build refresh request
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {p.token.RefreshToken},
		"client_id":     {p.clientID},
	}
	if p.clientSecret != "" {
		data.Set("client_secret", p.clientSecret)
	}

	tokenData, err := p.doTokenRequest(ctx, asMeta.TokenEndpoint, data)
	if err != nil {
		return err
	}

	// Preserve refresh token if new one wasn't issued
	if tokenData.RefreshToken == "" && p.token != nil {
		tokenData.RefreshToken = p.token.RefreshToken
	}

	p.token = tokenData
	if p.tokenStore != nil {
		if err := p.tokenStore.Save(tokenData); err != nil {
			log.Printf("[DEBUG] OAuthProvider: failed to save refreshed token: %v", err)
		}
	}

	log.Printf("[DEBUG] OAuthProvider: token refresh successful")
	return nil
}

// exchangeCode exchanges an authorization code for tokens.
func (p *OAuthProvider) exchangeCode(ctx context.Context, tokenEndpoint, code, codeVerifier, redirectURI string) (*TokenData, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {p.clientID},
		"code_verifier": {codeVerifier},
	}
	if p.clientSecret != "" {
		data.Set("client_secret", p.clientSecret)
	}

	return p.doTokenRequest(ctx, tokenEndpoint, data)
}

// doTokenRequest performs a token endpoint request and parses the response.
func (p *OAuthProvider) doTokenRequest(ctx context.Context, tokenEndpoint string, data url.Values) (*TokenData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	tokenData := &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scopes:       tokenResp.Scope,
	}

	if tokenResp.ExpiresIn > 0 {
		tokenData.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tokenData, nil
}

// startCallbackServer starts a local HTTP server to receive the OAuth callback.
func (p *OAuthProvider) startCallbackServer(expectedState string, codeCh chan<- string, errCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Check state
		state := r.URL.Query().Get("state")
		if state != expectedState {
			errCh <- fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		// Check for error
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			errCh <- fmt.Errorf("OAuth error: %s - %s", errMsg, desc)
			fmt.Fprintf(w, "<html><body><h1>Authentication Failed</h1><p>%s: %s</p><p>You can close this window.</p></body></html>", errMsg, desc)
			return
		}

		// Extract authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}

		codeCh <- code
		fmt.Fprintf(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to mcp-debug.</p></body></html>")
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", p.redirectPort),
		Handler: mux,
	}

	// Start listener
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		errCh <- fmt.Errorf("failed to start callback server on port %d: %w", p.redirectPort, err)
		return server
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	return server
}

// --- Helper functions ---

// parseResourceMetadataURL extracts the resource_metadata URL from a WWW-Authenticate header.
// Format: Bearer resource_metadata="https://..."
func parseResourceMetadataURL(wwwAuth string) string {
	if wwwAuth == "" {
		return ""
	}

	// Look for resource_metadata="<url>"
	const key = `resource_metadata="`
	idx := strings.Index(wwwAuth, key)
	if idx == -1 {
		return ""
	}

	rest := wwwAuth[idx+len(key):]
	endIdx := strings.Index(rest, `"`)
	if endIdx == -1 {
		return ""
	}

	return rest[:endIdx]
}

// getBaseURL extracts the base URL (scheme + host) from a full URL.
func getBaseURL(fullURL string) (string, error) {
	u, err := url.Parse(fullURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}

// fetchJSON fetches a URL and decodes the JSON response.
func fetchJSON[T any](ctx context.Context, url string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// generateCodeVerifier generates a PKCE code verifier (43-128 chars, base64url).
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generateCodeChallenge generates a PKCE S256 code challenge from a verifier.
func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// generateRandomString generates a random base64url string of the given byte length.
func generateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// buildAuthorizationURL constructs the OAuth authorization URL with all required parameters.
func buildAuthorizationURL(endpoint, clientID, redirectURI, scopes, state, codeChallenge string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	if scopes != "" {
		params.Set("scope", scopes)
	}
	return endpoint + "?" + params.Encode()
}

// openBrowser opens the given URL in the system default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "linux":
		// Check if running in WSL
		if isWSL() {
			cmd = "cmd.exe"
			args = []string{"/c", "start", url}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	return exec.Command(cmd, args...).Start()
}

// isWSL checks if running inside Windows Subsystem for Linux.
func isWSL() bool {
	_, err := exec.LookPath("cmd.exe")
	return err == nil
}

// shutdownServer gracefully shuts down an HTTP server.
func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
