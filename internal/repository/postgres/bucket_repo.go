package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// bucketRepository implements repository.BucketRepository.
type bucketRepository struct {
	db *DB
}

// NewBucketRepository creates a new PostgreSQL bucket repository.
func NewBucketRepository(db *DB) repository.BucketRepository {
	return &bucketRepository{db: db}
}

// Create creates a new bucket.
func (r *bucketRepository) Create(ctx context.Context, bucket *domain.Bucket) error {
	query := `
		INSERT INTO buckets (owner_id, name, region, versioning, acl, object_lock, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err := r.db.Pool.QueryRow(ctx, query,
		bucket.OwnerID,
		bucket.Name,
		bucket.Region,
		bucket.Versioning,
		bucket.ACL,
		bucket.ObjectLock,
		bucket.CreatedAt,
	).Scan(&bucket.ID)

	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", domain.ErrBucketAlreadyExists, bucket.Name)
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	return nil
}

// GetByID retrieves a bucket by ID.
func (r *bucketRepository) GetByID(ctx context.Context, id int64) (*domain.Bucket, error) {
	query := `
		SELECT id, owner_id, name, region, versioning, acl, object_lock, created_at
		FROM buckets
		WHERE id = $1
	`

	bucket := &domain.Bucket{}
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&bucket.ID,
		&bucket.OwnerID,
		&bucket.Name,
		&bucket.Region,
		&bucket.Versioning,
		&bucket.ACL,
		&bucket.ObjectLock,
		&bucket.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBucketNotFound
		}
		return nil, fmt.Errorf("failed to get bucket by ID: %w", err)
	}

	return bucket, nil
}

// GetByName retrieves a bucket by name.
func (r *bucketRepository) GetByName(ctx context.Context, name string) (*domain.Bucket, error) {
	query := `
		SELECT id, owner_id, name, region, versioning, acl, object_lock, created_at
		FROM buckets
		WHERE name = $1
	`

	bucket := &domain.Bucket{}
	err := r.db.Pool.QueryRow(ctx, query, name).Scan(
		&bucket.ID,
		&bucket.OwnerID,
		&bucket.Name,
		&bucket.Region,
		&bucket.Versioning,
		&bucket.ACL,
		&bucket.ObjectLock,
		&bucket.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBucketNotFound
		}
		return nil, fmt.Errorf("failed to get bucket by name: %w", err)
	}

	return bucket, nil
}

// List returns all buckets for a user (or all if userID is 0).
func (r *bucketRepository) List(ctx context.Context, userID int64) ([]*domain.Bucket, error) {
	var query string
	var rows pgx.Rows
	var err error

	if userID > 0 {
		query = `
			SELECT id, owner_id, name, region, versioning, acl, object_lock, created_at
			FROM buckets
			WHERE owner_id = $1
			ORDER BY name ASC
		`
		rows, err = r.db.Pool.Query(ctx, query, userID)
	} else {
		query = `
			SELECT id, owner_id, name, region, versioning, acl, object_lock, created_at
			FROM buckets
			ORDER BY name ASC
		`
		rows, err = r.db.Pool.Query(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list buckets: %w", err)
	}
	defer rows.Close()

	var buckets []*domain.Bucket
	for rows.Next() {
		bucket := &domain.Bucket{}
		err := rows.Scan(
			&bucket.ID,
			&bucket.OwnerID,
			&bucket.Name,
			&bucket.Region,
			&bucket.Versioning,
			&bucket.ACL,
			&bucket.ObjectLock,
			&bucket.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating buckets: %w", err)
	}

	return buckets, nil
}

// Update updates an existing bucket.
func (r *bucketRepository) Update(ctx context.Context, bucket *domain.Bucket) error {
	query := `
		UPDATE buckets
		SET versioning = $2, object_lock = $3
		WHERE id = $1
	`

	result, err := r.db.Pool.Exec(ctx, query,
		bucket.ID,
		bucket.Versioning,
		bucket.ObjectLock,
	)

	if err != nil {
		return fmt.Errorf("failed to update bucket: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBucketNotFound
	}

	return nil
}

// UpdateVersioning updates the versioning status of a bucket.
func (r *bucketRepository) UpdateVersioning(ctx context.Context, id int64, status domain.VersioningStatus) error {
	query := `UPDATE buckets SET versioning = $2 WHERE id = $1`

	result, err := r.db.Pool.Exec(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("failed to update versioning status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBucketNotFound
	}

	return nil
}

// UpdateACL updates the ACL of a bucket.
func (r *bucketRepository) UpdateACL(ctx context.Context, id int64, acl domain.BucketACL) error {
	query := `UPDATE buckets SET acl = $2 WHERE id = $1`

	result, err := r.db.Pool.Exec(ctx, query, id, acl)
	if err != nil {
		return fmt.Errorf("failed to update ACL: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBucketNotFound
	}

	return nil
}

// Delete deletes a bucket by ID.
func (r *bucketRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM buckets WHERE id = $1`

	result, err := r.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBucketNotFound
	}

	return nil
}

// DeleteByName deletes a bucket by name.
func (r *bucketRepository) DeleteByName(ctx context.Context, name string) error {
	query := `DELETE FROM buckets WHERE name = $1`

	result, err := r.db.Pool.Exec(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBucketNotFound
	}

	return nil
}

// ExistsByName checks if a bucket with the given name exists.
func (r *bucketRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM buckets WHERE name = $1)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check bucket existence: %w", err)
	}
	return exists, nil
}

// IsEmpty checks if a bucket contains any objects.
func (r *bucketRepository) IsEmpty(ctx context.Context, id int64) (bool, error) {
	var count int64
	err := r.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE bucket_id = $1 LIMIT 1`, id).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if bucket is empty: %w", err)
	}
	return count == 0, nil
}

// GetACLByName retrieves only the ACL for a bucket by name.
// This is optimized for anonymous access checks.
func (r *bucketRepository) GetACLByName(ctx context.Context, name string) (domain.BucketACL, error) {
	var acl domain.BucketACL
	err := r.db.Pool.QueryRow(ctx, `SELECT acl FROM buckets WHERE name = $1`, name).Scan(&acl)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrBucketNotFound
		}
		return "", fmt.Errorf("failed to get bucket ACL: %w", err)
	}
	return acl, nil
}

// Ensure bucketRepository implements repository.BucketRepository
var _ repository.BucketRepository = (*bucketRepository)(nil)
