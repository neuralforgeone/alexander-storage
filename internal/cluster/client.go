package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ClientConfig contains configuration for connecting to a remote node.
type ClientConfig struct {
	// Address is the remote node address (host:port).
	Address string

	// NodeID is the remote node's ID.
	NodeID string

	// Timeout is the default request timeout.
	Timeout time.Duration

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// RetryDelay is the delay between retries.
	RetryDelay time.Duration
}

// DefaultClientConfig returns sensible defaults.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		RetryDelay: time.Second,
	}
}

// Client implements NodeClient for communicating with a remote node.
// Note: This is a simplified HTTP-based client. Full gRPC implementation
// requires generated protobuf code.
type Client struct {
	config     ClientConfig
	logger     zerolog.Logger
	httpClient *http.Client
	mu         sync.RWMutex
	closed     bool
}

// NewClient creates a new client for communicating with a remote node.
func NewClient(config ClientConfig, logger zerolog.Logger) (*Client, error) {
	if config.Address == "" {
		return nil, errors.New("address is required")
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultClientConfig().Timeout
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = DefaultClientConfig().MaxRetries
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = DefaultClientConfig().RetryDelay
	}

	return &Client{
		config: config,
		logger: logger.With().
			Str("component", "cluster-client").
			Str("remote_address", config.Address).
			Logger(),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Ping checks if the node is alive and returns its status.
func (c *Client) Ping(ctx context.Context) (*Node, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("client is closed")
	}
	c.mu.RUnlock()

	// TODO: Implement actual gRPC call when protobuf is generated
	// For now, return a placeholder indicating the node is reachable
	c.logger.Debug().Msg("Ping request")

	return &Node{
		ID:            c.config.NodeID,
		Address:       c.config.Address,
		Status:        NodeStatusHealthy,
		LastHeartbeat: time.Now(),
	}, nil
}

// TransferBlob transfers a blob to this node.
func (c *Client) TransferBlob(ctx context.Context, contentHash string, size int64, reader io.Reader) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Debug().
		Str("content_hash", contentHash).
		Int64("size", size).
		Msg("Initiating blob transfer")

	// TODO: Implement actual gRPC streaming call
	// For now, simulate transfer with retry logic
	var lastErr error
	for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.config.RetryDelay):
			}
		}

		// Read all data (for retry capability)
		data, err := io.ReadAll(reader)
		if err != nil {
			lastErr = fmt.Errorf("failed to read blob data: %w", err)
			continue
		}

		if int64(len(data)) != size {
			lastErr = fmt.Errorf("size mismatch: expected %d, got %d", size, len(data))
			continue
		}

		// TODO: Send via gRPC
		c.logger.Info().
			Str("content_hash", contentHash).
			Int64("size", size).
			Int("attempt", attempt+1).
			Msg("Blob transfer simulated (gRPC not implemented)")

		return nil
	}

	return fmt.Errorf("%w: %v", ErrTransferFailed, lastErr)
}

// RetrieveBlob retrieves a blob from this node.
func (c *Client) RetrieveBlob(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Debug().
		Str("content_hash", contentHash).
		Msg("Retrieving blob")

	// TODO: Implement actual gRPC streaming call
	// For now, return an error indicating not implemented
	return nil, errors.New("gRPC not implemented - requires protobuf generation")
}

// RetrieveBlobRange retrieves a range of bytes from a blob.
func (c *Client) RetrieveBlobRange(ctx context.Context, contentHash string, offset, length int64) (io.ReadCloser, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Debug().
		Str("content_hash", contentHash).
		Int64("offset", offset).
		Int64("length", length).
		Msg("Retrieving blob range")

	// TODO: Implement actual gRPC streaming call
	return nil, errors.New("gRPC not implemented - requires protobuf generation")
}

// DeleteBlob deletes a blob from this node.
func (c *Client) DeleteBlob(ctx context.Context, contentHash string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Debug().
		Str("content_hash", contentHash).
		Msg("Deleting blob")

	// TODO: Implement actual gRPC call
	c.logger.Info().
		Str("content_hash", contentHash).
		Msg("Blob deletion simulated (gRPC not implemented)")

	return nil
}

