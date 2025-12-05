// Package crypto provides cryptographic utilities for Alexander Storage.
// This file contains SSE-S3 (Server-Side Encryption) implementation using
// AES-256-GCM with HKDF key derivation.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// SSE-S3 constants
const (
	// SSEKeySize is the size of derived encryption keys (256 bits).
	SSEKeySize = 32

	// SSENonceSize is the size of GCM nonce (96 bits).
	SSENonceSize = 12

	// SSETagSize is the size of GCM authentication tag (128 bits).
	SSETagSize = 16

	// SSEChunkSize is the size of chunks for streaming encryption (64KB).
	// Smaller chunks allow for better streaming but more overhead.
	SSEChunkSize = 64 * 1024

	// SSEHKDFInfo is the context info for HKDF key derivation.
	SSEHKDFInfo = "alexander-sse-s3-blob-encryption"
)

// SSE errors
var (
	// ErrSSEInvalidMasterKey indicates the master key is invalid.
	ErrSSEInvalidMasterKey = errors.New("SSE: master key must be 32 bytes")

	// ErrSSEDecryptionFailed indicates decryption failed.
	ErrSSEDecryptionFailed = errors.New("SSE: decryption failed")

	// ErrSSEInvalidData indicates the encrypted data is malformed.
	ErrSSEInvalidData = errors.New("SSE: invalid encrypted data")
)

// SSEEncryptor handles SSE-S3 blob encryption and decryption.
// It derives per-blob keys from the master key using HKDF.
type SSEEncryptor struct {
	masterKey []byte
}

// NewSSEEncryptor creates a new SSE encryptor with the given master key.
// The master key should be the same as ALEXANDER_AUTH_MASTER_KEY.
func NewSSEEncryptor(masterKey []byte) (*SSEEncryptor, error) {
	if len(masterKey) != SSEKeySize {
		return nil, ErrSSEInvalidMasterKey
	}

	// Make a copy of the key to prevent external modification
	keyCopy := make([]byte, SSEKeySize)
	copy(keyCopy, masterKey)

	return &SSEEncryptor{masterKey: keyCopy}, nil
}

// NewSSEEncryptorFromHex creates an SSE encryptor from a hex-encoded master key.
func NewSSEEncryptorFromHex(hexKey string) (*SSEEncryptor, error) {
	key, err := ParseHexKey(hexKey)
	if err != nil {
		return nil, err
	}
	return NewSSEEncryptor(key)
}

// DeriveKey derives a per-blob encryption key using HKDF-SHA256.
// The blobHash provides unique "salt" for each blob, ensuring different keys.
func (e *SSEEncryptor) DeriveKey(blobHash string) ([]byte, error) {
	// Use blob hash as salt (it's already a SHA-256 hash)
	salt := []byte(blobHash)

	// Create HKDF reader
	reader := hkdf.New(sha256.New, e.masterKey, salt, []byte(SSEHKDFInfo))

	// Derive key
	key := make([]byte, SSEKeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("failed to derive SSE key: %w", err)
	}

	return key, nil
}

// EncryptBlob encrypts blob content using AES-256-GCM.
// Returns the encrypted data with prepended nonce.
// Format: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (e *SSEEncryptor) EncryptBlob(plaintext []byte, blobHash string) ([]byte, error) {
	// Derive per-blob key
	key, err := e.DeriveKey(blobHash)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(key)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, SSENonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt: output is nonce || ciphertext || tag
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// DecryptBlob decrypts blob content encrypted with EncryptBlob.
// Expects format: nonce (12 bytes) || ciphertext || tag (16 bytes)
func (e *SSEEncryptor) DecryptBlob(ciphertext []byte, blobHash string) ([]byte, error) {
	// Minimum size: nonce + tag
	if len(ciphertext) < SSENonceSize+SSETagSize {
		return nil, ErrSSEInvalidData
	}

	// Derive per-blob key
	key, err := e.DeriveKey(blobHash)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(key)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce
	nonce := ciphertext[:SSENonceSize]
	encryptedData := ciphertext[SSENonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, ErrSSEDecryptionFailed
	}

	return plaintext, nil
}

// EncryptReader wraps a reader to encrypt data on the fly.
// Returns an io.Reader that produces encrypted data.
func (e *SSEEncryptor) EncryptReader(reader io.Reader, blobHash string) (*SSEEncryptingReader, error) {
	key, err := e.DeriveKey(blobHash)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		zeroBytes(key)
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		zeroBytes(key)
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, SSENonceSize)
	if _, err := rand.Read(nonce); err != nil {
		zeroBytes(key)
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	return &SSEEncryptingReader{
		reader:      reader,
		gcm:         gcm,
		nonce:       nonce,
		key:         key,
		nonceSent:   false,
		buffer:      nil,
		bufferIndex: 0,
		done:        false,
	}, nil
}

