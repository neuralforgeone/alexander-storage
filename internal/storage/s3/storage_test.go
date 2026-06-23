package s3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/storage"
)

func TestObjectKey(t *testing.T) {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	key := ObjectKey("blobs/", hash)
	require.Equal(t, "blobs/ab/cd/"+hash, key)
}

func TestNewStorage_RequiresBucket(t *testing.T) {
	t.Parallel()
	_, err := NewStorage(t.Context(), Config{Region: "us-east-1"}, zerolog.Nop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket")
}

func TestDelete_ReturnsNotFoundForMissingKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	s := &Storage{
		client: client,
		bucket: "test-bucket",
		prefix: "blobs/",
		logger: zerolog.Nop(),
	}

	err = s.Delete(ctx, "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	require.ErrorIs(t, err, storage.ErrBlobNotFound)
}