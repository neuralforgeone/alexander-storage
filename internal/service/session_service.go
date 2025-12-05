// Package service provides business logic services for Alexander Storage.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// SessionService handles dashboard session management.
type SessionService struct {
	sessionRepo repository.SessionRepository
	userRepo    repository.UserRepository
	logger      zerolog.Logger

	// Session configuration
	sessionDuration time.Duration
}

// SessionServiceConfig contains configuration for the session service.
type SessionServiceConfig struct {
	SessionDuration time.Duration // Default: 24 hours
}

// DefaultSessionServiceConfig returns the default session service configuration.
func DefaultSessionServiceConfig() SessionServiceConfig {
	return SessionServiceConfig{
		SessionDuration: 24 * time.Hour,
	}
}

// NewSessionService creates a new SessionService.
func NewSessionService(
	sessionRepo repository.SessionRepository,
	userRepo repository.UserRepository,
	logger zerolog.Logger,
	config SessionServiceConfig,
) *SessionService {
	if config.SessionDuration == 0 {
		config.SessionDuration = 24 * time.Hour
	}

	return &SessionService{
		sessionRepo:     sessionRepo,
		userRepo:        userRepo,
		logger:          logger.With().Str("service", "session").Logger(),
		sessionDuration: config.SessionDuration,
	}
}

// LoginInput contains the credentials for login.
type LoginInput struct {
	Username  string
	Password  string
	IPAddress string
	UserAgent string
}

// LoginOutput contains the result of a successful login.
type LoginOutput struct {
	Session *domain.Session
	User    *domain.User
}

// Login authenticates a user and creates a session.
// Only admin users can log in to the dashboard.
func (s *SessionService) Login(ctx context.Context, input LoginInput) (*LoginOutput, error) {
	// Get user by username
	user, err := s.userRepo.GetByUsername(ctx, input.Username)
	if err != nil {
		if err == repository.ErrNotFound {
			s.logger.Debug().Str("username", input.Username).Msg("login failed: user not found")
			return nil, ErrInvalidCredentials
		}
		s.logger.Error().Err(err).Str("username", input.Username).Msg("failed to get user")
		return nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	// Check if user is active
	if !user.IsActive {
		s.logger.Debug().Str("username", input.Username).Msg("login failed: user inactive")
		return nil, ErrUserInactive
	}

	// Check if user is admin
	if !user.IsAdmin {
		s.logger.Debug().Str("username", input.Username).Msg("login failed: user is not admin")
		return nil, ErrNotAdminUser
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password))
	if err != nil {
		s.logger.Debug().Str("username", input.Username).Msg("login failed: invalid password")
		return nil, ErrInvalidCredentials
	}

	// Create session
	session, err := domain.NewSession(user.ID, input.IPAddress, input.UserAgent)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to generate session token")
		return nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		s.logger.Error().Err(err).Int64("user_id", user.ID).Msg("failed to create session")
		return nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	s.logger.Info().
		Int64("user_id", user.ID).
		Str("username", user.Username).
		Str("session_id", session.ID.String()).
		Msg("user logged in")

	return &LoginOutput{
		Session: session,
		User:    user,
	}, nil
}

// ValidateSession validates a session token and returns the associated session and user.
func (s *SessionService) ValidateSession(ctx context.Context, token string) (*domain.Session, *domain.User, error) {
	// Get session by token
	session, err := s.sessionRepo.GetByToken(ctx, token)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, nil, ErrSessionNotFound
		}
		s.logger.Error().Err(err).Msg("failed to get session")
		return nil, nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	// Check if session is expired
	if session.IsExpired() {
		// Clean up expired session
		_ = s.sessionRepo.Delete(ctx, session.Token)
		return nil, nil, ErrSessionExpired
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, session.UserID)
	if err != nil {
		if err == repository.ErrNotFound {
			// User was deleted, clean up session
			_ = s.sessionRepo.Delete(ctx, session.Token)
			return nil, nil, ErrSessionNotFound
		}
		s.logger.Error().Err(err).Int64("user_id", session.UserID).Msg("failed to get user")
		return nil, nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	// Check if user is still active and admin
	if !user.IsActive {
		_ = s.sessionRepo.Delete(ctx, session.Token)
		return nil, nil, ErrUserInactive
	}
	if !user.IsAdmin {
		_ = s.sessionRepo.Delete(ctx, session.Token)
		return nil, nil, ErrNotAdminUser
	}

	return session, user, nil
}

// Logout terminates a session by token.
func (s *SessionService) Logout(ctx context.Context, token string) error {
	session, err := s.sessionRepo.GetByToken(ctx, token)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil // Session doesn't exist, consider it logged out
		}
		s.logger.Error().Err(err).Msg("failed to get session for logout")
		return fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	if err := s.sessionRepo.Delete(ctx, token); err != nil {
		s.logger.Error().Err(err).Str("session_id", session.ID.String()).Msg("failed to delete session")
		return fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	s.logger.Info().
		Str("session_id", session.ID.String()).
		Int64("user_id", session.UserID).
		Msg("user logged out")

	return nil
}

// LogoutUser terminates all sessions for a user.
func (s *SessionService) LogoutUser(ctx context.Context, userID int64) error {
	if err := s.sessionRepo.DeleteByUserID(ctx, userID); err != nil {
		s.logger.Error().Err(err).Int64("user_id", userID).Msg("failed to delete user sessions")
		return fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	s.logger.Info().Int64("user_id", userID).Msg("all user sessions terminated")

	return nil
}

// CleanExpired removes all expired sessions from the database.
// This should be called periodically (e.g., every hour).
func (s *SessionService) CleanExpired(ctx context.Context) (int64, error) {
	deleted, err := s.sessionRepo.DeleteExpired(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to clean expired sessions")
		return 0, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	if deleted > 0 {
		s.logger.Info().Int64("deleted", deleted).Msg("cleaned expired sessions")
	}

	return deleted, nil
}

// GetUserSessions returns all active sessions for a user.
func (s *SessionService) GetUserSessions(ctx context.Context, userID int64) ([]*domain.Session, error) {
	sessions, err := s.sessionRepo.GetByUserID(ctx, userID)
	if err != nil {
		s.logger.Error().Err(err).Int64("user_id", userID).Msg("failed to get user sessions")
		return nil, fmt.Errorf("%w: %v", ErrInternalError, err)
	}

	// Filter out expired sessions
	var activeSessions []*domain.Session
	for _, session := range sessions {
		if !session.IsExpired() {
			activeSessions = append(activeSessions, session)
		}
	}

	return activeSessions, nil
}
