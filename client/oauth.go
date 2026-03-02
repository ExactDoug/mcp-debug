package client

import (
	"bytes"
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

	// Optional: external callback handler (dashboard). Replaces ephemeral server.
	callbackHandler CallbackHandler
	// Optional: server name for event tracking and callback registration.
	serverName string
	// Optional: callback for auth events (published to dashboard event bus).
	onAuthEvent func(serverName, message string)
	// Optional: callback for auth success (e.g., trigger auto-reconnect).
	onAuthSuccess func()
	// When true, RefreshToken returns an error instead of starting interactive auth.
	// Used during discovery to prevent blocking on browser-based OAuth flows.
	passiveMode bool
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
	return NewOAuthProvider(OAuthConfig{
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientSecret,
		Scopes:       auth.Scopes,
		RedirectPort: auth.RedirectPort,
		TokenFile:    auth.TokenFile,
		ServerURL:    serverURL,
	}), nil
}

// SetCallbackHandler sets an external callback handler (e.g., dashboard).
// When set, OAuth flows use this instead of starting an ephemeral server.
func (p *OAuthProvider) SetCallbackHandler(h CallbackHandler) {
	p.callbackHandler = h
}

// SetServerName sets the server name for event tracking.
func (p *OAuthProvider) SetServerName(name string) {
	p.serverName = name
}

// SetAuthEventFunc sets a callback for auth events (e.g., dashboard event bus).
func (p *OAuthProvider) SetAuthEventFunc(fn func(serverName, message string)) {
	p.onAuthEvent = fn
}

// SetAuthSuccessFunc sets a callback that fires after successful authentication.
// Used by the integration layer to trigger auto-reconnect after dashboard-initiated auth.
func (p *OAuthProvider) SetAuthSuccessFunc(fn func()) {
	p.onAuthSuccess = fn
}

// SetRedirectPort overrides the redirect port used for OAuth redirect_uri construction.
// Called by the integration layer to sync with the dashboard's actual bound port.
func (p *OAuthProvider) SetRedirectPort(port int) {
	p.redirectPort = port
}

// SetPassiveMode configures the provider to only use cached/refreshed tokens.
// When true, RefreshToken returns an error instead of starting an interactive
// browser-based OAuth flow. Used during discovery to prevent blocking.
func (p *OAuthProvider) SetPassiveMode(passive bool) {
	p.passiveMode = passive
}

// ApplyAuth sets the Authorization header with the current access token.
func (p *OAuthProvider) ApplyAuth(req *http.Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to load cached token if we don't have one
	if p.token == nil && p.tokenStore != nil {
		if cached, err := p.tokenStore.Load(); err == nil && cached != nil {
			p.token = cached
			if p.clientID == "" && cached.ClientID != "" {
				p.clientID = cached.ClientID
				p.clientSecret = cached.ClientSecret
			}
		}
	}

	// If we have a valid token, use it
	if p.token != nil && !p.token.IsExpired() {
		req.Header.Set("Authorization", "Bearer "+p.token.AccessToken)
		return nil
	}

	// If token is expired but we have a refresh token, let 401 handler deal with it
	if p.token != nil && p.token.RefreshToken != "" && p.token.IsExpired() {
		log.Printf("[DEBUG] OAuthProvider: token expired, will be refreshed on 401")
	}

	return nil
}

// RefreshToken handles 401 responses by refreshing or re-obtaining the token.
func (p *OAuthProvider) RefreshToken(ctx context.Context, wwwAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// First try: refresh token
	if p.token != nil && p.token.RefreshToken != "" {
		log.Printf("[DEBUG] OAuthProvider: attempting token refresh")
		if err := p.refreshAccessToken(ctx, wwwAuth); err == nil {
			return nil
		}
		log.Printf("[DEBUG] OAuthProvider: refresh failed, falling back to full auth flow")
	}

	// In passive mode, don't attempt interactive auth — fail fast
	if p.passiveMode {
		return fmt.Errorf("authentication required (no cached token; use dashboard to authenticate)")
	}

	// Full OAuth flow
	return p.fullAuthorizationFlow(ctx, wwwAuth)
}

