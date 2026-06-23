package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/config"
)

func TestDatabaseConfigDSN(t *testing.T) {
	t.Parallel()
	cfg := config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "alexander",
		Password: "secret",
		Database: "alexander",
		SSLMode:  "disable",
	}
	require.Contains(t, cfg.DSN(), "host=localhost")
	require.Contains(t, cfg.DSN(), "dbname=alexander")
}