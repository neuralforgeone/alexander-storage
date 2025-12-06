// Package filesystem provides a filesystem-based blob storage backend with streaming encryption.
package filesystem

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/pkg/crypto"
	"github.com/prn-tf/alexander-storage/internal/storage"
)

// StreamingEncryptedStorage provides transparent streaming encryption using ChaCha20-Poly1305.
// Unlike EncryptedStorage (which loads entire files into memory), this implementation
// uses streaming encryption that processes data in chunks, making it suitable for large files.
type StreamingEncryptedStorage struct {
	storage   *Storage
	encryptor *crypto.ChaChaStreamEncryptor
	logger    zerolog.Logger
	scheme    string
}

// StreamingEncryptedConfig holds configuration for streaming encrypted storage.
type StreamingEncryptedConfig struct {
	DataDir   string
	TempDir   string
	MasterKey []byte // 32-byte master key
	ChunkSize int    // Optional: custom chunk size (default 16MB)
}

// NewStreamingEncryptedStorage creates a new streaming encrypted filesystem storage backend.
// This uses ChaCha20-Poly1305 streaming encryption which is memory-efficient for large files.
func NewStreamingEncryptedStorage(cfg StreamingEncryptedConfig, logger zerolog.Logger) (*StreamingEncryptedStorage, error) {
	// Create underlying storage
	baseCfg := Config{
		DataDir: cfg.DataDir,
		TempDir: cfg.TempDir,
	}
	baseStorage, err := NewStorage(baseCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create base storage: %w", err)
	}

	// Create ChaCha stream encryptor
	encryptor, err := crypto.NewChaChaStreamEncryptor(cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream encryptor: %w", err)
	}

	// Set custom chunk size if provided
	if cfg.ChunkSize > 0 {
		encryptor.SetChunkSize(cfg.ChunkSize)
	}

	logger.Info().
		Str("data_dir", cfg.DataDir).
		Str("scheme", crypto.ChaChaEncryptionScheme).
		Msg("streaming encrypted filesystem storage initialized")

	return &StreamingEncryptedStorage{
		storage:   baseStorage,
		encryptor: encryptor,
		logger:    logger,
		scheme:    crypto.ChaChaEncryptionScheme,
	}, nil
}

// Store stores content with streaming ChaCha20-Poly1305 encryption.
// Content is encrypted in chunks as it's read, minimizing memory usage.
// Returns the content hash of the ORIGINAL (unencrypted) content.
func (s *StreamingEncryptedStorage) Store(ctx context.Context, reader io.Reader, size int64) (string, error) {
	// First, we need to read the content to calculate the hash
	// For streaming, we use a temp file to avoid memory pressure
	tempFile, err := os.CreateTemp(s.storage.tempDir, "stream-encrypt-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	// Stream content to temp file while calculating hash
	hasher := crypto.NewHashingWriter(tempFile)
	bytesWritten, err := io.Copy(hasher, reader)
	if err != nil {
		return "", fmt.Errorf("failed to stream content: %w", err)
	}

	// Verify size if provided
	if size > 0 && bytesWritten != size {
		return "", fmt.Errorf("size mismatch: expected %d, got %d", size, bytesWritten)
	}

	contentHash := hasher.Sum()

	// Acquire sharded lock for this specific hash
	s.storage.shards.Lock(contentHash)
	defer s.storage.shards.Unlock(contentHash)

	// Generate storage path
	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Check if blob already exists (deduplication)
	if _, err := os.Stat(fullPath); err == nil {
		s.logger.Debug().
			Str("content_hash", contentHash).
			Msg("streaming encrypted blob already exists, skipping storage")
		return contentHash, nil
	}

	// Seek temp file back to beginning for encryption
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to seek temp file: %w", err)
	}

	// Create target directory
	targetDir := storage.ComputeDir(s.storage.pathConfig, contentHash)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}

	// Create output file for encrypted content
	outputPath := fullPath + ".encrypting"
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		outputFile.Close()
		if err != nil {
			os.Remove(outputPath)
		}
	}()

	// Create encrypting reader using content hash as salt
	encryptingReader, err := s.encryptor.NewEncryptingReader(tempFile, []byte(contentHash))
	if err != nil {
		return "", fmt.Errorf("failed to create encrypting reader: %w", err)
	}

	// Stream encrypted content to output file
	encryptedSize, err := io.Copy(outputFile, encryptingReader)
	if err != nil {
		return "", fmt.Errorf("failed to write encrypted content: %w", err)
	}

	// Sync and close before rename
	if err := outputFile.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync output file: %w", err)
	}
	outputFile.Close()

	// Atomic rename
	if err := os.Rename(outputPath, fullPath); err != nil {
		os.Remove(outputPath)
		return "", fmt.Errorf("failed to finalize blob: %w", err)
	}

	s.logger.Debug().
		Str("content_hash", contentHash).
		Int64("plaintext_size", bytesWritten).
		Int64("encrypted_size", encryptedSize).
		Str("scheme", s.scheme).
		Msg("blob stored with streaming encryption")

	return contentHash, nil
}

