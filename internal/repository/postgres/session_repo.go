package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// sessionRepository implements repository.SessionRepository.
type sessionRepository struct {
	db *DB
}

// NewSessionRepository creates a new PostgreSQL session repository.
func NewSessionRepository(db *DB) repository.SessionRepository {
	return &sessionRepository{db: db}
}

// Create creates a new session.
func (r *sessionRepository) Create(ctx context.Context, session *domain.Session) error {
	query := `
		INSERT INTO sessions (id, user_id, token, expires_at, created_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Pool.Exec(ctx, query,
		session.ID,
		session.UserID,
		session.Token,
		session.ExpiresAt,
		session.CreatedAt,
		session.IPAddress,
		session.UserAgent,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("session token already exists")
		}
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// GetByToken retrieves a session by its token.
func (r *sessionRepository) GetByToken(ctx context.Context, token string) (*domain.Session, error) {
	query := `
		SELECT id, user_id, token, expires_at, created_at, ip_address, user_agent
		FROM sessions
		WHERE token = $1
	`

	session := &domain.Session{}
	var ipAddress, userAgent *string

	err := r.db.Pool.QueryRow(ctx, query, token).Scan(
		&session.ID,
		&session.UserID,
		&session.Token,
		&session.ExpiresAt,
		&session.CreatedAt,
		&ipAddress,
		&userAgent,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get session by token: %w", err)
	}

	if ipAddress != nil {
		session.IPAddress = *ipAddress
	}
	if userAgent != nil {
		session.UserAgent = *userAgent
	}

	return session, nil
}

// GetByUserID returns all sessions for a user.
func (r *sessionRepository) GetByUserID(ctx context.Context, userID int64) ([]*domain.Session, error) {
	query := `
		SELECT id, user_id, token, expires_at, created_at, ip_address, user_agent
		FROM sessions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions by user ID: %w", err)
	}
	defer rows.Close()

	var sessions []*domain.Session
	for rows.Next() {
		session := &domain.Session{}
		var ipAddress, userAgent *string

		err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.Token,
			&session.ExpiresAt,
			&session.CreatedAt,
			&ipAddress,
			&userAgent,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if ipAddress != nil {
			session.IPAddress = *ipAddress
		}
		if userAgent != nil {
			session.UserAgent = *userAgent
		}

		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// Delete deletes a session by token.
func (r *sessionRepository) Delete(ctx context.Context, token string) error {
	query := `DELETE FROM sessions WHERE token = $1`

	result, err := r.db.Pool.Exec(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	if result.RowsAffected() == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// DeleteByUserID deletes all sessions for a user.
func (r *sessionRepository) DeleteByUserID(ctx context.Context, userID int64) error {
	query := `DELETE FROM sessions WHERE user_id = $1`

	_, err := r.db.Pool.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete sessions by user ID: %w", err)
	}

	return nil
}

// DeleteExpired deletes all expired sessions.
func (r *sessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM sessions WHERE expires_at < $1`

	result, err := r.db.Pool.Exec(ctx, query, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	return result.RowsAffected(), nil
}

// Refresh extends a session's expiration time.
func (r *sessionRepository) Refresh(ctx context.Context, token string, newExpiresAt time.Time) error {
	query := `UPDATE sessions SET expires_at = $2 WHERE token = $1`

	result, err := r.db.Pool.Exec(ctx, query, token, newExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to refresh session: %w", err)
	}

	if result.RowsAffected() == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// CountByUserID returns the number of active sessions for a user.
func (r *sessionRepository) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = $1 AND expires_at > $2`,
		userID, time.Now().UTC(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count sessions: %w", err)
	}
	return count, nil
}

// Ensure sessionRepository implements repository.SessionRepository
var _ repository.SessionRepository = (*sessionRepository)(nil)
