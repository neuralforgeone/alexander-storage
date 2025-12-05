// Package filesystem provides a filesystem-based blob storage backend with optional SSE-S3 encryption.
package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/pkg/crypto"
	"github.com/prn-tf/alexander-storage/internal/storage"
)

// EncryptedStorage wraps Storage to provide transparent SSE-S3 encryption.
// All new blobs are encrypted. Reading supports both encrypted and unencrypted blobs (mixed mode).
type EncryptedStorage struct {
	storage   *Storage
	encryptor *crypto.SSEEncryptor
	logger    zerolog.Logger
}

// EncryptedConfig holds configuration for encrypted storage.
type EncryptedConfig struct {
	DataDir   string
	TempDir   string
	MasterKey []byte // 32-byte master key for SSE-S3
}

// NewEncryptedStorage creates a new encrypted filesystem storage backend.
func NewEncryptedStorage(cfg EncryptedConfig, logger zerolog.Logger) (*EncryptedStorage, error) {
	// Create underlying storage
	baseCfg := Config{
		DataDir: cfg.DataDir,
		TempDir: cfg.TempDir,
	}
	baseStorage, err := NewStorage(baseCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create base storage: %w", err)
	}

	// Create SSE encryptor
	encryptor, err := crypto.NewSSEEncryptor(cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE encryptor: %w", err)
	}

	logger.Info().
		Str("data_dir", cfg.DataDir).
		Msg("encrypted filesystem storage initialized (SSE-S3 enabled)")

	return &EncryptedStorage{
		storage:   baseStorage,
		encryptor: encryptor,
		logger:    logger,
	}, nil
}

// Store stores content with SSE-S3 encryption.
// The content is encrypted before being written to disk.
// Returns the content hash of the ORIGINAL (unencrypted) content.
func (s *EncryptedStorage) Store(ctx context.Context, reader io.Reader, size int64) (string, error) {
	s.storage.mu.Lock()
	defer s.storage.mu.Unlock()

	// First, read all content to calculate hash and encrypt
	// Note: For very large files, a streaming approach would be better
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read content: %w", err)
	}

	// Verify size if provided
	if size > 0 && int64(len(plaintext)) != size {
		return "", fmt.Errorf("size mismatch: expected %d, got %d", size, len(plaintext))
	}

	// Calculate content hash (of plaintext, for CAS addressing)
	contentHash := crypto.SHA256Hex(plaintext)

	// Generate storage path
	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Check if blob already exists (deduplication)
	if _, err := os.Stat(fullPath); err == nil {
		s.logger.Debug().
			Str("content_hash", contentHash).
			Msg("encrypted blob already exists, skipping storage")
		return contentHash, nil
	}

	// Encrypt the content
	ciphertext, err := s.encryptor.EncryptBlob(plaintext, contentHash)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt content: %w", err)
	}

	// Create target directory
	targetDir := storage.ComputeDir(s.storage.pathConfig, contentHash)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}

	// Write encrypted content to file
	if err := os.WriteFile(fullPath, ciphertext, 0644); err != nil {
		return "", fmt.Errorf("failed to write encrypted blob: %w", err)
	}

	s.logger.Debug().
		Str("content_hash", contentHash).
		Int("plaintext_size", len(plaintext)).
		Int("encrypted_size", len(ciphertext)).
		Msg("blob stored with SSE-S3 encryption")

	return contentHash, nil
}

// Retrieve retrieves and decrypts content.
// This method assumes the content is encrypted.
// For mixed mode (supporting both encrypted and unencrypted), use RetrieveMixedMode.
func (s *EncryptedStorage) Retrieve(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	return s.RetrieveMixedMode(ctx, contentHash, true)
}

// RetrieveUnencrypted retrieves content without decryption (for legacy unencrypted blobs).
func (s *EncryptedStorage) RetrieveUnencrypted(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	return s.storage.Retrieve(ctx, contentHash)
}

