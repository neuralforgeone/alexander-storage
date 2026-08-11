package auth

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// AWSChunkedReader decodes aws-chunked transfer bodies used with
// x-amz-content-sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD.
//
// Wire format per chunk:
//
//	<hex-size>[;chunk-signature=<hex>]\r\n
//	<data of size bytes>\r\n
//
// Final chunk has size 0. Chunk signatures are not re-verified here; request-level
// auth already ran. This strips framing so stored objects contain raw payload only.
type AWSChunkedReader struct {
	r           *bufio.Reader
	remaining   int
	done        bool
	trailerDone bool
}

// NewAWSChunkedReader wraps r for aws-chunked decoding.
func NewAWSChunkedReader(r io.Reader) *AWSChunkedReader {
	return &AWSChunkedReader{r: bufio.NewReader(r)}
}

func (c *AWSChunkedReader) Read(p []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}

	// Need a new chunk header
	if c.remaining == 0 {
		if err := c.readChunkHeader(); err != nil {
			return 0, err
		}
		if c.done {
			return 0, io.EOF
		}
	}

	toRead := len(p)
	if toRead > c.remaining {
		toRead = c.remaining
	}
	n, err := io.ReadFull(c.r, p[:toRead])
	c.remaining -= n
	if err == io.ErrUnexpectedEOF {
		return n, fmt.Errorf("truncated aws-chunked body")
	}
	if err != nil && err != io.EOF {
		return n, err
	}

	// After chunk data, consume trailing CRLF
	if c.remaining == 0 && !c.done {
		if _, err := c.r.Discard(2); err != nil { // \r\n
			if err == io.EOF {
				return n, fmt.Errorf("truncated aws-chunked chunk trailer")
			}
			return n, err
		}
	}

	if n > 0 {
		return n, nil
	}
	if c.done {
		return 0, io.EOF
	}
	return 0, err
}

func (c *AWSChunkedReader) readChunkHeader() error {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		// Possible trailing blank after final chunk
		c.done = true
		return nil
	}

	// size[;extensions]
	sizePart := line
	if i := strings.IndexByte(line, ';'); i >= 0 {
		sizePart = line[:i]
	}
	size, err := strconv.ParseInt(sizePart, 16, 64)
	if err != nil {
		return fmt.Errorf("invalid aws-chunked size %q: %w", sizePart, err)
	}
	if size == 0 {
		c.done = true
		// Consume final CRLF after zero chunk if present (best-effort)
		_ = c.consumeOptionalCRLF()
		// Optional trailers until blank line — drain lightly
		c.drainTrailers()
		return nil
	}
	c.remaining = int(size)
	return nil
}

func (c *AWSChunkedReader) consumeOptionalCRLF() error {
	b, err := c.r.Peek(2)
	if err != nil {
		return err
	}
	if len(b) >= 2 && b[0] == '\r' && b[1] == '\n' {
		_, err = c.r.Discard(2)
		return err
	}
	return nil
}

func (c *AWSChunkedReader) drainTrailers() {
	if c.trailerDone {
		return
	}
	c.trailerDone = true
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return
		}
		if line == "\r\n" || line == "\n" || line == "" {
			return
		}
	}
}

// IsStreamingPayload reports whether the request uses aws-chunked streaming payload.
func IsStreamingPayload(contentSHA256 string) bool {
	return contentSHA256 == StreamingPayload ||
		contentSHA256 == "STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER" ||
		contentSHA256 == "STREAMING-UNSIGNED-PAYLOAD-TRAILER"
}
