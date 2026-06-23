package filesystem

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestListBlobsAndComputeStats(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStorage(Config{
		DataDir: filepath.Join(tmpDir, "data"),
		TempDir: filepath.Join(tmpDir, "temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	ctx := context.Background()
	payload := "stats payload"
	_, err = store.Store(ctx, strings.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)

	entries, err := store.ListBlobs(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, int64(len(payload)), entries[0].Size)

	stats, err := store.ComputeStats(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.BlobCount)
	require.Equal(t, int64(len(payload)), stats.UsedBytes)
}