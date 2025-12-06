package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

// TestMultipartUpload tests multipart upload operations.
func TestMultipartUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := getTestConfig()
	client := newS3Client(t, cfg)
	ctx := context.Background()

	bucketName := "test-multipart-" + time.Now().Format("20060102150405")

	// Create bucket
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		// Abort any remaining multipart uploads
		uploads, _ := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
			Bucket: aws.String(bucketName),
		})
		if uploads != nil {
			for _, upload := range uploads.Uploads {
				_, _ = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
					Bucket:   aws.String(bucketName),
					Key:      upload.Key,
					UploadId: upload.UploadId,
				})
			}
		}

		// Delete all objects
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

		// Delete bucket
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
	})

	t.Run("CompleteMultipartUpload", func(t *testing.T) {
		objectKey := "multipart-complete.bin"
		partSize := 5 * 1024 * 1024 // 5MB minimum part size
		numParts := 3

		// Generate random data
		totalSize := partSize * numParts
		data := make([]byte, totalSize)
		_, err := rand.Read(data)
		require.NoError(t, err)

		// Initiate multipart upload
		initResult, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		uploadID := initResult.UploadId

		// Upload parts
		var completedParts []types.CompletedPart
		for i := 0; i < numParts; i++ {
			partNumber := int32(i + 1)
			start := i * partSize
			end := start + partSize

			uploadResult, err := client.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:     aws.String(bucketName),
				Key:        aws.String(objectKey),
				UploadId:   uploadID,
				PartNumber: aws.Int32(partNumber),
				Body:       bytes.NewReader(data[start:end]),
			})
			require.NoError(t, err)

			completedParts = append(completedParts, types.CompletedPart{
				ETag:       uploadResult.ETag,
				PartNumber: aws.Int32(partNumber),
			})
		}

		// Complete multipart upload
		_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			UploadId: uploadID,
			MultipartUpload: &types.CompletedMultipartUpload{
				Parts: completedParts,
			},
		})
		require.NoError(t, err)

		// Verify the uploaded object
		getResult, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		defer getResult.Body.Close()

		downloaded, err := io.ReadAll(getResult.Body)
		require.NoError(t, err)
		require.Equal(t, data, downloaded)
	})

	t.Run("AbortMultipartUpload", func(t *testing.T) {
		objectKey := "multipart-abort.bin"

		// Initiate multipart upload
		initResult, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		uploadID := initResult.UploadId

		// Upload one part
		_, err = client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucketName),
			Key:        aws.String(objectKey),
			UploadId:   uploadID,
			PartNumber: aws.Int32(1),
			Body:       bytes.NewReader([]byte("test data")),
		})
		require.NoError(t, err)

		// Abort multipart upload
		_, err = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			UploadId: uploadID,
		})
		require.NoError(t, err)

		// Verify the upload is aborted (object should not exist)
		_, err = client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.Error(t, err)
	})

	t.Run("ListMultipartUploads", func(t *testing.T) {
		objectKey := "multipart-list.bin"

		// Initiate multipart upload
		initResult, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		uploadID := initResult.UploadId

		// List multipart uploads
		listResult, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)

		found := false
		for _, upload := range listResult.Uploads {
			if *upload.Key == objectKey && *upload.UploadId == *uploadID {
				found = true
				break
			}
		}
		require.True(t, found, "initiated upload should appear in list")

		// Clean up
		_, _ = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			UploadId: uploadID,
		})
	})

	t.Run("ListParts", func(t *testing.T) {
		objectKey := "multipart-listparts.bin"
		partSize := 5 * 1024 * 1024 // 5MB minimum part size

		// Initiate multipart upload
		initResult, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
		})
		require.NoError(t, err)
		uploadID := initResult.UploadId

		// Upload two parts
		data := make([]byte, partSize)
		_, err = rand.Read(data)
		require.NoError(t, err)

		for i := 1; i <= 2; i++ {
			_, err = client.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:     aws.String(bucketName),
				Key:        aws.String(objectKey),
				UploadId:   uploadID,
				PartNumber: aws.Int32(int32(i)),
				Body:       bytes.NewReader(data),
			})
			require.NoError(t, err)
		}

		// List parts
		listResult, err := client.ListParts(ctx, &s3.ListPartsInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			UploadId: uploadID,
		})
		require.NoError(t, err)
		require.Len(t, listResult.Parts, 2)

		// Verify part numbers are correct
		partNumbers := make([]int32, len(listResult.Parts))
		for i, part := range listResult.Parts {
			partNumbers[i] = *part.PartNumber
		}
		sort.Slice(partNumbers, func(i, j int) bool { return partNumbers[i] < partNumbers[j] })
		require.Equal(t, []int32{1, 2}, partNumbers)

		// Clean up
		_, _ = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucketName),
			Key:      aws.String(objectKey),
			UploadId: uploadID,
		})
	})
}
