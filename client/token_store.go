package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenData holds OAuth tokens and metadata for persistence.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       string    `json:"scopes,omitempty"`
}

// IsExpired returns true if the access token has expired or will expire within the buffer period.
func (t *TokenData) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false // No expiry info, assume valid
	}
	// Consider expired 60 seconds before actual expiry for safety margin
	return time.Now().After(t.ExpiresAt.Add(-60 * time.Second))
}

// TokenStore handles loading and saving OAuth tokens to a file.
type TokenStore struct {
	filePath string
}

// NewTokenStore creates a token store that persists to the given file path.
// The path can use ~ for home directory.
func NewTokenStore(filePath string) *TokenStore {
	// Expand ~ to home directory
	if len(filePath) > 0 && filePath[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[1:])
		}
	}
	return &TokenStore{filePath: filePath}
}

// Load reads tokens from the file. Returns nil if the file doesn't exist.
func (s *TokenStore) Load() (*TokenData, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token TokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// Save writes tokens to the file, creating parent directories as needed.
func (s *TokenStore) Save(token *TokenData) error {
	// Ensure parent directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	// Write with restrictive permissions (owner read/write only)
	if err := os.WriteFile(s.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// Delete removes the token file.
func (s *TokenStore) Delete() error {
	err := os.Remove(s.filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}
	return nil
}

// FilePath returns the resolved file path.
func (s *TokenStore) FilePath() string {
	return s.filePath
}
