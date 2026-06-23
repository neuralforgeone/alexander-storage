package s3

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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