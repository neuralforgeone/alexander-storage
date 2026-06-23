package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

)

func TestBlobRepository_UpsertAndGet(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	repo := NewBlobRepository(db)
	ctx := context.Background()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	path := "/data/ab/cd/abcdef"

	isNew, err := repo.UpsertWithRefIncrement(ctx, hash, 1024, path)
	require.NoError(t, err)
	require.True(t, isNew)

	got, err := repo.GetByHash(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, int64(1024), got.Size)
	require.Equal(t, int32(1), got.RefCount)
}