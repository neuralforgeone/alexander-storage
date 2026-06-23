// Package s3 provides an S3-compatible blob storage backend.
package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/storage"
)

// Config holds S3 backend settings.
type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Prefix          string
}

// Storage implements storage.Backend using S3.
type Storage struct {
	client *s3.Client
	bucket string
	prefix string
	logger zerolog.Logger
}

// NewStorage creates a new S3 storage backend.
func NewStorage(ctx context.Context, cfg Config, logger zerolog.Logger) (*Storage, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	})

	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}

	return &Storage{
		client: client,
		bucket: cfg.Bucket,
		prefix: prefix,
		logger: logger.With().Str("component", "s3-storage").Logger(),
	}, nil
}

// ObjectKey returns the S3 object key for a content hash.
func ObjectKey(prefix, contentHash string) string {
	if len(contentHash) < 4 {
		return prefix + contentHash
	}
	return fmt.Sprintf("%s%s/%s/%s", prefix, contentHash[:2], contentHash[2:4], contentHash)
}

func (s *Storage) objectKey(contentHash string) string {
	return ObjectKey(s.prefix, contentHash)
}

// Store stores content and returns its SHA-256 hash.
func (s *Storage) Store(ctx context.Context, reader io.Reader, size int64) (string, error) {
	tempFile, err := os.CreateTemp("", "s3-upload-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	success := false
	defer func() {
		_ = tempFile.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(tempFile, io.TeeReader(reader, hasher))
	if err != nil {
		return "", fmt.Errorf("stream content: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}
	if size > 0 && written != size {
		return "", fmt.Errorf("size mismatch: expected %d, got %d", size, written)
	}

	contentHash := hex.EncodeToString(hasher.Sum(nil))
	key := s.objectKey(contentHash)

	exists, err := s.Exists(ctx, contentHash)
	if err != nil {
		return "", err
	}
	if exists {
		_ = os.Remove(tempPath)
		s.logger.Debug().Str("content_hash", contentHash).Msg("blob already exists in S3")
		success = true
		return contentHash, nil
	}

	uploadFile, err := os.Open(tempPath)
	if err != nil {
		return "", fmt.Errorf("open temp file: %w", err)
	}
	defer uploadFile.Close()

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          uploadFile,
		ContentLength: aws.Int64(written),
	})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}

	_ = os.Remove(tempPath)
	success = true
	return contentHash, nil
}

// Retrieve returns blob content by hash.
func (s *Storage) Retrieve(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(contentHash)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, storage.ErrBlobNotFound
		}
		return nil, fmt.Errorf("get object: %w", err)
	}
	return out.Body, nil
}

// Delete removes a blob.
func (s *Storage) Delete(ctx context.Context, contentHash string) error {
	exists, err := s.Exists(ctx, contentHash)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrBlobNotFound
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(contentHash)),
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Exists checks blob presence.
func (s *Storage) Exists(ctx context.Context, contentHash string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(contentHash)),
	})
	if err != nil {
		var nsk *types.NotFound
		if errors.As(err, &nsk) {
			return false, nil
		}
		return false, fmt.Errorf("head object: %w", err)
	}
	return true, nil
}

// GetSize returns blob size.
func (s *Storage) GetSize(ctx context.Context, contentHash string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(contentHash)),
	})
	if err != nil {
		var nsk *types.NotFound
		if errors.As(err, &nsk) {
			return 0, storage.ErrBlobNotFound
		}
		return 0, fmt.Errorf("head object: %w", err)
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}

// GetPath returns the S3 URI for a blob.
func (s *Storage) GetPath(contentHash string) string {
	return fmt.Sprintf("s3://%s/%s", s.bucket, s.objectKey(contentHash))
}

// HealthCheck verifies bucket access.
func (s *Storage) HealthCheck(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("head bucket: %w", err)
	}
	return nil
}