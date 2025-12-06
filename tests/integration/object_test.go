package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

// TestObjectOperations tests basic object CRUD operations.
func TestObjectOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-objects-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		// Clean up: list and delete all objects, then delete bucket
		listResult, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if listResult != nil {
			for _, obj := range listResult.Contents {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
			}
		}
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	objectKey := "test-object.txt"
	objectContent := []byte("Hello, Alexander Storage!")

	t.Run("PutObject", func(t *testing.T) {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucketName),
			Key:         aws.String(objectKey),
			Body:        bytes.NewReader(objectContent),
			ContentType: aws.String("text/plain"),
		})
		require.NoError(t, err)
	})

	t.Run("HeadObject", func(t *testing.T) {
		result, err := client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		require.Equal(t, int64(len(objectContent)), *result.ContentLength)
		require.Equal(t, "text/plain", *result.ContentType)
	})

	t.Run("GetObject", func(t *testing.T) {
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, objectContent, body)
	})

	t.Run("ListObjectsV2", func(t *testing.T) {
		result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
		require.Len(t, result.Contents, 1)
		require.Equal(t, objectKey, *result.Contents[0].Key)
	})

	t.Run("CopyObject", func(t *testing.T) {
		copyKey := "test-object-copy.txt"
		_, err := client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(bucketName),
			Key:        aws.String(copyKey),
			CopySource: aws.String(bucketName + "/" + objectKey),
		})
		require.NoError(t, err)

		// Verify copy exists
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(copyKey),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, objectContent, body)

		// Clean up copy
		_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(copyKey),
		})
	})

	t.Run("DeleteObject", func(t *testing.T) {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
	})

	t.Run("GetObject_NotFound", func(t *testing.T) {
		_, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.Error(t, err)
	})
}

// TestLargeObjectUpload tests uploading objects of various sizes.
func TestLargeObjectUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-large-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		listResult, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if listResult != nil {
			for _, obj := range listResult.Contents {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
			}
		}
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"1MB", 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
	}

	for _, tc := range sizes {
		t.Run(tc.name, func(t *testing.T) {
			// Generate random data
			data := make([]byte, tc.size)
			_, err := rand.Read(data)
			require.NoError(t, err)

			key := "large-object-" + tc.name

			// Upload
			_, err = client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
				Body:   bytes.NewReader(data),
			})
			require.NoError(t, err)

			// Download and verify
			result, err := client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			require.NoError(t, err)
			defer result.Body.Close()

			downloaded, err := io.ReadAll(result.Body)
			require.NoError(t, err)
			require.Equal(t, data, downloaded)

			// Clean up
			_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
		})
	}
}

// TestObjectMetadata tests custom metadata handling.
func TestObjectMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-metadata-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		listResult, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if listResult != nil {
			for _, obj := range listResult.Contents {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(bucketName),
					Key:    obj.Key,
				})
			}
		}
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	objectKey := "test-metadata.txt"
	metadata := map[string]string{
		"custom-key":   "custom-value",
		"another-key":  "another-value",
		"x-custom-tag": "tag-value",
	}

	t.Run("PutObject_WithMetadata", func(t *testing.T) {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			Body:     bytes.NewReader([]byte("content")),
			Metadata: metadata,
		})
		require.NoError(t, err)
	})

	t.Run("HeadObject_VerifyMetadata", func(t *testing.T) {
		result, err := client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)

		for key, expectedValue := range metadata {
			actualValue, ok := result.Metadata[key]
			require.True(t, ok, "metadata key %s should exist", key)
			require.Equal(t, expectedValue, actualValue)
		}
	})
}
