package dashboard

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

//go:embed assets/index.html
var assetsFS embed.FS

// ServerStatus represents the status of an upstream MCP server.
type ServerStatus struct {
	Name      string      `json:"name"`
	Prefix    string      `json:"prefix,omitempty"`
	Transport string      `json:"transport"`
	URL       string      `json:"url,omitempty"`
	Connected bool        `json:"connected"`
	ToolCount int         `json:"tools"`
	Auth      *AuthStatus `json:"auth"`
	Error     string      `json:"error,omitempty"`
}

// AuthStatus represents the authentication state for a server.
type AuthStatus struct {
	Type            string `json:"type"`
	Status          string `json:"status"` // "authenticated", "expired", "needs_auth"
	TokenExpiresMin int    `json:"token_expires_in_minutes,omitempty"`
	Scopes          string `json:"scopes,omitempty"`
	ClientID        string `json:"client_id,omitempty"`
}

// ServerStatusProvider is implemented by the integration layer to give
// the dashboard access to server state.
type ServerStatusProvider interface {
	GetServerStatuses() []ServerStatus
}

// AuthTrigger is implemented by the integration layer to allow
// the dashboard to initiate OAuth flows.
type AuthTrigger interface {
	TriggerAuth(ctx context.Context, serverName string) (authURL string, err error)
	RevokeAuth(serverName string) error
}

// Server is the persistent dashboard web server.
type Server struct {
	httpServer     *http.Server
	events         *EventBus
	callbacks      *CallbackRegistry
	statusProvider ServerStatusProvider
	authTrigger    AuthTrigger
	port           int // configured/preferred port
	portRange      int // number of ports to try
	actualPort     int // actual bound port (set by Start)
}

// NewServer creates a new dashboard server.
func NewServer(port int, portRange int) *Server {
	if port == 0 {
		port = 8100
	}
	if portRange < 1 {
		portRange = 1
	}

	callbacks := NewCallbackRegistry(port)
	events := NewEventBus()

	s := &Server{
		events:    events,
		callbacks: callbacks,
		port:      port,
		portRange: portRange,
	}

	mux := http.NewServeMux()

	// Dashboard SPA
	mux.HandleFunc("/", s.handleIndex)

	// API endpoints
	mux.HandleFunc("/api/servers", s.handleServers)
	mux.HandleFunc("/api/auth/", s.handleAuth)
	mux.HandleFunc("/api/tokens/", s.handleTokens)
	mux.HandleFunc("/api/events", s.handleEvents)

	// OAuth callback (shared with STDIO-triggered flows)
	mux.HandleFunc("/callback", callbacks.HandleCallback)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: withCSRF(mux),
	}

	return s
}

// SetStatusProvider sets the server status provider.
func (s *Server) SetStatusProvider(provider ServerStatusProvider) {
	s.statusProvider = provider
}

// SetAuthTrigger sets the auth trigger.
func (s *Server) SetAuthTrigger(trigger AuthTrigger) {
	s.authTrigger = trigger
}

// Events returns the event bus for publishing events from other components.
func (s *Server) Events() *EventBus {
	return s.events
}

// Callbacks returns the callback registry for OAuth flow coordination.
func (s *Server) Callbacks() *CallbackRegistry {
	return s.callbacks
}

// Port returns the actual bound port (after Start), or the configured port if not yet started.
func (s *Server) Port() int {
	if s.actualPort != 0 {
		return s.actualPort
	}
	return s.port
}

// Start starts the dashboard server in a goroutine.
// Tries ports in the configured range until one is available.
func (s *Server) Start() error {
	var listener net.Listener
	var boundPort int
	var lastErr error

	for i := 0; i < s.portRange; i++ {
		candidate := s.port + i
		addr := fmt.Sprintf("127.0.0.1:%d", candidate)
		listener, lastErr = net.Listen("tcp", addr)
		if lastErr == nil {
			boundPort = candidate
			break
		}
		if s.portRange > 1 {
			log.Printf("Dashboard port %d unavailable, trying next...", candidate)
		}
	}

	if listener == nil {
		if s.portRange == 1 {
			return fmt.Errorf("dashboard server failed to bind to 127.0.0.1:%d: %w", s.port, lastErr)
		}
		return fmt.Errorf("dashboard server failed to bind to any port in range %d-%d: %w",
			s.port, s.port+s.portRange-1, lastErr)
	}

	// Update actual port and dependent state
	s.actualPort = boundPort
	s.httpServer.Addr = fmt.Sprintf("127.0.0.1:%d", boundPort)
	s.callbacks.SetPort(boundPort)

	go func() {
		log.Printf("Dashboard available at http://localhost:%d", boundPort)
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Dashboard server error: %v", err)
		}
	}()

	// Periodic cleanup of stale pending auths
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.callbacks.CleanupStale(10 * time.Minute)
		}
	}()

	return nil
}

// Stop gracefully stops the dashboard server.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// withCSRF wraps a handler to check X-Requested-With header on mutation requests.
func withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			if r.Header.Get("X-Requested-With") != "mcp-debug" {
				http.Error(w, "Missing X-Requested-With header", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
