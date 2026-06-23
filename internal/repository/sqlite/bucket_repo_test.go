package sqlite

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/domain"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	db, err := NewDB(ctx, DefaultConfig(":memory:"), zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, db.Migrate(ctx))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestBucketRepository_CreateAndGet(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	repo := NewBucketRepository(db)
	ctx := context.Background()

	bucket := domain.NewBucket(1, "test-bucket")
	require.NoError(t, repo.Create(ctx, bucket))
	require.NotZero(t, bucket.ID)

	got, err := repo.GetByName(ctx, "test-bucket")
	require.NoError(t, err)
	require.Equal(t, bucket.Name, got.Name)
	require.Equal(t, bucket.OwnerID, got.OwnerID)
}

func TestBucketRepository_ListByOwner(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	repo := NewBucketRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, domain.NewBucket(42, "bucket-a")))
	require.NoError(t, repo.Create(ctx, domain.NewBucket(42, "bucket-b")))

	buckets, err := repo.List(ctx, 42)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
}

func TestBucketRepository_Delete(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	repo := NewBucketRepository(db)
	ctx := context.Background()

	bucket := domain.NewBucket(1, "delete-me")
	require.NoError(t, repo.Create(ctx, bucket))
	require.NoError(t, repo.Delete(ctx, bucket.ID))

	_, err := repo.GetByName(ctx, "delete-me")
	require.Error(t, err)
}