package s3

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/prn-tf/alexander-storage/internal/storage"
)

// ListBlobs lists objects under the blobs prefix.
func (s *Storage) ListBlobs(ctx context.Context, prefix string, limit int) ([]storage.BlobEntry, error) {
	if limit <= 0 {
		limit = 100000
	}

	listPrefix := s.prefix
	if prefix != "" {
		listPrefix += prefix
	}

	var entries []storage.BlobEntry
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(listPrefix),
		MaxKeys: aws.Int32(int32(limit)),
	})

	for paginator.HasMorePages() && len(entries) < limit {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			hash := objectHashFromKey(s.prefix, key)
			if hash == "" {
				continue
			}
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			entries = append(entries, storage.BlobEntry{ContentHash: hash, Size: size})
			if len(entries) >= limit {
				break
			}
		}
	}
	return entries, nil
}

// ComputeStats aggregates object counts and sizes in the bucket prefix.
func (s *Storage) ComputeStats(ctx context.Context) (*storage.StatsSnapshot, error) {
	entries, err := s.ListBlobs(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	var used int64
	for _, e := range entries {
		used += e.Size
	}
	return &storage.StatsSnapshot{
		TotalBytes: used,
		UsedBytes:  used,
		FreeBytes:  0,
		BlobCount:  int64(len(entries)),
	}, nil
}

func objectHashFromKey(prefix, key string) string {
	key = strings.TrimPrefix(key, prefix)
	parts := strings.Split(key, "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-1]
}