// RetrieveMixedMode retrieves content, decrypting only if isEncrypted is true.
// This supports the mixed mode where old blobs may be unencrypted.
func (s *EncryptedStorage) RetrieveMixedMode(ctx context.Context, contentHash string, isEncrypted bool) (io.ReadCloser, error) {
	if !isEncrypted {
		// Return raw content for unencrypted blobs
		return s.storage.Retrieve(ctx, contentHash)
	}

	s.storage.mu.RLock()
	defer s.storage.mu.RUnlock()

	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Read encrypted content
	ciphertext, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storage.ErrBlobNotFound
		}
		return nil, fmt.Errorf("failed to read encrypted blob: %w", err)
	}

	// Decrypt
	plaintext, err := s.encryptor.DecryptBlob(ciphertext, contentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt blob: %w", err)
	}

	return &bytesReadCloser{data: plaintext}, nil
}

// Delete removes a blob from storage.
func (s *EncryptedStorage) Delete(ctx context.Context, contentHash string) error {
	return s.storage.Delete(ctx, contentHash)
}

// Exists checks if a blob exists in storage.
func (s *EncryptedStorage) Exists(ctx context.Context, contentHash string) (bool, error) {
	return s.storage.Exists(ctx, contentHash)
}

// GetSize returns the size of the ENCRYPTED blob on disk.
// Note: This is NOT the original plaintext size.
func (s *EncryptedStorage) GetSize(ctx context.Context, contentHash string) (int64, error) {
	return s.storage.GetSize(ctx, contentHash)
}

// GetPath returns the storage path for a blob.
func (s *EncryptedStorage) GetPath(contentHash string) string {
	return s.storage.GetPath(contentHash)
}

// HealthCheck verifies the storage backend is accessible.
func (s *EncryptedStorage) HealthCheck(ctx context.Context) error {
	return s.storage.HealthCheck(ctx)
}

// GetDataDir returns the data directory path.
func (s *EncryptedStorage) GetDataDir() string {
	return s.storage.GetDataDir()
}

// GetTempDir returns the temp directory path.
func (s *EncryptedStorage) GetTempDir() string {
	return s.storage.GetTempDir()
}

// EncryptExistingBlob encrypts an existing unencrypted blob in place.
// Used by the encrypt-blobs migration CLI command.
func (s *EncryptedStorage) EncryptExistingBlob(ctx context.Context, contentHash string) error {
	s.storage.mu.Lock()
	defer s.storage.mu.Unlock()

	fullPath := storage.ComputePath(s.storage.pathConfig, contentHash)

	// Read existing (unencrypted) content
	plaintext, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ErrBlobNotFound
		}
		return fmt.Errorf("failed to read blob: %w", err)
	}

	// Verify the content hash matches
	actualHash := crypto.SHA256Hex(plaintext)
	if actualHash != contentHash {
		return fmt.Errorf("content hash mismatch: expected %s, got %s", contentHash, actualHash)
	}

	// Encrypt the content
	ciphertext, err := s.encryptor.EncryptBlob(plaintext, contentHash)
	if err != nil {
		return fmt.Errorf("failed to encrypt content: %w", err)
	}

	// Write encrypted content (atomic via temp file)
	tempPath := fullPath + ".encrypting"
	if err := os.WriteFile(tempPath, ciphertext, 0644); err != nil {
		return fmt.Errorf("failed to write encrypted blob: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to replace blob: %w", err)
	}

	s.logger.Debug().
		Str("content_hash", contentHash).
		Int("plaintext_size", len(plaintext)).
		Int("encrypted_size", len(ciphertext)).
		Msg("existing blob encrypted")

	return nil
}

// bytesReadCloser wraps a byte slice as an io.ReadCloser.
type bytesReadCloser struct {
	data  []byte
	index int
}

func (b *bytesReadCloser) Read(p []byte) (int, error) {
	if b.index >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.index:])
	b.index += n
	return n, nil
}

func (b *bytesReadCloser) Close() error {
	return nil
}

// Ensure EncryptedStorage implements storage.Backend
var _ storage.Backend = (*EncryptedStorage)(nil)
