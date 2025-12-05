package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/repository"
)

// objectRepository implements repository.ObjectRepository.
type objectRepository struct {
	db *DB
}

// NewObjectRepository creates a new PostgreSQL object repository.
func NewObjectRepository(db *DB) repository.ObjectRepository {
	return &objectRepository{db: db}
}

// Create creates a new object.
func (r *objectRepository) Create(ctx context.Context, obj *domain.Object) error {
	query := `
		INSERT INTO objects (bucket_id, key, version_id, is_latest, is_delete_marker, 
			content_hash, size, content_type, etag, storage_class, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`

	err := r.db.Pool.QueryRow(ctx, query,
		obj.BucketID,
		obj.Key,
		obj.VersionID,
		obj.IsLatest,
		obj.IsDeleteMarker,
		obj.ContentHash,
		obj.Size,
		obj.ContentType,
		obj.ETag,
		obj.StorageClass,
		obj.Metadata,
		obj.CreatedAt,
	).Scan(&obj.ID)

	if err != nil {
		return fmt.Errorf("failed to create object: %w", err)
	}

	return nil
}

// GetByID retrieves an object by ID.
func (r *objectRepository) GetByID(ctx context.Context, id int64) (*domain.Object, error) {
	query := `
		SELECT id, bucket_id, key, version_id, is_latest, is_delete_marker, 
			content_hash, size, content_type, etag, storage_class, metadata, created_at, deleted_at
		FROM objects
		WHERE id = $1
	`

	obj := &domain.Object{}
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&obj.ID,
		&obj.BucketID,
		&obj.Key,
		&obj.VersionID,
		&obj.IsLatest,
		&obj.IsDeleteMarker,
		&obj.ContentHash,
		&obj.Size,
		&obj.ContentType,
		&obj.ETag,
		&obj.StorageClass,
		&obj.Metadata,
		&obj.CreatedAt,
		&obj.DeletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get object by ID: %w", err)
	}

	return obj, nil
}

// GetByKey retrieves the latest version of an object by bucket ID and key.
func (r *objectRepository) GetByKey(ctx context.Context, bucketID int64, key string) (*domain.Object, error) {
	query := `
		SELECT id, bucket_id, key, version_id, is_latest, is_delete_marker, 
			content_hash, size, content_type, etag, storage_class, metadata, created_at, deleted_at
		FROM objects
		WHERE bucket_id = $1 AND key = $2 AND is_latest = TRUE AND deleted_at IS NULL
	`

	obj := &domain.Object{}
	err := r.db.Pool.QueryRow(ctx, query, bucketID, key).Scan(
		&obj.ID,
		&obj.BucketID,
		&obj.Key,
		&obj.VersionID,
		&obj.IsLatest,
		&obj.IsDeleteMarker,
		&obj.ContentHash,
		&obj.Size,
		&obj.ContentType,
		&obj.ETag,
		&obj.StorageClass,
		&obj.Metadata,
		&obj.CreatedAt,
		&obj.DeletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get object by key: %w", err)
	}

	return obj, nil
}

// GetByKeyAndVersion retrieves a specific version of an object.
func (r *objectRepository) GetByKeyAndVersion(ctx context.Context, bucketID int64, key string, versionID uuid.UUID) (*domain.Object, error) {
	query := `
		SELECT id, bucket_id, key, version_id, is_latest, is_delete_marker, 
			content_hash, size, content_type, etag, storage_class, metadata, created_at, deleted_at
		FROM objects
		WHERE bucket_id = $1 AND key = $2 AND version_id = $3
	`

	obj := &domain.Object{}
	err := r.db.Pool.QueryRow(ctx, query, bucketID, key, versionID).Scan(
		&obj.ID,
		&obj.BucketID,
		&obj.Key,
		&obj.VersionID,
		&obj.IsLatest,
		&obj.IsDeleteMarker,
		&obj.ContentHash,
		&obj.Size,
		&obj.ContentType,
		&obj.ETag,
		&obj.StorageClass,
		&obj.Metadata,
		&obj.CreatedAt,
		&obj.DeletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get object by version: %w", err)
	}

	return obj, nil
}

