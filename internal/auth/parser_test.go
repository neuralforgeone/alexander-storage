package auth

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetAuthType(t *testing.T) {
	t.Parallel()

	signed := &http.Request{Header: http.Header{}}
	signed.Header.Set(AuthorizationHeader, SignV4Algorithm+" Credential=AKIA/20240101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=abc")
	require.Equal(t, AuthTypeSignedV4, GetAuthType(signed))

	presigned := &http.Request{URL: &url.URL{RawQuery: "X-Amz-Algorithm=" + SignV4Algorithm}}
	require.Equal(t, AuthTypePresignedV4, GetAuthType(presigned))

	anon := &http.Request{URL: &url.URL{}}
	require.Equal(t, AuthTypeAnonymous, GetAuthType(anon))
}

func TestParseSignV4(t *testing.T) {
	t.Parallel()

	header := SignV4Algorithm +
		" Credential=AKIAIOSFODNN7EXAMPLE/20240101/us-east-1/s3/aws4_request," +
		" SignedHeaders=host;x-amz-date," +
		" Signature=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	sv, err := ParseSignV4(header)
	require.NoError(t, err)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", sv.Credential.AccessKey)
	require.Equal(t, "us-east-1", sv.Credential.Scope.Region)
	require.Equal(t, ServiceS3, sv.Credential.Scope.Service)
	require.Equal(t, []string{"host", "x-amz-date"}, sv.SignedHeaders)
}

func TestParseSignV4_Invalid(t *testing.T) {
	t.Parallel()
	_, err := ParseSignV4("Bearer token")
	require.Error(t, err)
}

func TestParsePresignedV4(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: &url.URL{}}
	q := req.URL.Query()
	q.Set(XAmzAlgorithmHeader, SignV4Algorithm)
	q.Set(XAmzCredentialHeader, "AKIAIOSFODNN7EXAMPLE/20240101/us-east-1/s3/aws4_request")
	q.Set(XAmzSignedHeadersHeader, "host")
	q.Set(XAmzSignatureHeader, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	q.Set(XAmzExpiresHeader, "3600")
	req.URL.RawQuery = q.Encode()

	sv, expires, err := ParsePresignedV4(req)
	require.NoError(t, err)
	require.Equal(t, int64(3600), expires)
	require.Equal(t, "AKIAIOSFODNN7EXAMPLE", sv.Credential.AccessKey)
}

func TestValidateRequestTime(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateRequestTime(time.Now().UTC()))
	require.Error(t, ValidateRequestTime(time.Now().UTC().Add(-1*time.Hour)))
}