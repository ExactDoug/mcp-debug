package dashboard

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// PendingAuth represents an in-progress OAuth authorization flow.
type PendingAuth struct {
	State      string
	ServerName string
	CodeCh     chan string
	ErrCh      chan error
	CreatedAt  time.Time
}

// CallbackRegistry manages pending OAuth authorization flows.
// It provides the /callback endpoint handler and allows OAuthProviders
// to register pending flows instead of starting ephemeral servers.
type CallbackRegistry struct {
	mu      sync.Mutex
	pending map[string]*PendingAuth
	port    int
}

// NewCallbackRegistry creates a new callback registry.
func NewCallbackRegistry(port int) *CallbackRegistry {
	return &CallbackRegistry{
		pending: make(map[string]*PendingAuth),
		port:    port,
	}
}

// SetPort updates the port used for callback URLs.
// Called by Server.Start() when the actual bound port differs from the configured port.
func (r *CallbackRegistry) SetPort(port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.port = port
}

// RegisterPending registers a pending OAuth flow.
// Returns the callback URL and channels for receiving the authorization code or error.
func (r *CallbackRegistry) RegisterPending(state, serverName string) (callbackURL string, codeCh <-chan string, errCh <-chan error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	code := make(chan string, 1)
	errC := make(chan error, 1)

	r.pending[state] = &PendingAuth{
		State:      state,
		ServerName: serverName,
		CodeCh:     code,
		ErrCh:      errC,
		CreatedAt:  time.Now(),
	}

	callbackURL = fmt.Sprintf("http://localhost:%d/callback", r.port)
	return callbackURL, code, errC
}

// UnregisterPending removes a pending auth (cleanup on timeout/cancel).
func (r *CallbackRegistry) UnregisterPending(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, state)
}

// HandleCallback is the HTTP handler for /callback.
func (r *CallbackRegistry) HandleCallback(w http.ResponseWriter, req *http.Request) {
	state := req.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "Missing state parameter", http.StatusBadRequest)
		return
	}

	r.mu.Lock()
	pending, exists := r.pending[state]
	if exists {
		delete(r.pending, state)
	}
	r.mu.Unlock()

	if !exists {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<h1>No Authentication In Progress</h1>
<p>This callback URL is not associated with an active authentication flow.</p>
<p><a href="/">Return to Dashboard</a></p>
</body></html>`)
		return
	}

	// Check for OAuth error
	if errMsg := req.URL.Query().Get("error"); errMsg != "" {
		desc := req.URL.Query().Get("error_description")
		pending.ErrCh <- fmt.Errorf("OAuth error: %s - %s", errMsg, desc)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><body>
<h1>Authentication Failed</h1>
<p>%s: %s</p>
<p><a href="/">Return to Dashboard</a></p>
</body></html>`, errMsg, desc)
		return
	}

	// Extract authorization code
	code := req.URL.Query().Get("code")
	if code == "" {
		pending.ErrCh <- fmt.Errorf("no authorization code in callback")
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	pending.CodeCh <- code
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html><html><body>
<h1>Authentication Successful</h1>
<p>You can close this window and return to mcp-debug.</p>
<p><a href="/">Or return to the Dashboard</a></p>
</body></html>`)
}

// CleanupStale removes pending auths older than the given duration.
func (r *CallbackRegistry) CleanupStale(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for state, pending := range r.pending {
		if pending.CreatedAt.Before(cutoff) {
			pending.ErrCh <- fmt.Errorf("authorization timed out")
			delete(r.pending, state)
		}
	}
}
