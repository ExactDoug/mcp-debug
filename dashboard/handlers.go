package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "Dashboard not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.statusProvider == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"servers": []ServerStatus{}})
		return
	}

	statuses := s.statusProvider.GetServerStatuses()
	writeJSON(w, http.StatusOK, map[string]interface{}{"servers": statuses})
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	serverName := strings.TrimPrefix(r.URL.Path, "/api/auth/")
	if serverName == "" {
		http.Error(w, "Server name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleAuthStart(w, r, serverName)
	case http.MethodDelete:
		s.handleAuthRevoke(w, r, serverName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request, serverName string) {
	if s.authTrigger == nil {
		http.Error(w, "Auth trigger not configured", http.StatusServiceUnavailable)
		return
	}

	authURL, err := s.authTrigger.TriggerAuth(r.Context(), serverName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.events.Publish(Event{
		Type:    EventAuth,
		Server:  serverName,
		Message: "OAuth flow started",
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"auth_url": authURL,
		"server":   serverName,
	})
}

func (s *Server) handleAuthRevoke(w http.ResponseWriter, r *http.Request, serverName string) {
	if s.authTrigger == nil {
		http.Error(w, "Auth trigger not configured", http.StatusServiceUnavailable)
		return
	}

	if err := s.authTrigger.RevokeAuth(serverName); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.events.Publish(Event{
		Type:    EventAuth,
		Server:  serverName,
		Message: "Token revoked",
	})

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "server": serverName})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serverName := strings.TrimPrefix(r.URL.Path, "/api/tokens/")
	if serverName == "" {
		http.Error(w, "Server name required", http.StatusBadRequest)
		return
	}

	if s.statusProvider == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"auth": nil})
		return
	}

	for _, status := range s.statusProvider.GetServerStatuses() {
		if status.Name == serverName {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"server": serverName,
				"auth":   status.Auth,
			})
			return
		}
	}

	http.Error(w, "Server not found", http.StatusNotFound)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	_, ch, unsubscribe := s.events.Subscribe()
	defer unsubscribe()

	// Send initial ping
	fmt.Fprint(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := MarshalSSE(event)
			if err != nil {
				continue
			}
			w.Write(data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
