package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// sessionRepository implements repository.SessionRepository for SQLite.
type sessionRepository struct {
	db *DB
}

// NewSessionRepository creates a new SQLite session repository.
func NewSessionRepository(db *DB) repository.SessionRepository {
	return &sessionRepository{db: db}
}

// Create creates a new session.
func (r *sessionRepository) Create(ctx context.Context, session *domain.Session) error {
	query := `
		INSERT INTO sessions (id, user_id, token, expires_at, created_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(ctx, query,
		session.ID.String(),
		session.UserID,
		session.Token,
		session.ExpiresAt.Format(time.RFC3339),
		session.CreatedAt.Format(time.RFC3339),
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
		WHERE token = ?
	`

	session := &domain.Session{}
	var id, expiresAt, createdAt string
	var ipAddress, userAgent *string

	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&id,
		&session.UserID,
		&session.Token,
		&expiresAt,
		&createdAt,
		&ipAddress,
		&userAgent,
	)

	if err != nil {
		if isNoRows(err) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get session by token: %w", err)
	}

	session.ID = parseUUID(id)
	session.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

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
		WHERE user_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions by user ID: %w", err)
	}
	defer rows.Close()

	var sessions []*domain.Session
	for rows.Next() {
		session := &domain.Session{}
		var id, expiresAt, createdAt string
		var ipAddress, userAgent *string

		err := rows.Scan(
			&id,
			&session.UserID,
			&session.Token,
			&expiresAt,
			&createdAt,
			&ipAddress,
			&userAgent,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		session.ID = parseUUID(id)
		session.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

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
	query := `DELETE FROM sessions WHERE token = ?`

	result, err := r.db.ExecContext(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// DeleteByUserID deletes all sessions for a user.
func (r *sessionRepository) DeleteByUserID(ctx context.Context, userID int64) error {
	query := `DELETE FROM sessions WHERE user_id = ?`

	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete sessions by user ID: %w", err)
	}

	return nil
}

// DeleteExpired deletes all expired sessions.
func (r *sessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM sessions WHERE expires_at < ?`

	result, err := r.db.ExecContext(ctx, query, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

// Refresh extends a session's expiration time.
func (r *sessionRepository) Refresh(ctx context.Context, token string, newExpiresAt time.Time) error {
	query := `UPDATE sessions SET expires_at = ? WHERE token = ?`

	result, err := r.db.ExecContext(ctx, query, newExpiresAt.Format(time.RFC3339), token)
	if err != nil {
		return fmt.Errorf("failed to refresh session: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// CountByUserID returns the number of active sessions for a user.
func (r *sessionRepository) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND expires_at > ?`,
		userID, time.Now().UTC().Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count sessions: %w", err)
	}
	return count, nil
}

// parseUUID parses a UUID string into a uuid.UUID.
// Returns uuid.Nil on error.
func parseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// Ensure sessionRepository implements repository.SessionRepository.
var _ repository.SessionRepository = (*sessionRepository)(nil)