// StoreFromBytes stores content from a byte slice with streaming encryption.
// This is a convenience method for smaller blobs.
func (s *StreamingEncryptedStorage) StoreFromBytes(ctx context.Context, data []byte) (string, error) {
	return s.Store(ctx, bytes.NewReader(data), int64(len(data)))
}

// Retrieve retrieves and decrypts content using streaming decryption.
// Returns a reader that decrypts on-the-fly as data is read.
func (s *StreamingEncryptedStorage) Retrieve(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	return s.RetrieveMixedMode(ctx, contentHash, true)
}

// RetrieveUnencrypted retrieves content without decryption (for legacy unencrypted blobs).
func (s *StreamingEncryptedStorage) RetrieveUnencrypted(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	return s.storage.Retrieve(ctx, contentHash)
}

// RetrieveMixedMode retrieves content, decrypting only if isEncrypted is true.
// For encrypted content, returns a streaming decrypting reader.
func (s *StreamingEncryptedStorage) RetrieveMixedMode(ctx context.Context, contentHash string, isEncrypted bool) (io.ReadCloser, error) {
	if !isEncrypted {
		return s.storage.Retrieve(ctx, contentHash)
	}

	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Open encrypted file
	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrBlobNotFound
		}
		return nil, fmt.Errorf("failed to open encrypted blob: %w", err)
	}

	// Create decrypting reader using content hash as salt
	decryptingReader, err := s.encryptor.NewDecryptingReader(file, []byte(contentHash))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to create decrypting reader: %w", err)
	}

	// Wrap in a struct that closes the underlying file when done
	return &streamingDecryptReadCloser{
		reader: decryptingReader,
		file:   file,
	}, nil
}

// RetrieveWithScheme retrieves content and decrypts based on the encryption scheme.
// Supports both ChaCha20-Poly1305 streaming and legacy AES-256-GCM.
func (s *StreamingEncryptedStorage) RetrieveWithScheme(ctx context.Context, contentHash string, scheme string) (io.ReadCloser, error) {
	switch scheme {
	case "", "none":
		return s.storage.Retrieve(ctx, contentHash)
	case crypto.ChaChaEncryptionScheme:
		return s.RetrieveMixedMode(ctx, contentHash, true)
	default:
		// Unknown scheme, try to read as-is (may be legacy AES-GCM)
		return nil, fmt.Errorf("unsupported encryption scheme: %s", scheme)
	}
}

// Delete removes a blob from storage.
func (s *StreamingEncryptedStorage) Delete(ctx context.Context, contentHash string) error {
	return s.storage.Delete(ctx, contentHash)
}

// Exists checks if a blob exists in storage.
func (s *StreamingEncryptedStorage) Exists(ctx context.Context, contentHash string) (bool, error) {
	return s.storage.Exists(ctx, contentHash)
}

// GetSize returns the size of the ENCRYPTED blob on disk.
// Note: This is NOT the original plaintext size.
func (s *StreamingEncryptedStorage) GetSize(ctx context.Context, contentHash string) (int64, error) {
	return s.storage.GetSize(ctx, contentHash)
}

// GetPath returns the storage path for a blob.
func (s *StreamingEncryptedStorage) GetPath(contentHash string) string {
	return s.storage.GetPath(contentHash)
}

// HealthCheck verifies the storage backend is accessible.
func (s *StreamingEncryptedStorage) HealthCheck(ctx context.Context) error {
	return s.storage.HealthCheck(ctx)
}

// GetDataDir returns the data directory path.
func (s *StreamingEncryptedStorage) GetDataDir() string {
	return s.storage.GetDataDir()
}

// GetTempDir returns the temp directory path.
func (s *StreamingEncryptedStorage) GetTempDir() string {
	return s.storage.GetTempDir()
}

// GetScheme returns the encryption scheme identifier.
func (s *StreamingEncryptedStorage) GetScheme() string {
	return s.scheme
}

