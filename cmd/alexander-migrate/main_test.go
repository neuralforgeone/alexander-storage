package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNextMigrationVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIGRATIONS_PATH", dir)

	next, err := nextMigrationVersion()
	require.NoError(t, err)
	require.Equal(t, 1, next)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "000001_init.up.sql"), []byte("-- init"), 0644))
	next, err = nextMigrationVersion()
	require.NoError(t, err)
	require.Equal(t, 2, next)
}

func TestCreateMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIGRATIONS_PATH", dir)

	require.NoError(t, createMigration("add_indexes"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	names := []string{entries[0].Name(), entries[1].Name()}
	require.True(t, strings.HasSuffix(names[0], ".sql"))
	require.True(t, strings.HasSuffix(names[1], ".sql"))
}