// DecryptReader wraps a reader to decrypt data on the fly.
// The input reader must provide data in the format: nonce || ciphertext || tag
func (e *SSEEncryptor) DecryptReader(reader io.Reader, blobHash string) (*SSEDecryptingReader, error) {
	key, err := e.DeriveKey(blobHash)
	if err != nil {
		return nil, err
	}

	return &SSEDecryptingReader{
		reader:    reader,
		key:       key,
		blobHash:  blobHash,
		encryptor: e,
		nonce:     nil,
		buffer:    nil,
	}, nil
}

// SSEEncryptingReader encrypts data as it's read.
type SSEEncryptingReader struct {
	reader      io.Reader
	gcm         cipher.AEAD
	nonce       []byte
	key         []byte
	nonceSent   bool
	buffer      []byte
	bufferIndex int
	done        bool
}

// Read implements io.Reader for streaming encryption.
// Note: For simplicity, this reads all data into memory.
// For very large files, a chunked approach would be better.
func (r *SSEEncryptingReader) Read(p []byte) (int, error) {
	// First, send the nonce
	if !r.nonceSent {
		n := copy(p, r.nonce[r.bufferIndex:])
		r.bufferIndex += n
		if r.bufferIndex >= len(r.nonce) {
			r.nonceSent = true
			r.bufferIndex = 0
		}
		return n, nil
	}

	// If we have buffered encrypted data, send it
	if r.buffer != nil && r.bufferIndex < len(r.buffer) {
		n := copy(p, r.buffer[r.bufferIndex:])
		r.bufferIndex += n
		if r.bufferIndex >= len(r.buffer) {
			// Clean up
			zeroBytes(r.key)
			return n, io.EOF
		}
		return n, nil
	}

	if r.done {
		return 0, io.EOF
	}

	// Read all remaining data and encrypt
	plaintext, err := io.ReadAll(r.reader)
	if err != nil {
		return 0, err
	}

	r.buffer = r.gcm.Seal(nil, r.nonce, plaintext, nil)
	r.bufferIndex = 0
	r.done = true

	n := copy(p, r.buffer)
	r.bufferIndex = n
	if r.bufferIndex >= len(r.buffer) {
		zeroBytes(r.key)
		return n, io.EOF
	}
	return n, nil
}

// Close cleans up resources.
func (r *SSEEncryptingReader) Close() error {
	zeroBytes(r.key)
	return nil
}

// SSEDecryptingReader decrypts data as it's read.
type SSEDecryptingReader struct {
	reader      io.Reader
	key         []byte
	blobHash    string
	encryptor   *SSEEncryptor
	nonce       []byte
	buffer      []byte
	bufferIndex int
	done        bool
}

// Read implements io.Reader for streaming decryption.
// Note: For simplicity, this reads all data into memory then decrypts.
func (r *SSEDecryptingReader) Read(p []byte) (int, error) {
	// If we have buffered decrypted data, send it
	if r.buffer != nil && r.bufferIndex < len(r.buffer) {
		n := copy(p, r.buffer[r.bufferIndex:])
		r.bufferIndex += n
		if r.bufferIndex >= len(r.buffer) {
			return n, io.EOF
		}
		return n, nil
	}

	if r.done {
		return 0, io.EOF
	}

	// Read all encrypted data
	ciphertext, err := io.ReadAll(r.reader)
	if err != nil {
		return 0, err
	}

	// Decrypt
	plaintext, err := r.encryptor.DecryptBlob(ciphertext, r.blobHash)
	if err != nil {
		return 0, err
	}

	r.buffer = plaintext
	r.bufferIndex = 0
	r.done = true
	zeroBytes(r.key)

	n := copy(p, r.buffer)
	r.bufferIndex = n
	if r.bufferIndex >= len(r.buffer) {
		return n, io.EOF
	}
	return n, nil
}

// Close cleans up resources.
func (r *SSEDecryptingReader) Close() error {
	zeroBytes(r.key)
	return nil
}

// zeroBytes zeros out a byte slice for security.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// CalculateEncryptedSize returns the size of encrypted data given plaintext size.
// Encrypted format: nonce (12) + ciphertext (same as plaintext) + tag (16)
func CalculateEncryptedSize(plaintextSize int64) int64 {
	return int64(SSENonceSize) + plaintextSize + int64(SSETagSize)
}

// CalculatePlaintextSize returns the original size given encrypted size.
func CalculatePlaintextSize(encryptedSize int64) int64 {
	overhead := int64(SSENonceSize + SSETagSize)
	if encryptedSize < overhead {
		return 0
	}
	return encryptedSize - overhead
}
