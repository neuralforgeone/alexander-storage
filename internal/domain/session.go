// Package domain contains the core business entities for Alexander Storage.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

const (
	// SessionTokenLength is the length of session tokens in bytes (32 bytes = 64 hex chars).
	SessionTokenLength = 32

	// DefaultSessionDuration is the default session validity period.
	DefaultSessionDuration = 24 * time.Hour

	// MaxSessionDuration is the maximum allowed session duration.
	MaxSessionDuration = 7 * 24 * time.Hour
)

// Session represents an authenticated web dashboard session.
// Sessions are stored in the database and validated on each request.
type Session struct {
	// ID is the unique identifier for the session.
	ID uuid.UUID `json:"id"`

	// UserID is the ID of the authenticated user.
	UserID int64 `json:"user_id"`

	// Token is the secure random token used for authentication.
	// This is sent in cookies and used to look up the session.
	Token string `json:"token"`

	// ExpiresAt is when the session expires.
	ExpiresAt time.Time `json:"expires_at"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// IPAddress is the client IP address that created the session.
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the client user agent string.
	UserAgent string `json:"user_agent,omitempty"`
}

// NewSession creates a new session for the given user.
func NewSession(userID int64, ipAddress, userAgent string) (*Session, error) {
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &Session{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     token,
		ExpiresAt: now.Add(DefaultSessionDuration),
		CreatedAt: now,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}, nil
}

// GenerateSessionToken generates a cryptographically secure session token.
func GenerateSessionToken() (string, error) {
	bytes := make([]byte, SessionTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().UTC().After(s.ExpiresAt)
}

// IsValid returns true if the session is still valid.
func (s *Session) IsValid() bool {
	return !s.IsExpired()
}

// Refresh extends the session expiration time.
func (s *Session) Refresh() {
	s.ExpiresAt = time.Now().UTC().Add(DefaultSessionDuration)
}

// TimeUntilExpiry returns the duration until the session expires.
func (s *Session) TimeUntilExpiry() time.Duration {
	return time.Until(s.ExpiresAt)
}

// SessionInfo contains minimal session information for display.
type SessionInfo struct {
	ID        uuid.UUID `json:"id"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IsCurrent bool      `json:"is_current"`
}

// ToInfo converts a session to session info.
func (s *Session) ToInfo(currentToken string) *SessionInfo {
	return &SessionInfo{
		ID:        s.ID,
		IPAddress: s.IPAddress,
		UserAgent: s.UserAgent,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		IsCurrent: s.Token == currentToken,
	}
}
