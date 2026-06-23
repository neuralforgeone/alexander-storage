package repository

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/config"
)

func TestCreatePostgresFactoryNotImplemented(t *testing.T) {
	t.Parallel()
	_, err := CreatePostgres(context.Background(), config.DatabaseConfig{}, zerolog.Nop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not implemented")
}

func TestCreateSQLiteFactoryNotImplemented(t *testing.T) {
	t.Parallel()
	_, err := CreateSQLite(context.Background(), config.DatabaseConfig{}, zerolog.Nop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not implemented")
}