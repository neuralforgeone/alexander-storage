package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"net/http/httptest"
	"strings"
	"sync"
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

type mockS3Store struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMockS3Server(store *mockS3Store) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		parts := strings.SplitN(path, "/", 2)
		bucket := parts[0]
		key := ""
		if len(parts) == 2 {
			key = parts[1]
		}

		switch r.Method {
		case http.MethodHead:
			if bucket == "test-bucket" && key == "" {
				w.WriteHeader(http.StatusOK)
				return
			}
			store.mu.Lock()
			body, ok := store.objects[key]
			store.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)

		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			store.mu.Lock()
			store.objects[key] = body
			store.mu.Unlock()
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			store.mu.Lock()
			body, ok := store.objects[key]
			store.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)

		case http.MethodDelete:
			store.mu.Lock()
			_, ok := store.objects[key]
			if ok {
				delete(store.objects, key)
			}
			store.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
}

func newTestStorage(t *testing.T, serverURL string) *Storage {
	t.Helper()
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(serverURL)
		o.UsePathStyle = true
	})

	return &Storage{
		client: client,
		bucket: "test-bucket",
		prefix: "blobs/",
		logger: zerolog.Nop(),
	}
}

func TestStorage_RoundtripStoreRetrieveGetSizeHealthCheck(t *testing.T) {
	t.Parallel()

	store := &mockS3Store{objects: make(map[string][]byte)}
	server := newMockS3Server(store)
	defer server.Close()

	s := newTestStorage(t, server.URL)
	ctx := context.Background()
	payload := []byte("hello s3 streaming backend")

	hash, err := s.Store(ctx, bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)

	hasher := sha256.Sum256(payload)
	require.Equal(t, hex.EncodeToString(hasher[:]), hash)

	require.NoError(t, s.HealthCheck(ctx))

	gotSize, err := s.GetSize(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), gotSize)

	rc, err := s.Retrieve(ctx, hash)
	require.NoError(t, err)
	defer rc.Close()
	retrieved, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, retrieved)

	exists, err := s.Exists(ctx, hash)
	require.NoError(t, err)
	require.True(t, exists)

	dupHash, err := s.Store(ctx, bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)
	require.Equal(t, hash, dupHash)
}

func TestDelete_ReturnsNotFoundForMissingKey(t *testing.T) {
	t.Parallel()

	store := &mockS3Store{objects: make(map[string][]byte)}
	server := newMockS3Server(store)
	defer server.Close()

	s := newTestStorage(t, server.URL)
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	err := s.Delete(context.Background(), hash)
	require.ErrorIs(t, err, storage.ErrBlobNotFound)
}