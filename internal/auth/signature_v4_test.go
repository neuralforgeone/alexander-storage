package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetSigningKeyDeterministic(t *testing.T) {
	t.Parallel()

	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	key1 := GetSigningKey("secret", date, "us-east-1", ServiceS3)
	key2 := GetSigningKey("secret", date, "us-east-1", ServiceS3)
	require.Equal(t, key1, key2)
	require.NotEmpty(t, key1)
}

func TestGetCanonicalRequest(t *testing.T) {
	t.Parallel()

	req, _ := http.NewRequest(http.MethodGet, "http://localhost/my-bucket", nil)
	req.Host = "localhost"

	canonical := GetCanonicalRequest(req, []string{"host"}, EmptyStringSHA256)
	require.Contains(t, canonical, "GET")
	require.Contains(t, canonical, "/my-bucket")
	require.Contains(t, canonical, EmptyStringSHA256)
}

func TestVerifySignatureRoundTrip(t *testing.T) {
	t.Parallel()

	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	accessKey := "AKIAIOSFODNN7EXAMPLE"
	requestTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	req, err := http.NewRequest(http.MethodPut, "http://localhost/test-bucket/object.txt", nil)
	require.NoError(t, err)
	req.Host = "localhost"
	req.Header.Set(XAmzDateHeader, requestTime.Format(ISO8601BasicFormat))
	req.Header.Set(XAmzContentSHA256Header, UnsignedPayload)

	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	payloadHash := GetPayloadHash(req)
	canonical := GetCanonicalRequest(req, signedHeaders, payloadHash)
	scope := CredentialScope{Date: requestTime, Region: DefaultRegion, Service: ServiceS3}
	stringToSign := GetStringToSign(canonical, requestTime, scope)
	signingKey := GetSigningKey(secret, requestTime, DefaultRegion, ServiceS3)
	signature := GetSignature(signingKey, stringToSign)

	signedValues := SignedValues{
		Credential: CredentialHeader{
			AccessKey: accessKey,
			Scope:     scope,
		},
		SignedHeaders: signedHeaders,
		Signature:     signature,
	}

	err = VerifySignature(req, secret, signedValues, payloadHash)
	require.NoError(t, err)

	signedValues.Signature = "0000000000000000000000000000000000000000000000000000000000000000"
	require.Error(t, VerifySignature(req, secret, signedValues, payloadHash))
}