// List returns objects in a bucket with pagination and optional prefix filtering.
func (r *objectRepository) List(ctx context.Context, bucketID int64, opts repository.ObjectListOptions) (*repository.ObjectListResult, error) {
	maxKeys := opts.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	query := `
		SELECT key, version_id, is_latest, size, etag, created_at, storage_class
		FROM objects
		WHERE bucket_id = $1 AND is_latest = TRUE AND deleted_at IS NULL
			AND ($2 = '' OR key LIKE $2 || '%')
			AND ($3 = '' OR key > $3)
		ORDER BY key ASC
		LIMIT $4
	`

	rows, err := r.db.Pool.Query(ctx, query, bucketID, opts.Prefix, opts.StartAfter, maxKeys+1)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}
	defer rows.Close()

	var objects []*domain.ObjectInfo
	for rows.Next() {
		obj := &domain.ObjectInfo{}
		var versionID uuid.UUID
		err := rows.Scan(
			&obj.Key,
			&versionID,
			&obj.IsLatest,
			&obj.Size,
			&obj.ETag,
			&obj.LastModified,
			&obj.StorageClass,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan object: %w", err)
		}
		obj.VersionID = versionID.String()
		objects = append(objects, obj)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating objects: %w", err)
	}

	result := &repository.ObjectListResult{
		KeyCount: len(objects),
	}

	if len(objects) > maxKeys {
		result.IsTruncated = true
		result.NextContinuationToken = objects[maxKeys-1].Key
		result.Objects = objects[:maxKeys]
	} else {
		result.Objects = objects
	}

	return result, nil
}

// ListVersions returns all versions of objects in a bucket.
func (r *objectRepository) ListVersions(ctx context.Context, bucketID int64, opts repository.ObjectListOptions) (*repository.ObjectVersionListResult, error) {
	maxKeys := opts.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	query := `
		SELECT key, version_id, is_latest, is_delete_marker, size, etag, created_at, storage_class
		FROM objects
		WHERE bucket_id = $1 AND deleted_at IS NULL
			AND ($2 = '' OR key LIKE $2 || '%')
			AND ($3 = '' OR key > $3)
		ORDER BY key ASC, created_at DESC
		LIMIT $4
	`

	rows, err := r.db.Pool.Query(ctx, query, bucketID, opts.Prefix, opts.StartAfter, maxKeys+1)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}
	defer rows.Close()

	var versions []*domain.ObjectVersion
	var deleteMarkers []*domain.ObjectVersion

	for rows.Next() {
		ver := &domain.ObjectVersion{}
		var versionID uuid.UUID
		var isDeleteMarker bool
		err := rows.Scan(
			&ver.Key,
			&versionID,
			&ver.IsLatest,
			&isDeleteMarker,
			&ver.Size,
			&ver.ETag,
			&ver.LastModified,
			&ver.StorageClass,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan version: %w", err)
		}
		ver.VersionID = versionID.String()
		ver.IsDeleteMarker = isDeleteMarker

		if isDeleteMarker {
			deleteMarkers = append(deleteMarkers, ver)
		} else {
			versions = append(versions, ver)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating versions: %w", err)
	}

	result := &repository.ObjectVersionListResult{
		Versions:      versions,
		DeleteMarkers: deleteMarkers,
	}

	totalCount := len(versions) + len(deleteMarkers)
	if totalCount > maxKeys {
		result.IsTruncated = true
	}

	return result, nil
}

