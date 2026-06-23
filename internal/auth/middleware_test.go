package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetPayloadHash(t *testing.T) {
	t.Parallel()

	getReq, _ := http.NewRequest(http.MethodGet, "http://localhost/bucket", nil)
	require.Equal(t, EmptyStringSHA256, GetPayloadHash(getReq))

	putReq, _ := http.NewRequest(http.MethodPut, "http://localhost/bucket", nil)
	putReq.Header.Set(XAmzContentSHA256Header, UnsignedPayload)
	require.Equal(t, UnsignedPayload, GetPayloadHash(putReq))
}

func TestGetRequestTime(t *testing.T) {
	t.Parallel()

	req, _ := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.Header.Set(XAmzDateHeader, "20240101T120000Z")

	ts, err := GetRequestTime(req)
	require.NoError(t, err)
	require.Equal(t, 2024, ts.Year())
	require.Equal(t, time.January, ts.Month())
}