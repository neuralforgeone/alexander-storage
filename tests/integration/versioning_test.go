package integration

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

// TestObjectVersioning tests object versioning operations.
func TestObjectVersioning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-versioning-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	// Enable versioning
	_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucketName),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		// Delete all versions
		versionsResult, _ := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket: aws.String(bucketName),
		})
		if versionsResult != nil {
			for _, version := range versionsResult.Versions {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket:    aws.String(bucketName),
					Key:       version.Key,
					VersionId: version.VersionId,
				})
			}
			for _, marker := range versionsResult.DeleteMarkers {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket:    aws.String(bucketName),
					Key:       marker.Key,
					VersionId: marker.VersionId,
				})
			}
		}

		// Delete bucket
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	objectKey := "versioned-object.txt"
	content1 := []byte("Version 1 content")
	content2 := []byte("Version 2 content with more data")
	content3 := []byte("Version 3")

	var versionIDs []string

	t.Run("CreateMultipleVersions", func(t *testing.T) {
		contents := [][]byte{content1, content2, content3}

		for i, content := range contents {
			result, err := client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(objectKey),
				Body:   bytes.NewReader(content),
			})
			require.NoError(t, err)
			require.NotNil(t, result.VersionId, "version %d should have version ID", i+1)
			versionIDs = append(versionIDs, *result.VersionId)
		}

		require.Len(t, versionIDs, 3)
	})

	t.Run("GetLatestVersion", func(t *testing.T) {
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, content3, body, "should get latest version")
	})

	t.Run("GetSpecificVersion", func(t *testing.T) {
		// Get version 1
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket:    aws.String(bucketName),
			Key:       aws.String(objectKey),
			VersionId: aws.String(versionIDs[0]),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, content1, body, "should get first version")
	})

	t.Run("ListObjectVersions", func(t *testing.T) {
		result, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket: aws.String(bucketName),
			Prefix: aws.String(objectKey),
		})
		require.NoError(t, err)
		require.Len(t, result.Versions, 3)

		// Verify version IDs
		foundVersions := make(map[string]bool)
		for _, version := range result.Versions {
			foundVersions[*version.VersionId] = true
		}
		for _, versionID := range versionIDs {
			require.True(t, foundVersions[versionID], "version %s should exist", versionID)
		}
	})

	t.Run("DeleteCreatesMarker", func(t *testing.T) {
		// Delete without version ID should create delete marker
		deleteResult, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		require.NotNil(t, deleteResult.VersionId)
		require.True(t, *deleteResult.DeleteMarker)

		// Get should return 404
		_, err = client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.Error(t, err)

		// But specific version should still work
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket:    aws.String(bucketName),
			Key:       aws.String(objectKey),
			VersionId: aws.String(versionIDs[2]),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, content3, body)
	})

	t.Run("DeleteSpecificVersion", func(t *testing.T) {
		// Delete version 1 permanently
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    aws.String(bucketName),
			Key:       aws.String(objectKey),
			VersionId: aws.String(versionIDs[0]),
		})
		require.NoError(t, err)

		// Version 1 should no longer exist
		_, err = client.GetObject(ctx, &s3.GetObjectInput{
			Bucket:    aws.String(bucketName),
			Key:       aws.String(objectKey),
			VersionId: aws.String(versionIDs[0]),
		})
		require.Error(t, err)

		// Version 2 should still exist
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket:    aws.String(bucketName),
			Key:       aws.String(objectKey),
			VersionId: aws.String(versionIDs[1]),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, content2, body)
	})
}

// TestVersioningSuspend tests suspending versioning.
func TestVersioningSuspend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-suspend-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		// Delete all versions
		versionsResult, _ := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket: aws.String(bucketName),
		})
		if versionsResult != nil {
			for _, version := range versionsResult.Versions {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket:    aws.String(bucketName),
					Key:       version.Key,
					VersionId: version.VersionId,
				})
			}
			for _, marker := range versionsResult.DeleteMarkers {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket:    aws.String(bucketName),
					Key:       marker.Key,
					VersionId: marker.VersionId,
				})
			}
		}

		// Delete bucket
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	objectKey := "suspend-test.txt"

	t.Run("EnableVersioning", func(t *testing.T) {
		_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(bucketName),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatusEnabled,
			},
		})
		require.NoError(t, err)
	})

	t.Run("CreateVersionedObject", func(t *testing.T) {
		result, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
			Body:   bytes.NewReader([]byte("versioned content")),
		})
		require.NoError(t, err)
		require.NotNil(t, result.VersionId)
	})

	t.Run("SuspendVersioning", func(t *testing.T) {
		_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(bucketName),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatusSuspended,
			},
		})
		require.NoError(t, err)
	})

	t.Run("OverwriteWhileSuspended", func(t *testing.T) {
		// When versioning is suspended, overwrites use "null" version
		result, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
			Body:   bytes.NewReader([]byte("overwritten content")),
		})
		require.NoError(t, err)
		// Version ID might be "null" or empty when suspended
		_ = result
	})

	t.Run("VerifyVersionsExist", func(t *testing.T) {
		result, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket: aws.String(bucketName),
			Prefix: aws.String(objectKey),
		})
		require.NoError(t, err)
		// Should have the original version + the null version
		require.GreaterOrEqual(t, len(result.Versions), 1)
	})
}
