package client

import (
	"net/http"
	"testing"

	"mcp-debug/config"
)

func TestBearerTokenProvider_ApplyAuth(t *testing.T) {
	provider := &BearerTokenProvider{Token: "my-secret-token"}

	req, _ := http.NewRequest("POST", "http://example.com/mcp", nil)
	err := provider.ApplyAuth(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := req.Header.Get("Authorization")
	expected := "Bearer my-secret-token"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestBearerTokenProvider_EmptyToken(t *testing.T) {
	provider := &BearerTokenProvider{Token: ""}

	req, _ := http.NewRequest("POST", "http://example.com/mcp", nil)
	err := provider.ApplyAuth(req)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestNewAuthProviderFromConfig_Bearer(t *testing.T) {
	auth := &config.AuthConfig{
		Type:  "bearer",
		Token: "test-token-123",
	}

	provider, err := NewAuthProviderFromConfig(auth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	req, _ := http.NewRequest("POST", "http://example.com/mcp", nil)
	provider.ApplyAuth(req)
	if req.Header.Get("Authorization") != "Bearer test-token-123" {
		t.Errorf("unexpected header: %s", req.Header.Get("Authorization"))
	}
}

func TestNewAuthProviderFromConfig_BearerMissingToken(t *testing.T) {
	auth := &config.AuthConfig{
		Type: "bearer",
	}

	_, err := NewAuthProviderFromConfig(auth)
	if err == nil {
		t.Fatal("expected error for bearer with no token")
	}
}

func TestNewAuthProviderFromConfig_Nil(t *testing.T) {
	provider, err := NewAuthProviderFromConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != nil {
		t.Error("expected nil provider for nil config")
	}
}

func TestNewAuthProviderFromConfig_EmptyType(t *testing.T) {
	auth := &config.AuthConfig{}

	provider, err := NewAuthProviderFromConfig(auth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != nil {
		t.Error("expected nil provider for empty type")
	}
}

func TestNewAuthProviderFromConfig_UnsupportedType(t *testing.T) {
	auth := &config.AuthConfig{
		Type: "kerberos",
	}

	_, err := NewAuthProviderFromConfig(auth)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