// GetTokenStatus returns the current auth status for dashboard display.
func (p *OAuthProvider) GetTokenStatus() *TokenStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token == nil && p.tokenStore != nil {
		if cached, _ := p.tokenStore.Load(); cached != nil {
			p.token = cached
		}
	}

	if p.token == nil {
		return &TokenStatus{Status: "needs_auth"}
	}

	if p.token.IsExpired() {
		if p.token.RefreshToken != "" {
			return &TokenStatus{Status: "expired", Scopes: p.token.Scopes, ClientID: p.clientID}
		}
		return &TokenStatus{Status: "needs_auth", Scopes: p.token.Scopes, ClientID: p.clientID}
	}

	remaining := time.Until(p.token.ExpiresAt)
	return &TokenStatus{
		Status:       "authenticated",
		ExpiresInMin: int(remaining.Minutes()),
		Scopes:       p.token.Scopes,
		ClientID:     p.clientID,
	}
}

// InitiateAuthFlow starts an OAuth flow proactively (e.g., from dashboard).
// Returns the authorization URL. The flow completes asynchronously when
// the callback is received.
func (p *OAuthProvider) InitiateAuthFlow(ctx context.Context) (string, error) {
	p.mu.Lock()

	flow, err := p.prepareOAuthFlow(ctx, "")
	if err != nil {
		p.mu.Unlock()
		return "", err
	}

	// Register callback
	var codeCh <-chan string
	var errCh <-chan error
	var callbackServer *http.Server

	if p.callbackHandler != nil {
		_, codeCh, errCh = p.callbackHandler.RegisterPending(flow.state, p.serverName)
	} else {
		code := make(chan string, 1)
		errC := make(chan error, 1)
		codeCh = code
		errCh = errC
		callbackServer = p.startCallbackServer(flow.state, code, errC)
	}

	p.mu.Unlock()

	if p.onAuthEvent != nil {
		p.onAuthEvent(p.serverName, "OAuth flow initiated from dashboard")
	}

	// Complete flow asynchronously
	go func() {
		var authCode string
		select {
		case authCode = <-codeCh:
			// Got the code
		case err := <-errCh:
			log.Printf("[DEBUG] OAuthProvider: dashboard auth error: %v", err)
			if p.callbackHandler != nil {
				p.callbackHandler.UnregisterPending(flow.state)
			}
			if callbackServer != nil {
				shutdownServer(callbackServer)
			}
			if p.onAuthEvent != nil {
				p.onAuthEvent(p.serverName, fmt.Sprintf("Authentication failed: %v", err))
			}
			return
		case <-time.After(5 * time.Minute):
			log.Printf("[DEBUG] OAuthProvider: dashboard auth timed out")
			if p.callbackHandler != nil {
				p.callbackHandler.UnregisterPending(flow.state)
			}
			if callbackServer != nil {
				shutdownServer(callbackServer)
			}
			return
		}

		if p.callbackHandler != nil {
			p.callbackHandler.UnregisterPending(flow.state)
		}
		if callbackServer != nil {
			shutdownServer(callbackServer)
		}

		p.mu.Lock()
		defer p.mu.Unlock()

		if err := p.completeTokenExchange(context.Background(), flow, authCode); err != nil {
			log.Printf("[DEBUG] OAuthProvider: dashboard auth token exchange failed: %v", err)
			if p.onAuthEvent != nil {
				p.onAuthEvent(p.serverName, fmt.Sprintf("Token exchange failed: %v", err))
			}
			return
		}

		log.Printf("[DEBUG] OAuthProvider: dashboard-initiated auth successful")
		if p.onAuthEvent != nil {
			p.onAuthEvent(p.serverName, "Authentication successful")
		}
		if p.onAuthSuccess != nil {
			p.onAuthSuccess()
		}
	}()

	return flow.authURL, nil
}

// --- Internal types and methods ---

// authServerMetadata holds discovered authorization server endpoints.
type authServerMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
}

// resourceMetadata holds protected resource metadata (RFC 9728).
type resourceMetadata struct {
	AuthorizationServers []string `json:"authorization_servers"`
	Resource             string   `json:"resource"`
}

// oauthFlowState holds the prepared state for an OAuth flow.
type oauthFlowState struct {
	asMeta       *authServerMetadata
	codeVerifier string
	state        string
	redirectURI  string
	authURL      string
}

