package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// lifecycleRepository implements repository.LifecycleRepository.
type lifecycleRepository struct {
	db *DB
}

// NewLifecycleRepository creates a new PostgreSQL lifecycle repository.
func NewLifecycleRepository(db *DB) repository.LifecycleRepository {
	return &lifecycleRepository{db: db}
}

// Create creates a new lifecycle rule.
func (r *lifecycleRepository) Create(ctx context.Context, rule *domain.LifecycleRule) error {
	query := `
		INSERT INTO lifecycle_rules (bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err := r.db.Pool.QueryRow(ctx, query,
		rule.BucketID,
		rule.RuleID,
		rule.Prefix,
		rule.ExpirationDays,
		rule.Status,
		rule.CreatedAt,
		rule.UpdatedAt,
	).Scan(&rule.ID)

	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("lifecycle rule '%s' already exists in bucket", rule.RuleID)
		}
		return fmt.Errorf("failed to create lifecycle rule: %w", err)
	}

	return nil
}

// GetByID retrieves a lifecycle rule by ID.
func (r *lifecycleRepository) GetByID(ctx context.Context, id int64) (*domain.LifecycleRule, error) {
	query := `
		SELECT id, bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at
		FROM lifecycle_rules
		WHERE id = $1
	`

	rule := &domain.LifecycleRule{}
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&rule.ID,
		&rule.BucketID,
		&rule.RuleID,
		&rule.Prefix,
		&rule.ExpirationDays,
		&rule.Status,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get lifecycle rule: %w", err)
	}

	return rule, nil
}

// GetByBucketAndRuleID retrieves a rule by bucket ID and rule ID.
func (r *lifecycleRepository) GetByBucketAndRuleID(ctx context.Context, bucketID int64, ruleID string) (*domain.LifecycleRule, error) {
	query := `
		SELECT id, bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at
		FROM lifecycle_rules
		WHERE bucket_id = $1 AND rule_id = $2
	`

	rule := &domain.LifecycleRule{}
	err := r.db.Pool.QueryRow(ctx, query, bucketID, ruleID).Scan(
		&rule.ID,
		&rule.BucketID,
		&rule.RuleID,
		&rule.Prefix,
		&rule.ExpirationDays,
		&rule.Status,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get lifecycle rule: %w", err)
	}

	return rule, nil
}

// ListByBucket returns all lifecycle rules for a bucket.
func (r *lifecycleRepository) ListByBucket(ctx context.Context, bucketID int64) ([]*domain.LifecycleRule, error) {
	query := `
		SELECT id, bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at
		FROM lifecycle_rules
		WHERE bucket_id = $1
		ORDER BY rule_id ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, bucketID)
	if err != nil {
		return nil, fmt.Errorf("failed to list lifecycle rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.LifecycleRule
	for rows.Next() {
		rule := &domain.LifecycleRule{}
		err := rows.Scan(
			&rule.ID,
			&rule.BucketID,
			&rule.RuleID,
			&rule.Prefix,
			&rule.ExpirationDays,
			&rule.Status,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan lifecycle rule: %w", err)
		}
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lifecycle rules: %w", err)
	}

	return rules, nil
}

// ListEnabledByBucket returns only enabled rules for a bucket.
func (r *lifecycleRepository) ListEnabledByBucket(ctx context.Context, bucketID int64) ([]*domain.LifecycleRule, error) {
	query := `
		SELECT id, bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at
		FROM lifecycle_rules
		WHERE bucket_id = $1 AND status = 'Enabled'
		ORDER BY rule_id ASC
	`

	rows, err := r.db.Pool.Query(ctx, query, bucketID)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled lifecycle rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.LifecycleRule
	for rows.Next() {
		rule := &domain.LifecycleRule{}
		err := rows.Scan(
			&rule.ID,
			&rule.BucketID,
			&rule.RuleID,
			&rule.Prefix,
			&rule.ExpirationDays,
			&rule.Status,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan lifecycle rule: %w", err)
		}
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lifecycle rules: %w", err)
	}

	return rules, nil
}

// Update updates an existing lifecycle rule.
func (r *lifecycleRepository) Update(ctx context.Context, rule *domain.LifecycleRule) error {
	query := `
		UPDATE lifecycle_rules
		SET prefix = $2, expiration_days = $3, status = $4, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.Pool.Exec(ctx, query,
		rule.ID,
		rule.Prefix,
		rule.ExpirationDays,
		rule.Status,
	)

	if err != nil {
		return fmt.Errorf("failed to update lifecycle rule: %w", err)
	}

	if result.RowsAffected() == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// Delete deletes a lifecycle rule by ID.
func (r *lifecycleRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM lifecycle_rules WHERE id = $1`

	result, err := r.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete lifecycle rule: %w", err)
	}

	if result.RowsAffected() == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// DeleteByBucketAndRuleID deletes a rule by bucket ID and rule ID.
func (r *lifecycleRepository) DeleteByBucketAndRuleID(ctx context.Context, bucketID int64, ruleID string) error {
	query := `DELETE FROM lifecycle_rules WHERE bucket_id = $1 AND rule_id = $2`

	result, err := r.db.Pool.Exec(ctx, query, bucketID, ruleID)
	if err != nil {
		return fmt.Errorf("failed to delete lifecycle rule: %w", err)
	}

	if result.RowsAffected() == 0 {
		return repository.ErrNotFound
	}

	return nil
}

// DeleteByBucket deletes all lifecycle rules for a bucket.
func (r *lifecycleRepository) DeleteByBucket(ctx context.Context, bucketID int64) error {
	query := `DELETE FROM lifecycle_rules WHERE bucket_id = $1`

	_, err := r.db.Pool.Exec(ctx, query, bucketID)
	if err != nil {
		return fmt.Errorf("failed to delete lifecycle rules by bucket: %w", err)
	}

	return nil
}

// ListAllEnabled returns all enabled lifecycle rules across all buckets.
func (r *lifecycleRepository) ListAllEnabled(ctx context.Context) ([]*domain.LifecycleRule, error) {
	query := `
		SELECT id, bucket_id, rule_id, prefix, expiration_days, status, created_at, updated_at
		FROM lifecycle_rules
		WHERE status = 'Enabled'
		ORDER BY bucket_id ASC, rule_id ASC
	`

	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list all enabled lifecycle rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.LifecycleRule
	for rows.Next() {
		rule := &domain.LifecycleRule{}
		err := rows.Scan(
			&rule.ID,
			&rule.BucketID,
			&rule.RuleID,
			&rule.Prefix,
			&rule.ExpirationDays,
			&rule.Status,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan lifecycle rule: %w", err)
		}
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lifecycle rules: %w", err)
	}

	return rules, nil
}

// Ensure lifecycleRepository implements repository.LifecycleRepository
var _ repository.LifecycleRepository = (*lifecycleRepository)(nil)