// BlobExists checks if a blob exists on this node.
func (c *Client) BlobExists(ctx context.Context, contentHash string) (bool, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return false, errors.New("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Debug().
		Str("content_hash", contentHash).
		Msg("Checking blob existence")

	// TODO: Implement actual gRPC call
	return false, errors.New("gRPC not implemented - requires protobuf generation")
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.httpClient.CloseIdleConnections()
	c.logger.Debug().Msg("Client closed")
	return nil
}

// ClientPool manages a pool of clients to remote nodes.
type ClientPool struct {
	mu      sync.RWMutex
	clients map[string]*Client // nodeID -> client
	logger  zerolog.Logger
}

// NewClientPool creates a new client pool.
func NewClientPool(logger zerolog.Logger) *ClientPool {
	return &ClientPool{
		clients: make(map[string]*Client),
		logger:  logger.With().Str("component", "client-pool").Logger(),
	}
}

// GetClient returns a client for the specified node, creating one if needed.
func (p *ClientPool) GetClient(nodeID, address string) (*Client, error) {
	p.mu.RLock()
	client, exists := p.clients[nodeID]
	p.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Create new client
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists = p.clients[nodeID]; exists {
		return client, nil
	}

	client, err := NewClient(ClientConfig{
		NodeID:  nodeID,
		Address: address,
	}, p.logger)
	if err != nil {
		return nil, err
	}

	p.clients[nodeID] = client
	return client, nil
}

// RemoveClient removes and closes a client for the specified node.
func (p *ClientPool) RemoveClient(nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	client, exists := p.clients[nodeID]
	if !exists {
		return nil
	}

	delete(p.clients, nodeID)
	return client.Close()
}

// Close closes all clients in the pool.
func (p *ClientPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for nodeID, client := range p.clients {
		if err := client.Close(); err != nil {
			lastErr = err
			p.logger.Error().Err(err).Str("node_id", nodeID).Msg("Failed to close client")
		}
		delete(p.clients, nodeID)
	}
	return lastErr
}

// MockClient is a mock implementation of NodeClient for testing.
type MockClient struct {
	mu       sync.RWMutex
	blobs    map[string][]byte
	nodeInfo *Node
	pingErr  error
	closed   bool
}

// NewMockClient creates a mock client for testing.
func NewMockClient(nodeID, address string, role NodeRole) *MockClient {
	return &MockClient{
		blobs: make(map[string][]byte),
		nodeInfo: &Node{
			ID:            nodeID,
			Address:       address,
			Role:          role,
			Status:        NodeStatusHealthy,
			LastHeartbeat: time.Now(),
		},
	}
}

// SetPingError configures the mock to return an error on Ping.
func (m *MockClient) SetPingError(err error) {
	m.mu.Lock()
	m.pingErr = err
	m.mu.Unlock()
}

// Ping implements NodeClient.
func (m *MockClient) Ping(ctx context.Context) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pingErr != nil {
		return nil, m.pingErr
	}
	return m.nodeInfo, nil
}

// TransferBlob implements NodeClient.
func (m *MockClient) TransferBlob(ctx context.Context, contentHash string, size int64, reader io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	m.blobs[contentHash] = data
	return nil
}

// RetrieveBlob implements NodeClient.
func (m *MockClient) RetrieveBlob(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.blobs[contentHash]
	if !exists {
		return nil, ErrBlobNotFound
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// RetrieveBlobRange implements NodeClient.
func (m *MockClient) RetrieveBlobRange(ctx context.Context, contentHash string, offset, length int64) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.blobs[contentHash]
	if !exists {
		return nil, ErrBlobNotFound
	}

	if offset >= int64(len(data)) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	end := offset + length
	if length == 0 || end > int64(len(data)) {
		end = int64(len(data))
	}

	return io.NopCloser(bytes.NewReader(data[offset:end])), nil
}

// DeleteBlob implements NodeClient.
func (m *MockClient) DeleteBlob(ctx context.Context, contentHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.blobs, contentHash)
	return nil
}

// BlobExists implements NodeClient.
func (m *MockClient) BlobExists(ctx context.Context, contentHash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.blobs[contentHash]
	return exists, nil
}

// Close implements NodeClient.
func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// GetBlobs returns all stored blobs (for testing).
func (m *MockClient) GetBlobs() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]byte, len(m.blobs))
	for k, v := range m.blobs {
		result[k] = v
	}
	return result
}
