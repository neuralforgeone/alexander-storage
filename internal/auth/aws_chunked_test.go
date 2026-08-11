package auth

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAWSChunkedReader(t *testing.T) {
	t.Parallel()

	// Two data chunks + final zero chunk (signatures ignored)
	raw := "" +
		"5;chunk-signature=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n" +
		"hello\r\n" +
		"6;chunk-signature=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\r\n" +
		" world\r\n" +
		"0;chunk-signature=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\r\n" +
		"\r\n"

	got, err := io.ReadAll(NewAWSChunkedReader(strings.NewReader(raw)))
	require.NoError(t, err)
	require.Equal(t, "hello world", string(got))
}

func TestIsStreamingPayload(t *testing.T) {
	t.Parallel()
	require.True(t, IsStreamingPayload(StreamingPayload))
	require.False(t, IsStreamingPayload(UnsignedPayload))
	require.False(t, IsStreamingPayload(EmptyStringSHA256))
}