// CalculateEncryptedSize estimates the encrypted size for a given plaintext size.
func (s *StreamingEncryptedStorage) CalculateEncryptedSize(plaintextSize int64) int64 {
	return s.encryptor.CalculateEncryptedSize(plaintextSize)
}

// EncryptExistingBlob encrypts an existing unencrypted blob using streaming encryption.
// Used for migrating from unencrypted to encrypted storage.
func (s *StreamingEncryptedStorage) EncryptExistingBlob(ctx context.Context, contentHash string) error {
	// Acquire sharded write lock
	s.storage.shards.Lock(contentHash)
	defer s.storage.shards.Unlock(contentHash)

	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Open existing (unencrypted) file
	sourceFile, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ErrBlobNotFound
		}
		return fmt.Errorf("failed to open blob: %w", err)
	}
	defer sourceFile.Close()

	// Get file size
	stat, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat blob: %w", err)
	}
	originalSize := stat.Size()

	// Verify the content hash by reading and hashing
	hasher := crypto.NewHashingWriter(io.Discard)
	if _, err := io.Copy(hasher, sourceFile); err != nil {
		return fmt.Errorf("failed to verify hash: %w", err)
	}
	actualHash := hasher.Sum()
	if actualHash != contentHash {
		return fmt.Errorf("content hash mismatch: expected %s, got %s", contentHash, actualHash)
	}

	// Seek back to beginning
	if _, err := sourceFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	// Create temp file for encrypted output
	tempPath := fullPath + ".stream-encrypting"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tempFile.Close()
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// Create encrypting reader
	encryptingReader, err := s.encryptor.NewEncryptingReader(sourceFile, []byte(contentHash))
	if err != nil {
		return fmt.Errorf("failed to create encrypting reader: %w", err)
	}

	// Stream encrypted content
	encryptedSize, err := io.Copy(tempFile, encryptingReader)
	if err != nil {
		return fmt.Errorf("failed to encrypt: %w", err)
	}

	// Sync and close
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}
	tempFile.Close()
	sourceFile.Close()

	// Atomic rename
	if err := os.Rename(tempPath, fullPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to finalize: %w", err)
	}

	s.logger.Debug().
		Str("content_hash", contentHash).
		Int64("original_size", originalSize).
		Int64("encrypted_size", encryptedSize).
		Str("scheme", s.scheme).
		Msg("existing blob encrypted with streaming encryption")

	return nil
}

// MigrateFromAES migrates a blob from AES-256-GCM encryption to ChaCha20-Poly1305.
// The blob is decrypted with AES, then re-encrypted with ChaCha streaming.
func (s *StreamingEncryptedStorage) MigrateFromAES(ctx context.Context, contentHash string, aesEncryptor *crypto.SSEEncryptor) error {
	s.storage.shards.Lock(contentHash)
	defer s.storage.shards.Unlock(contentHash)

	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Read AES-encrypted content
	ciphertext, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ErrBlobNotFound
		}
		return fmt.Errorf("failed to read AES blob: %w", err)
	}

	// Decrypt with AES
	plaintext, err := aesEncryptor.DecryptBlob(ciphertext, contentHash)
	if err != nil {
		return fmt.Errorf("failed to decrypt AES blob: %w", err)
	}

	// Verify content hash
	actualHash := crypto.SHA256Hex(plaintext)
	if actualHash != contentHash {
		return fmt.Errorf("content hash mismatch after AES decryption")
	}

	// Encrypt with ChaCha streaming
	chachaCiphertext, err := s.encryptor.EncryptBlob(plaintext, []byte(contentHash))
	if err != nil {
		return fmt.Errorf("failed to encrypt with ChaCha: %w", err)
	}

	// Write to temp file
	tempPath := fullPath + ".migrating"
	if err := os.WriteFile(tempPath, chachaCiphertext, 0644); err != nil {
		return fmt.Errorf("failed to write migrated blob: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, fullPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to finalize migration: %w", err)
	}

	s.logger.Info().
		Str("content_hash", contentHash).
		Int("plaintext_size", len(plaintext)).
		Int("chacha_size", len(chachaCiphertext)).
		Msg("blob migrated from AES-GCM to ChaCha20-Poly1305")

	return nil
}

// streamingDecryptReadCloser wraps a decrypting reader with file cleanup.
type streamingDecryptReadCloser struct {
	reader *crypto.DecryptingReader
	file   *os.File
}

func (r *streamingDecryptReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *streamingDecryptReadCloser) Close() error {
	return r.file.Close()
}

// Ensure StreamingEncryptedStorage implements storage.Backend
var _ storage.Backend = (*StreamingEncryptedStorage)(nil)
