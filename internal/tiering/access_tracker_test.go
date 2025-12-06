package tiering

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestMemoryAccessTracker_RecordAccess(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Record access to a new blob
	err := tracker.RecordAccess(ctx, "hash1")
	require.NoError(t, err)

	// Check access info
	info, err := tracker.GetAccessInfo(ctx, "hash1")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "hash1", info.ContentHash)
	require.Equal(t, int64(1), info.AccessCount)
	require.Equal(t, TierHot, info.CurrentTier)

	// Record another access
	err = tracker.RecordAccess(ctx, "hash1")
	require.NoError(t, err)

	info, err = tracker.GetAccessInfo(ctx, "hash1")
	require.NoError(t, err)
	require.Equal(t, int64(2), info.AccessCount)
}

func TestMemoryAccessTracker_GetAccessCount(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Non-existent blob
	count, err := tracker.GetAccessCount(ctx, "nonexistent")
	require.NoError(t, err)
	require.Equal(t, 0, count)

	// Record multiple accesses
	for i := 0; i < 5; i++ {
		err = tracker.RecordAccess(ctx, "hash1")
		require.NoError(t, err)
	}

	count, err = tracker.GetAccessCount(ctx, "hash1")
	require.NoError(t, err)
	require.Equal(t, 5, count)
}

func TestMemoryAccessTracker_GetLastAccess(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Non-existent blob
	lastAccess, err := tracker.GetLastAccess(ctx, "nonexistent")
	require.NoError(t, err)
	require.True(t, lastAccess.IsZero())

	// Record access
	before := time.Now()
	err = tracker.RecordAccess(ctx, "hash1")
	require.NoError(t, err)
	after := time.Now()

	lastAccess, err = tracker.GetLastAccess(ctx, "hash1")
	require.NoError(t, err)
	require.True(t, !lastAccess.Before(before) && !lastAccess.After(after))
}

func TestMemoryAccessTracker_GetAccessStats(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Non-existent blob
	stats, err := tracker.GetAccessStats(ctx, "nonexistent")
	require.NoError(t, err)
	require.Nil(t, stats)

	// Record accesses
	err = tracker.RecordAccess(ctx, "hash1")
	require.NoError(t, err)
	err = tracker.RecordAccess(ctx, "hash1")
	require.NoError(t, err)

	stats, err = tracker.GetAccessStats(ctx, "hash1")
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Equal(t, "hash1", stats.ContentHash)
	require.Equal(t, 2, stats.TotalAccessCount)
	require.False(t, stats.FirstAccessTime.IsZero())
	require.False(t, stats.LastAccessTime.IsZero())
}

func TestMemoryAccessTracker_RegisterBlob(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	now := time.Now()
	info := &BlobAccessInfo{
		ContentHash:    "hash1",
		CurrentTier:    TierWarm,
		Size:           1024,
		CreatedAt:      now.Add(-24 * time.Hour),
		LastAccessedAt: now.Add(-1 * time.Hour),
		AccessCount:    10,
		BucketName:     "test-bucket",
	}

	err := tracker.RegisterBlob(ctx, info)
	require.NoError(t, err)

	// Verify registration
	retrieved, err := tracker.GetAccessInfo(ctx, "hash1")
	require.NoError(t, err)
	require.Equal(t, TierWarm, retrieved.CurrentTier)
	require.Equal(t, int64(1024), retrieved.Size)
	require.Equal(t, int64(10), retrieved.AccessCount)
	require.Equal(t, "test-bucket", retrieved.BucketName)
}

func TestMemoryAccessTracker_UpdateTier(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Register blob
	info := &BlobAccessInfo{
		ContentHash: "hash1",
		CurrentTier: TierHot,
		Size:        1024,
	}
	err := tracker.RegisterBlob(ctx, info)
	require.NoError(t, err)

	// Update tier
	err = tracker.UpdateTier(ctx, "hash1", TierCold)
	require.NoError(t, err)

	// Verify
	retrieved, err := tracker.GetAccessInfo(ctx, "hash1")
	require.NoError(t, err)
	require.Equal(t, TierCold, retrieved.CurrentTier)
}