// Update updates an existing object.
func (r *objectRepository) Update(ctx context.Context, obj *domain.Object) error {
	query := `
		UPDATE objects
		SET content_type = $2, metadata = $3, storage_class = $4
		WHERE id = $1
	`

	result, err := r.db.Pool.Exec(ctx, query,
		obj.ID,
		obj.ContentType,
		obj.Metadata,
		obj.StorageClass,
	)

	if err != nil {
		return fmt.Errorf("failed to update object: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrObjectNotFound
	}

	return nil
}

// MarkNotLatest marks an object as not the latest version.
func (r *objectRepository) MarkNotLatest(ctx context.Context, bucketID int64, key string) error {
	query := `
		UPDATE objects
		SET is_latest = FALSE
		WHERE bucket_id = $1 AND key = $2 AND is_latest = TRUE
	`

	_, err := r.db.Pool.Exec(ctx, query, bucketID, key)
	if err != nil {
		return fmt.Errorf("failed to mark as not latest: %w", err)
	}

	return nil
}

// Delete hard-deletes an object by ID.
func (r *objectRepository) Delete(ctx context.Context, id int64) error {
	query := `UPDATE objects SET deleted_at = $2 WHERE id = $1`

	result, err := r.db.Pool.Exec(ctx, query, id, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrObjectNotFound
	}

	return nil
}

// DeleteAllVersions deletes all versions of an object.
func (r *objectRepository) DeleteAllVersions(ctx context.Context, bucketID int64, key string) error {
	query := `UPDATE objects SET deleted_at = $3 WHERE bucket_id = $1 AND key = $2`

	_, err := r.db.Pool.Exec(ctx, query, bucketID, key, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to delete all versions: %w", err)
	}

	return nil
}

// CountByBucket returns the number of objects in a bucket.
func (r *objectRepository) CountByBucket(ctx context.Context, bucketID int64) (int64, error) {
	var count int64
	err := r.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM objects WHERE bucket_id = $1 AND is_latest = TRUE AND deleted_at IS NULL`, bucketID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count objects: %w", err)
	}
	return count, nil
}

// GetContentHashForVersion retrieves the content hash for a specific version.
func (r *objectRepository) GetContentHashForVersion(ctx context.Context, bucketID int64, key string, versionID uuid.UUID) (*string, error) {
	var contentHash *string
	err := r.db.Pool.QueryRow(ctx, `SELECT content_hash FROM objects WHERE bucket_id = $1 AND key = $2 AND version_id = $3`, bucketID, key, versionID).Scan(&contentHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get content hash: %w", err)
	}
	return contentHash, nil
}

// ListExpiredObjects returns latest objects older than cutoff, with optional prefix.
// Used by lifecycle service for expiration processing.
func (r *objectRepository) ListExpiredObjects(ctx context.Context, bucketID int64, prefix string, olderThan time.Time, limit int) ([]*domain.Object, error) {
	query := `
		SELECT id, bucket_id, key, version_id, is_latest, is_delete_marker, 
			content_hash, size, content_type, etag, storage_class, metadata, created_at, deleted_at
		FROM objects
		WHERE bucket_id = $1 
			AND is_latest = TRUE 
			AND is_delete_marker = FALSE
			AND deleted_at IS NULL
			AND created_at < $2
			AND ($3 = '' OR key LIKE $3 || '%')
		ORDER BY created_at ASC
		LIMIT $4
	`

	rows, err := r.db.Pool.Query(ctx, query, bucketID, olderThan, prefix, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list expired objects: %w", err)
	}
	defer rows.Close()

	var objects []*domain.Object
	for rows.Next() {
		obj := &domain.Object{}
		err := rows.Scan(
			&obj.ID,
			&obj.BucketID,
			&obj.Key,
			&obj.VersionID,
			&obj.IsLatest,
			&obj.IsDeleteMarker,
			&obj.ContentHash,
			&obj.Size,
			&obj.ContentType,
			&obj.ETag,
			&obj.StorageClass,
			&obj.Metadata,
			&obj.CreatedAt,
			&obj.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan object: %w", err)
		}
		objects = append(objects, obj)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating objects: %w", err)
	}

	return objects, nil
}

// Ensure objectRepository implements repository.ObjectRepository
var _ repository.ObjectRepository = (*objectRepository)(nil)

// Ensure objectRepository implements repository.ObjectRepository
var _ repository.ObjectRepository = (*objectRepository)(nil)
