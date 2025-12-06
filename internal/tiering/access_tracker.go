// Package tiering provides automatic data tiering for Alexander Storage.
package tiering

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// MemoryAccessTracker is an in-memory implementation of AccessTracker and BlobAccessTracker.
// It's suitable for single-node deployments or testing. For production multi-node
// deployments, use a Redis-backed implementation.
type MemoryAccessTracker struct {
	mu     sync.RWMutex
	blobs  map[string]*BlobAccessInfo
	stats  map[string]*AccessStats
	logger zerolog.Logger
}

// NewMemoryAccessTracker creates a new in-memory access tracker.
func NewMemoryAccessTracker(logger zerolog.Logger) *MemoryAccessTracker {
	return &MemoryAccessTracker{
		blobs:  make(map[string]*BlobAccessInfo),
		stats:  make(map[string]*AccessStats),
		logger: logger.With().Str("component", "memory-access-tracker").Logger(),
	}
}

// RecordAccess records an access to a blob.
func (t *MemoryAccessTracker) RecordAccess(ctx context.Context, contentHash string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	// Update blob access info
	info, exists := t.blobs[contentHash]
	if !exists {
		info = &BlobAccessInfo{
			ContentHash:    contentHash,
			CurrentTier:    TierHot,
			CreatedAt:      now,
			LastAccessedAt: now,
			AccessCount:    0,
		}
		t.blobs[contentHash] = info
	}

	info.LastAccessedAt = now
	info.AccessCount++

	// Update stats
	stats, exists := t.stats[contentHash]
	if !exists {
		stats = &AccessStats{
			ContentHash:     contentHash,
			FirstAccessTime: now,
		}
		t.stats[contentHash] = stats
	}

	stats.TotalAccessCount++
	stats.LastAccessTime = now

	return nil
}

// GetAccessInfo returns access information for a blob.
func (t *MemoryAccessTracker) GetAccessInfo(ctx context.Context, contentHash string) (*BlobAccessInfo, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	info, exists := t.blobs[contentHash]
	if !exists {
		return nil, ErrNoTargetNode // Use a more specific error in production
	}

	// Return a copy
	infoCopy := *info
	return &infoCopy, nil
}

// GetBlobsForTiering returns blobs that may need tiering based on access patterns.
func (t *MemoryAccessTracker) GetBlobsForTiering(ctx context.Context, policy PolicyConfig, limit int) ([]*BlobAccessInfo, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var candidates []*BlobAccessInfo
	now := time.Now()

	for _, info := range t.blobs {
		// Check size constraints
		if policy.MinSize > 0 && info.Size < policy.MinSize {
			continue
		}
		if policy.MaxSize > 0 && info.Size > policy.MaxSize {
			continue
		}

		// Calculate days since last access
		daysSinceAccess := int(now.Sub(info.LastAccessedAt).Hours() / 24)

		// Check if eligible for tiering
		eligible := false
		switch info.CurrentTier {
		case TierHot:
			if daysSinceAccess >= policy.HotToWarmDays {
				eligible = true
			}
		case TierWarm:
			if daysSinceAccess >= policy.WarmToColdDays {
				eligible = true
			}
		}

		if eligible {
			infoCopy := *info
			candidates = append(candidates, &infoCopy)
			if limit > 0 && len(candidates) >= limit {
				break
			}
		}
	}

	return candidates, nil
}

// GetAccessCount returns the access count for a blob.
func (t *MemoryAccessTracker) GetAccessCount(ctx context.Context, contentHash string) (int, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats, exists := t.stats[contentHash]
	if !exists {
		return 0, nil
	}

	return stats.TotalAccessCount, nil
}

// GetLastAccess returns the last access time for a blob.
func (t *MemoryAccessTracker) GetLastAccess(ctx context.Context, contentHash string) (time.Time, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats, exists := t.stats[contentHash]
	if !exists {
		return time.Time{}, nil
	}

	return stats.LastAccessTime, nil
}

// GetAccessStats returns full access statistics for a blob.
func (t *MemoryAccessTracker) GetAccessStats(ctx context.Context, contentHash string) (*AccessStats, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats, exists := t.stats[contentHash]
	if !exists {
		return nil, nil
	}

	// Return a copy with calculated recent access counts
	// Note: For proper implementation, you'd track individual access timestamps
	// This simplified version just returns total count for all periods
	statsCopy := *stats
	statsCopy.AccessesLast24h = stats.TotalAccessCount // Simplified
	statsCopy.AccessesLast7d = stats.TotalAccessCount
	statsCopy.AccessesLast30d = stats.TotalAccessCount

	return &statsCopy, nil
}

// Cleanup removes old access records.
func (t *MemoryAccessTracker) Cleanup(ctx context.Context, olderThan time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)

	for hash, info := range t.blobs {
		if info.LastAccessedAt.Before(cutoff) {
			delete(t.blobs, hash)
			delete(t.stats, hash)
		}
	}

	return nil
}

// RegisterBlob registers a new blob with initial access info.
func (t *MemoryAccessTracker) RegisterBlob(ctx context.Context, info *BlobAccessInfo) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	infoCopy := *info

	if infoCopy.CreatedAt.IsZero() {
		infoCopy.CreatedAt = now
	}
	if infoCopy.LastAccessedAt.IsZero() {
		infoCopy.LastAccessedAt = now
	}
	if infoCopy.CurrentTier == "" {
		infoCopy.CurrentTier = TierHot
	}

	t.blobs[info.ContentHash] = &infoCopy

	// Initialize stats
	t.stats[info.ContentHash] = &AccessStats{
		ContentHash:      info.ContentHash,
		TotalAccessCount: int(info.AccessCount),
		LastAccessTime:   info.LastAccessedAt,
		FirstAccessTime:  info.CreatedAt,
	}

	return nil
}

// UpdateTier updates the current tier of a blob.
func (t *MemoryAccessTracker) UpdateTier(ctx context.Context, contentHash string, tier Tier) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, exists := t.blobs[contentHash]
	if !exists {
		return ErrNoTargetNode
	}

	info.CurrentTier = tier
	return nil
}

// GetAllBlobs returns all tracked blobs.
func (t *MemoryAccessTracker) GetAllBlobs(ctx context.Context) ([]*BlobAccessInfo, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*BlobAccessInfo, 0, len(t.blobs))
	for _, info := range t.blobs {
		infoCopy := *info
		result = append(result, &infoCopy)
	}

	return result, nil
}

// Count returns the number of tracked blobs.
func (t *MemoryAccessTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.blobs)
}

// Verify interface compliance
var _ AccessTracker = (*MemoryAccessTracker)(nil)
var _ BlobAccessTracker = (*MemoryAccessTracker)(nil)