func TestMemoryAccessTracker_GetBlobsForTiering(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	now := time.Now()

	// Register blobs with different access patterns
	blobs := []*BlobAccessInfo{
		{
			ContentHash:    "hot-recent",
			CurrentTier:    TierHot,
			Size:           2 * 1024 * 1024, // 2MB
			LastAccessedAt: now.Add(-10 * 24 * time.Hour),
		},
		{
			ContentHash:    "hot-old",
			CurrentTier:    TierHot,
			Size:           2 * 1024 * 1024,               // 2MB
			LastAccessedAt: now.Add(-40 * 24 * time.Hour), // 40 days old
		},
		{
			ContentHash:    "warm-old",
			CurrentTier:    TierWarm,
			Size:           2 * 1024 * 1024,                // 2MB
			LastAccessedAt: now.Add(-100 * 24 * time.Hour), // 100 days old
		},
		{
			ContentHash:    "hot-small",
			CurrentTier:    TierHot,
			Size:           512 * 1024, // 512KB - below min size
			LastAccessedAt: now.Add(-40 * 24 * time.Hour),
		},
	}

	for _, blob := range blobs {
		err := tracker.RegisterBlob(ctx, blob)
		require.NoError(t, err)
	}

	policy := PolicyConfig{
		ID:             "test",
		Enabled:        true,
		HotToWarmDays:  30,
		WarmToColdDays: 90,
		MinSize:        1024 * 1024, // 1MB min
	}

	candidates, err := tracker.GetBlobsForTiering(ctx, policy, 10)
	require.NoError(t, err)

	// Should find:
	// - hot-old (hot -> warm, 40 days > 30)
	// - warm-old (warm -> cold, 100 days > 90)
	// NOT: hot-recent (only 10 days), hot-small (below min size)
	require.Len(t, candidates, 2)

	hashes := make(map[string]bool)
	for _, c := range candidates {
		hashes[c.ContentHash] = true
	}
	require.True(t, hashes["hot-old"])
	require.True(t, hashes["warm-old"])
}

func TestMemoryAccessTracker_Cleanup(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	now := time.Now()

	// Register blobs
	blobs := []*BlobAccessInfo{
		{
			ContentHash:    "recent",
			CurrentTier:    TierHot,
			LastAccessedAt: now.Add(-1 * time.Hour),
		},
		{
			ContentHash:    "old",
			CurrentTier:    TierHot,
			LastAccessedAt: now.Add(-48 * time.Hour),
		},
	}

	for _, blob := range blobs {
		err := tracker.RegisterBlob(ctx, blob)
		require.NoError(t, err)
	}

	require.Equal(t, 2, tracker.Count())

	// Cleanup old records
	err := tracker.Cleanup(ctx, 24*time.Hour)
	require.NoError(t, err)

	require.Equal(t, 1, tracker.Count())

	// Verify only recent blob remains
	_, err = tracker.GetAccessInfo(ctx, "recent")
	require.NoError(t, err)

	_, err = tracker.GetAccessInfo(ctx, "old")
	require.Error(t, err)
}

func TestMemoryAccessTracker_GetAllBlobs(t *testing.T) {
	tracker := NewMemoryAccessTracker(zerolog.Nop())
	ctx := context.Background()

	// Empty
	blobs, err := tracker.GetAllBlobs(ctx)
	require.NoError(t, err)
	require.Empty(t, blobs)

	// Register blobs
	for i := 0; i < 3; i++ {
		err = tracker.RecordAccess(ctx, "hash"+string(rune('0'+i)))
		require.NoError(t, err)
	}

	blobs, err = tracker.GetAllBlobs(ctx)
	require.NoError(t, err)
	require.Len(t, blobs, 3)
}

func TestPolicyConfig_Default(t *testing.T) {
	policy := DefaultPolicyConfig()

	require.Equal(t, "default", policy.ID)
	require.True(t, policy.Enabled)
	require.Equal(t, 30, policy.HotToWarmDays)
	require.Equal(t, 90, policy.WarmToColdDays)
	require.Equal(t, int64(1024*1024), policy.MinSize)
}

func TestControllerConfig_Default(t *testing.T) {
	config := DefaultControllerConfig()

	require.Equal(t, time.Hour, config.ScanInterval)
	require.Equal(t, 5, config.MaxConcurrentMigrations)
	require.Equal(t, 100, config.MigrationBatchSize)
	require.Equal(t, 5*time.Minute, config.RetryDelay)
	require.Equal(t, 3, config.MaxRetries)
}
