package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(filepath.Join(dir, "tokens.json"))

	token := &TokenData{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Scopes:       "openid profile",
	}

	// Save
	if err := store.Save(token); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil token")
	}
	if loaded.AccessToken != "access-123" {
		t.Errorf("expected access token access-123, got %s", loaded.AccessToken)
	}
	if loaded.RefreshToken != "refresh-456" {
		t.Errorf("expected refresh token refresh-456, got %s", loaded.RefreshToken)
	}
}

func TestTokenStore_LoadNonExistent(t *testing.T) {
	store := NewTokenStore(filepath.Join(t.TempDir(), "nonexistent.json"))

	token, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != nil {
		t.Error("expected nil token for nonexistent file")
	}
}

func TestTokenStore_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(filepath.Join(dir, "sub", "dir", "tokens.json"))

	token := &TokenData{AccessToken: "test", TokenType: "Bearer"}
	if err := store.Save(token); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, "sub", "dir", "tokens.json")); err != nil {
		t.Fatalf("token file not created: %v", err)
	}
}

func TestTokenStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := NewTokenStore(path)

	token := &TokenData{AccessToken: "secret", TokenType: "Bearer"}
	store.Save(token)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected permissions 0600, got %o", perm)
	}
}

func TestTokenStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(filepath.Join(dir, "tokens.json"))

	token := &TokenData{AccessToken: "test", TokenType: "Bearer"}
	store.Save(token)

	if err := store.Delete(); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	loaded, _ := store.Load()
	if loaded != nil {
		t.Error("expected nil after delete")
	}
}

func TestTokenStore_DeleteNonExistent(t *testing.T) {
	store := NewTokenStore(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err := store.Delete(); err != nil {
		t.Fatalf("delete nonexistent should not error: %v", err)
	}
}

func TestTokenData_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		token := &TokenData{ExpiresAt: time.Now().Add(10 * time.Minute)}
		if token.IsExpired() {
			t.Error("token should not be expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		token := &TokenData{ExpiresAt: time.Now().Add(-10 * time.Minute)}
		if !token.IsExpired() {
			t.Error("token should be expired")
		}
	})

	t.Run("expires within buffer", func(t *testing.T) {
		// Within 60-second safety margin
		token := &TokenData{ExpiresAt: time.Now().Add(30 * time.Second)}
		if !token.IsExpired() {
			t.Error("token within buffer should be considered expired")
		}
	})

	t.Run("zero expiry", func(t *testing.T) {
		token := &TokenData{}
		if token.IsExpired() {
			t.Error("zero expiry should not be expired")
		}
	})
}

func TestTokenStore_TildeExpansion(t *testing.T) {
	store := NewTokenStore("~/test-tokens.json")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "test-tokens.json")
	if store.FilePath() != expected {
		t.Errorf("expected %s, got %s", expected, store.FilePath())
	}
}
