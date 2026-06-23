package main

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/config"
	"github.com/prn-tf/alexander-storage/internal/domain"
)

func TestInitRepositories_SQLite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repos, db, err := initRepositories(ctx, &config.Config{
		Database: config.DatabaseConfig{
			Driver: "sqlite",
			Path:   ":memory:",
		},
	}, zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, repos)
	require.NotNil(t, db)
	defer db.Close()

	bucket := domain.NewBucket(1, "server-bootstrap-test")
	require.NoError(t, repos.Bucket.Create(ctx, bucket))

	got, err := repos.Bucket.GetByName(ctx, "server-bootstrap-test")
	require.NoError(t, err)
	require.Equal(t, "server-bootstrap-test", got.Name)
}