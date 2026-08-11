// Package bootstrap wires repository implementations without import cycles.
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/config"
	"github.com/prn-tf/alexander-storage/internal/repository"
	"github.com/prn-tf/alexander-storage/internal/repository/postgres"
	"github.com/prn-tf/alexander-storage/internal/repository/sqlite"
)

// CreatePostgres creates PostgreSQL repositories.
func CreatePostgres(ctx context.Context, cfg config.DatabaseConfig, logger zerolog.Logger) (*repository.CreateRepositoriesResult, error) {
	pgDB, err := postgres.NewDB(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}

	return &repository.CreateRepositoriesResult{
		Database: pgDB,
		Repos: &repository.Repositories{
			User:      postgres.NewUserRepository(pgDB),
			AccessKey: postgres.NewAccessKeyRepository(pgDB),
			Bucket:    postgres.NewBucketRepository(pgDB),
			Object:    postgres.NewObjectRepository(pgDB),
			Blob:      postgres.NewBlobRepository(pgDB),
			Multipart: postgres.NewMultipartRepository(pgDB),
			Session:   postgres.NewSessionRepository(pgDB),
			Lifecycle: postgres.NewLifecycleRepository(pgDB),
		},
	}, nil
}

// CreateSQLite creates SQLite repositories.
func CreateSQLite(ctx context.Context, cfg config.DatabaseConfig, logger zerolog.Logger) (*repository.CreateRepositoriesResult, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
	} else if cfg.Path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
			return nil, fmt.Errorf("sqlite mkdir: %w", err)
		}
	}

	sqliteCfg := sqlite.DefaultConfig(cfg.Path)
	if cfg.MaxOpenConns > 0 {
		sqliteCfg.MaxOpenConns = cfg.MaxOpenConns
	}
	if cfg.MaxIdleConns > 0 {
		sqliteCfg.MaxIdleConns = cfg.MaxIdleConns
	}
	if cfg.ConnMaxLifetime > 0 {
		sqliteCfg.ConnMaxLifetime = cfg.ConnMaxLifetime
	}
	if cfg.JournalMode != "" {
		sqliteCfg.JournalMode = cfg.JournalMode
	}
	if cfg.BusyTimeout > 0 {
		sqliteCfg.BusyTimeout = cfg.BusyTimeout
	}
	if cfg.CacheSize != 0 {
		sqliteCfg.CacheSize = cfg.CacheSize
	}
	if cfg.SynchronousMode != "" {
		sqliteCfg.SynchronousMode = cfg.SynchronousMode
	}

	sqliteDB, err := sqlite.NewDB(ctx, sqliteCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}

	if err := sqliteDB.Migrate(ctx); err != nil {
		sqliteDB.Close()
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}

	return &repository.CreateRepositoriesResult{
		Database: sqliteDB,
		Repos: &repository.Repositories{
			User:      sqlite.NewUserRepository(sqliteDB),
			AccessKey: sqlite.NewAccessKeyRepository(sqliteDB),
			Bucket:    sqlite.NewBucketRepository(sqliteDB),
			Object:    sqlite.NewObjectRepository(sqliteDB),
			Blob:      sqlite.NewBlobRepository(sqliteDB),
			Multipart: sqlite.NewMultipartRepository(sqliteDB),
			Session:   sqlite.NewSessionRepository(sqliteDB),
			Lifecycle: sqlite.NewLifecycleRepository(sqliteDB),
		},
	}, nil
}