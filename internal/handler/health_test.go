package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type mockDB struct{ pingErr error }

func (m *mockDB) Ping(ctx context.Context) error { return m.pingErr }

type healthMockStorage struct{ healthErr error }

func (m *healthMockStorage) Store(ctx context.Context, reader io.Reader, size int64) (string, error) {
	return "", nil
}
func (m *healthMockStorage) Retrieve(ctx context.Context, hash string) (io.ReadCloser, error) {
	return nil, nil
}
func (m *healthMockStorage) Delete(ctx context.Context, hash string) error { return nil }
func (m *healthMockStorage) Exists(ctx context.Context, hash string) (bool, error) {
	return false, nil
}
func (m *healthMockStorage) GetSize(ctx context.Context, hash string) (int64, error) { return 0, nil }
func (m *healthMockStorage) GetPath(hash string) string                                { return "" }
func (m *healthMockStorage) HealthCheck(ctx context.Context) error                     { return m.healthErr }

func TestHealthChecker_HandleLiveness(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker(HealthCheckerConfig{Logger: zerolog.Nop()})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	hc.HandleLiveness(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, StatusHealthy, body["status"])
}

func TestHealthChecker_HandleHealth_AllHealthy(t *testing.T) {
	t.Parallel()

	hc := NewHealthChecker(HealthCheckerConfig{
		DatabaseChecker: &mockDB{},
		StorageBackend:  &healthMockStorage{},
		Logger:          zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	hc.HandleHealth(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var status HealthStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &status))
	require.Equal(t, StatusHealthy, status.Status)
	require.Equal(t, StatusHealthy, status.Components["database"].Status)
	require.Equal(t, StatusHealthy, status.Components["storage"].Status)
}

func TestSimpleHealth(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	SimpleHealth(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "healthy")
}