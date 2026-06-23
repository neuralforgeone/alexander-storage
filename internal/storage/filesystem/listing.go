package filesystem

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/prn-tf/alexander-storage/internal/storage"
)

// ListBlobs walks the data directory and returns blob metadata.
func (s *Storage) ListBlobs(ctx context.Context, prefix string, limit int) ([]storage.BlobEntry, error) {
	if limit == 0 {
		limit = 10000
	}

	var entries []storage.BlobEntry
	err := filepath.WalkDir(s.dataDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}

		hash := filepath.Base(path)
		if len(hash) != 64 {
			return nil
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil
		}
		if prefix != "" && !strings.HasPrefix(hash, prefix) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		entries = append(entries, storage.BlobEntry{
			ContentHash: hash,
			Size:        info.Size(),
		})
		if len(entries) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// ComputeStats aggregates blob counts and sizes from the data directory.
func (s *Storage) ComputeStats(ctx context.Context) (*storage.StatsSnapshot, error) {
	entries, err := s.listAllBlobs(ctx)
	if err != nil {
		return nil, err
	}

	var used int64
	for _, e := range entries {
		used += e.Size
	}

	freeBytes := int64(0)
	if stat, err := diskFreeBytes(s.dataDir); err == nil {
		freeBytes = stat
	}

	return &storage.StatsSnapshot{
		TotalBytes: used + freeBytes,
		UsedBytes:  used,
		FreeBytes:  freeBytes,
		BlobCount:  int64(len(entries)),
	}, nil
}

func (s *Storage) listAllBlobs(ctx context.Context) ([]storage.BlobEntry, error) {
	var entries []storage.BlobEntry
	err := filepath.WalkDir(s.dataDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		hash := filepath.Base(path)
		if len(hash) != 64 {
			return nil
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, storage.BlobEntry{ContentHash: hash, Size: info.Size()})
		return nil
	})
	return entries, err
}

func diskFreeBytes(path string) (int64, error) {
	// Best-effort free space; returns 0 when unavailable.
	var free uint64
	err := getDiskFreeSpace(path, &free)
	if err != nil {
		return 0, err
	}
	return int64(free), nil
}