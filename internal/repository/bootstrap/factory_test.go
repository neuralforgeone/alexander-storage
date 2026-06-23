package bootstrap

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/config"
	"github.com/prn-tf/alexander-storage/internal/domain"
)

func TestCreateSQLite_InMemory(t *testing.T) {
	t.Parallel()

	result, err := CreateSQLite(context.Background(), config.DatabaseConfig{Path: ":memory:"}, zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, result.Repos)
	defer result.Database.Close()

	ctx := context.Background()
	bucket := domain.NewBucket(1, "bootstrap-bucket")
	require.NoError(t, result.Repos.Bucket.Create(ctx, bucket))

	got, err := result.Repos.Bucket.GetByName(ctx, "bootstrap-bucket")
	require.NoError(t, err)
	require.Equal(t, "bootstrap-bucket", got.Name)
}