// prepareOAuthFlow does discovery, registration, PKCE setup, and builds the auth URL.
// Must be called with p.mu held.
func (p *OAuthProvider) prepareOAuthFlow(ctx context.Context, wwwAuth string) (*oauthFlowState, error) {
	// Discover authorization server
	asMeta, err := p.discoverAuthServer(ctx, wwwAuth)
	if err != nil {
		return nil, fmt.Errorf("auth server discovery failed: %w", err)
	}

	// Dynamic client registration if no client_id configured
	if p.clientID == "" {
		if p.tokenStore != nil {
			if cached, err := p.tokenStore.Load(); err == nil && cached != nil && cached.ClientID != "" {
				log.Printf("[DEBUG] OAuthProvider: using cached client registration")
				p.clientID = cached.ClientID
				p.clientSecret = cached.ClientSecret
			}
		}

		if p.clientID == "" && asMeta.RegistrationEndpoint != "" {
			log.Printf("[DEBUG] OAuthProvider: performing dynamic client registration at %s", asMeta.RegistrationEndpoint)
			clientID, clientSecret, err := p.registerClient(ctx, asMeta.RegistrationEndpoint)
			if err != nil {
				return nil, fmt.Errorf("dynamic client registration failed: %w", err)
			}
			p.clientID = clientID
			p.clientSecret = clientSecret
			log.Printf("[DEBUG] OAuthProvider: registered as client %s", clientID)
		}

		if p.clientID == "" {
			return nil, fmt.Errorf("no client_id configured and server does not support dynamic registration")
		}
	}

	// Generate PKCE parameters
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Generate state parameter for CSRF protection
	state, err := generateRandomString(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", p.redirectPort)
	authURL := buildAuthorizationURL(asMeta.AuthorizationEndpoint, p.clientID, redirectURI, p.scopes, state, codeChallenge)

	return &oauthFlowState{
		asMeta:       asMeta,
		codeVerifier: codeVerifier,
		state:        state,
		redirectURI:  redirectURI,
		authURL:      authURL,
	}, nil
}

// completeTokenExchange exchanges the auth code for tokens and stores them.
// Must be called with p.mu held.
func (p *OAuthProvider) completeTokenExchange(ctx context.Context, flow *oauthFlowState, authCode string) error {
	log.Printf("[DEBUG] OAuthProvider: exchanging authorization code for tokens")
	tokenData, err := p.exchangeCode(ctx, flow.asMeta.TokenEndpoint, authCode, flow.codeVerifier, flow.redirectURI)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	p.token = tokenData
	p.token.ClientID = p.clientID
	p.token.ClientSecret = p.clientSecret
	if p.tokenStore != nil {
		if err := p.tokenStore.Save(p.token); err != nil {
			log.Printf("[DEBUG] OAuthProvider: failed to save token: %v", err)
		}
	}

	return nil
}

// fullAuthorizationFlow performs the complete PKCE authorization code flow.
// Called with p.mu held (from RefreshToken).
func (p *OAuthProvider) fullAuthorizationFlow(ctx context.Context, wwwAuth string) error {
	flow, err := p.prepareOAuthFlow(ctx, wwwAuth)
	if err != nil {
		return err
	}

	// Register callback with external handler or start ephemeral server
	var codeCh <-chan string
	var errCh <-chan error
	var callbackServer *http.Server

	if p.callbackHandler != nil {
		_, codeCh, errCh = p.callbackHandler.RegisterPending(flow.state, p.serverName)
	} else {
		code := make(chan string, 1)
		errC := make(chan error, 1)
		codeCh = code
		errCh = errC
		callbackServer = p.startCallbackServer(flow.state, code, errC)
	}

	// Open browser
	log.Printf("[DEBUG] OAuthProvider: opening browser for authorization")
	fmt.Fprintf(log.Writer(), "\n=== MCP OAuth Authentication ===\n")
	fmt.Fprintf(log.Writer(), "Opening browser for authentication...\n")
	fmt.Fprintf(log.Writer(), "If the browser doesn't open, visit:\n%s\n", flow.authURL)
	fmt.Fprintf(log.Writer(), "Dashboard: http://localhost:%d\n", p.redirectPort)
	fmt.Fprintf(log.Writer(), "================================\n\n")

	if err := openBrowser(flow.authURL); err != nil {
		log.Printf("[DEBUG] OAuthProvider: failed to open browser: %v", err)
	}

	if p.onAuthEvent != nil {
		p.onAuthEvent(p.serverName, "Opening browser for authentication")
	}

	// Wait for callback with timeout
	var authCode string
	select {
	case authCode = <-codeCh:
		// Got the code
	case err := <-errCh:
		p.cleanupFlow(flow.state, callbackServer)
		return fmt.Errorf("callback error: %w", err)
	case <-ctx.Done():
		p.cleanupFlow(flow.state, callbackServer)
		return fmt.Errorf("authorization timed out: %w", ctx.Err())
	case <-time.After(5 * time.Minute):
		p.cleanupFlow(flow.state, callbackServer)
		return fmt.Errorf("authorization timed out after 5 minutes")
	}

	p.cleanupFlow(flow.state, callbackServer)

	if err := p.completeTokenExchange(ctx, flow, authCode); err != nil {
		return err
	}

	log.Printf("[DEBUG] OAuthProvider: authentication successful")
	if p.onAuthEvent != nil {
		p.onAuthEvent(p.serverName, "Authentication successful")
	}
	return nil
}

// cleanupFlow cleans up after an OAuth flow (unregisters callback or shuts down server).
func (p *OAuthProvider) cleanupFlow(state string, callbackServer *http.Server) {
	if p.callbackHandler != nil {
		p.callbackHandler.UnregisterPending(state)
	}
	if callbackServer != nil {
		shutdownServer(callbackServer)
	}
}

// refreshAccessToken uses the refresh token to get a new access token.
func (p *OAuthProvider) refreshAccessToken(ctx context.Context, wwwAuth string) error {
	asMeta, err := p.discoverAuthServer(ctx, wwwAuth)
	if err != nil {
		return fmt.Errorf("auth server discovery for refresh failed: %w", err)
	}

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
	if p.onAuthEvent != nil {
		p.onAuthEvent(p.serverName, "Token refreshed successfully")
	}
	return nil
}

// --- Discovery, registration, and token exchange ---

// discoverAuthServer discovers the authorization server from the MCP server.
func (p *OAuthProvider) discoverAuthServer(ctx context.Context, wwwAuth string) (*authServerMetadata, error) {
	resourceMetadataURL := parseResourceMetadataURL(wwwAuth)

	if resourceMetadataURL == "" {
		serverBase, err := getBaseURL(p.serverURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse server URL: %w", err)
		}
		resourceMetadataURL = serverBase + "/.well-known/oauth-protected-resource"
	}

	log.Printf("[DEBUG] OAuthProvider: fetching resource metadata from %s", resourceMetadataURL)
	resMeta, err := fetchJSON[resourceMetadata](ctx, resourceMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resource metadata: %w", err)
	}

	if len(resMeta.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("resource metadata has no authorization_servers")
	}

	authServerURL := strings.TrimRight(resMeta.AuthorizationServers[0], "/")
	asMetadataURL := authServerURL + "/.well-known/openid-configuration"

	log.Printf("[DEBUG] OAuthProvider: fetching auth server metadata from %s", asMetadataURL)
	asMeta, err := fetchJSON[authServerMetadata](ctx, asMetadataURL)
	if err != nil {
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

// registerClient performs RFC 7591 Dynamic Client Registration.
func (p *OAuthProvider) registerClient(ctx context.Context, registrationEndpoint string) (clientID, clientSecret string, err error) {
	regData := map[string]interface{}{
		"client_name":                "mcp-debug",
		"redirect_uris":             []string{fmt.Sprintf("http://localhost:%d/callback", p.redirectPort)},
		"grant_types":               []string{"authorization_code", "refresh_token"},
		"response_types":            []string{"code"},
		"token_endpoint_auth_method": "client_secret_post",
		"application_type":          "native",
	}

	body, err := json.Marshal(regData)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("failed to create registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read registration response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("registration endpoint returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var regResp struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return "", "", fmt.Errorf("failed to parse registration response: %w", err)
	}

	if regResp.ClientID == "" {
		return "", "", fmt.Errorf("registration response missing client_id")
	}

	return regResp.ClientID, regResp.ClientSecret, nil
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
// Used as fallback when no external CallbackHandler is set.
func (p *OAuthProvider) startCallbackServer(expectedState string, codeCh chan<- string, errCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state != expectedState {
			errCh <- fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			errCh <- fmt.Errorf("OAuth error: %s - %s", errMsg, desc)
			fmt.Fprintf(w, "<html><body><h1>Authentication Failed</h1><p>%s: %s</p><p>You can close this window.</p></body></html>", errMsg, desc)
			return
		}

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

func parseResourceMetadataURL(wwwAuth string) string {
	if wwwAuth == "" {
		return ""
	}

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

func getBaseURL(fullURL string) (string, error) {
	u, err := url.Parse(fullURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}

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

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

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

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "linux":
		if isWSL() {
			cmd = "powershell.exe"
			args = []string{"-NoProfile", "-Command", fmt.Sprintf("Start-Process '%s'", url)}
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

func isWSL() bool {
	_, err := exec.LookPath("cmd.exe")
	return err == nil
}

func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
