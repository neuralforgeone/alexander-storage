// Package middleware provides HTTP middleware for Alexander Storage.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
)

// CSRFConfig contains configuration for the CSRF middleware.
type CSRFConfig struct {
	// TokenLength is the length of the CSRF token in bytes (default: 32).
	TokenLength int

	// CookieName is the name of the CSRF cookie (default: "csrf_token").
	CookieName string

	// HeaderName is the name of the CSRF header for AJAX requests (default: "X-CSRF-Token").
	HeaderName string

	// FormField is the name of the CSRF form field (default: "csrf_token").
	FormField string

	// CookiePath is the path for the CSRF cookie (default: "/").
	CookiePath string

	// CookieMaxAge is the max age for the CSRF cookie in seconds (default: 86400 = 24h).
	CookieMaxAge int

	// Secure sets the Secure flag on the cookie.
	Secure bool

	// SameSite sets the SameSite attribute on the cookie.
	SameSite http.SameSite

	// ExemptPaths are paths that don't require CSRF validation (e.g., login).
	ExemptPaths []string

	// ExemptMethods are HTTP methods that don't require CSRF validation.
	ExemptMethods []string
}

// DefaultCSRFConfig returns the default CSRF configuration.
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		TokenLength:   32,
		CookieName:    "csrf_token",
		HeaderName:    "X-CSRF-Token",
		FormField:     "csrf_token",
		CookiePath:    "/dashboard",
		CookieMaxAge:  86400,
		Secure:        false,
		SameSite:      http.SameSiteStrictMode,
		ExemptPaths:   []string{"/dashboard/login"},
		ExemptMethods: []string{"GET", "HEAD", "OPTIONS"},
	}
}

// CSRFMiddleware provides CSRF protection for forms.
type CSRFMiddleware struct {
	config CSRFConfig
}

// NewCSRFMiddleware creates a new CSRF middleware.
func NewCSRFMiddleware(config CSRFConfig) *CSRFMiddleware {
	if config.TokenLength == 0 {
		config.TokenLength = 32
	}
	if config.CookieName == "" {
		config.CookieName = "csrf_token"
	}
	if config.HeaderName == "" {
		config.HeaderName = "X-CSRF-Token"
	}
	if config.FormField == "" {
		config.FormField = "csrf_token"
	}
	if config.CookiePath == "" {
		config.CookiePath = "/dashboard"
	}
	if config.CookieMaxAge == 0 {
		config.CookieMaxAge = 86400
	}
	if config.SameSite == 0 {
		config.SameSite = http.SameSiteStrictMode
	}
	if config.ExemptMethods == nil {
		config.ExemptMethods = []string{"GET", "HEAD", "OPTIONS"}
	}

	return &CSRFMiddleware{
		config: config,
	}
}

// ctxKey is the context key type for CSRF token.
type csrfCtxKey struct{}

// TokenFromContext retrieves the CSRF token from the context.
func TokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(csrfCtxKey{}).(string); ok {
		return token
	}
	return ""
}

// Handler returns the CSRF middleware handler.
func (m *CSRFMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if method is exempt
		if m.isExemptMethod(r.Method) {
			// For GET requests, ensure token is set and pass it to context
			token := m.getOrCreateToken(w, r)
			ctx := context.WithValue(r.Context(), csrfCtxKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Check if path is exempt
		if m.isExemptPath(r.URL.Path) {
			token := m.getOrCreateToken(w, r)
			ctx := context.WithValue(r.Context(), csrfCtxKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Validate CSRF token for mutating methods
		if !m.validateToken(r) {
			http.Error(w, "CSRF token validation failed", http.StatusForbidden)
			return
		}

		// Token is valid, continue
		token := m.getOrCreateToken(w, r)
		ctx := context.WithValue(r.Context(), csrfCtxKey{}, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getOrCreateToken retrieves existing CSRF token or creates a new one.
func (m *CSRFMiddleware) getOrCreateToken(w http.ResponseWriter, r *http.Request) string {
	// Try to get existing token from cookie
	cookie, err := r.Cookie(m.config.CookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Generate new token
	token, err := m.generateToken()
	if err != nil {
		return ""
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    token,
		Path:     m.config.CookiePath,
		MaxAge:   m.config.CookieMaxAge,
		HttpOnly: false, // Must be readable by JavaScript for HTMX
		Secure:   m.config.Secure || r.TLS != nil,
		SameSite: m.config.SameSite,
	})

	return token
}

// validateToken validates the CSRF token from request.
func (m *CSRFMiddleware) validateToken(r *http.Request) bool {
	// Get token from cookie
	cookie, err := r.Cookie(m.config.CookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	cookieToken := cookie.Value

	// Get token from request (header or form)
	requestToken := r.Header.Get(m.config.HeaderName)
	if requestToken == "" {
		// Try form field
		if err := r.ParseForm(); err == nil {
			requestToken = r.FormValue(m.config.FormField)
		}
	}

	if requestToken == "" {
		return false
	}

	// Constant-time comparison
	return subtle.ConstantTimeCompare([]byte(cookieToken), []byte(requestToken)) == 1
}

// generateToken generates a new CSRF token.
func (m *CSRFMiddleware) generateToken() (string, error) {
	b := make([]byte, m.config.TokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// isExemptMethod checks if the HTTP method is exempt from CSRF validation.
func (m *CSRFMiddleware) isExemptMethod(method string) bool {
	for _, exempt := range m.config.ExemptMethods {
		if method == exempt {
			return true
		}
	}
	return false
}

// isExemptPath checks if the path is exempt from CSRF validation.
func (m *CSRFMiddleware) isExemptPath(path string) bool {
	for _, exempt := range m.config.ExemptPaths {
		if path == exempt {
			return true
		}
	}
	return false
}

// ClearToken clears the CSRF token (call on logout).
func (m *CSRFMiddleware) ClearToken(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.config.CookieName,
		Value:    "",
		Path:     m.config.CookiePath,
		MaxAge:   -1,
		HttpOnly: false,
		SameSite: m.config.SameSite,
	})
}

// TokenRefresher is a middleware that refreshes the CSRF token periodically.
// This can be used to rotate tokens for additional security.
type TokenRefresher struct {
	csrf         *CSRFMiddleware
	refreshAfter time.Duration
}

// NewTokenRefresher creates a new token refresher.
func NewTokenRefresher(csrf *CSRFMiddleware, refreshAfter time.Duration) *TokenRefresher {
	return &TokenRefresher{
		csrf:         csrf,
		refreshAfter: refreshAfter,
	}
}
