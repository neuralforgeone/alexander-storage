package storage

import "context"

// BlobEntry describes a stored blob for listing operations.
type BlobEntry struct {
	ContentHash string
	Size        int64
}

// BlobLister lists blobs stored in a backend.
type BlobLister interface {
	ListBlobs(ctx context.Context, prefix string, limit int) ([]BlobEntry, error)
}

// StatsSnapshot contains aggregate storage statistics.
type StatsSnapshot struct {
	TotalBytes int64
	UsedBytes  int64
	FreeBytes  int64
	BlobCount  int64
}

// StatsProvider reports storage utilization.
type StatsProvider interface {
	ComputeStats(ctx context.Context) (*StatsSnapshot, error)
}