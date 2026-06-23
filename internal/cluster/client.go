package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/prn-tf/alexander-storage/internal/cluster/proto"
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

func grpcNotFound(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return ErrBlobNotFound
	}
	return nil
}

// Client implements NodeClient for communicating with a remote node via gRPC.
type Client struct {
	config     ClientConfig
	logger     zerolog.Logger
	conn       *grpc.ClientConn
	rpc        pb.NodeServiceClient
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

	conn, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial gRPC: %w", err)
	}

	return &Client{
		config: config,
		logger: logger.With().
			Str("component", "cluster-client").
			Str("remote_address", config.Address).
			Logger(),
		conn: conn,
		rpc:  pb.NewNodeServiceClient(conn),
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

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	resp, err := c.rpc.Ping(ctx, &pb.PingRequest{})
	if err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	node := &Node{
		ID:            resp.NodeId,
		Address:       c.config.Address,
		Role:          NodeRole(resp.Role),
		Status:        NodeStatus(resp.Status),
		LastHeartbeat: time.Now(),
	}
	if resp.StorageStats != nil {
		node.Stats = &StorageStats{
			TotalBytes: resp.StorageStats.TotalBytes,
			UsedBytes:  resp.StorageStats.UsedBytes,
			FreeBytes:  resp.StorageStats.FreeBytes,
			BlobCount:  resp.StorageStats.BlobCount,
		}
	}
	if node.ID == "" {
		node.ID = c.config.NodeID
	}

	return node, nil
}

// TransferBlob transfers a blob to this node.
func (c *Client) TransferBlob(ctx context.Context, contentHash string, size int64, reader io.Reader) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("client is closed")
	}
	c.mu.RUnlock()

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read blob data: %w", err)
	}
	if size > 0 && int64(len(data)) != size {
		return fmt.Errorf("size mismatch: expected %d, got %d", size, len(data))
	}

	var lastErr error
	for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.config.RetryDelay):
			}
		}

		if err := c.transferBlobOnce(ctx, contentHash, size, data); err != nil {
			lastErr = err
			continue
		}

		c.logger.Info().
			Str("content_hash", contentHash).
			Int64("size", size).
			Int("attempt", attempt+1).
			Msg("Blob transfer completed")

		return nil
	}

	return fmt.Errorf("%w: %v", ErrTransferFailed, lastErr)
}

func (c *Client) transferBlobOnce(ctx context.Context, contentHash string, size int64, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	stream, err := c.rpc.TransferBlob(ctx)
	if err != nil {
		return err
	}

	if err := stream.Send(&pb.TransferBlobRequest{
		Payload: &pb.TransferBlobRequest_Metadata{
			Metadata: &pb.BlobMetadata{
				ContentHash: contentHash,
				Size:        size,
				BlobType:    "single",
			},
		},
	}); err != nil {
		return err
	}

	for offset := 0; offset < len(data); offset += grpcChunkSize {
		end := offset + grpcChunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := stream.Send(&pb.TransferBlobRequest{
			Payload: &pb.TransferBlobRequest_DataChunk{DataChunk: data[offset:end]},
		}); err != nil {
			return err
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	if !resp.Success {
		if resp.ErrorMessage != "" {
			return errors.New(resp.ErrorMessage)
		}
		return ErrTransferFailed
	}
	return nil
}

// RetrieveBlob retrieves a blob from this node.
func (c *Client) RetrieveBlob(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	return c.RetrieveBlobRange(ctx, contentHash, 0, 0)
}

// RetrieveBlobRange retrieves a range of bytes from a blob.
func (c *Client) RetrieveBlobRange(ctx context.Context, contentHash string, offset, length int64) (io.ReadCloser, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, errors.New("client is closed")
	}
	c.mu.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	stream, err := c.rpc.RetrieveBlob(ctx, &pb.RetrieveBlobRequest{
		ContentHash: contentHash,
		Offset:      offset,
		Length:      length,
	})
	if err != nil {
		if nf := grpcNotFound(err); nf != nil {
			return nil, nf
		}
		return nil, fmt.Errorf("retrieve failed: %w", err)
	}

	var buf bytes.Buffer
	for {
		resp, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			if nf := grpcNotFound(recvErr); nf != nil {
				return nil, nf
			}
			return nil, fmt.Errorf("retrieve failed: %w", recvErr)
		}
		if chunk := resp.GetDataChunk(); chunk != nil {
			buf.Write(chunk)
		}
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// DeleteBlob deletes a blob from this node.
func (c *Client) DeleteBlob(ctx context.Context, contentHash string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("client is closed")
	}
	c.mu.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	resp, err := c.rpc.DeleteBlob(ctx, &pb.DeleteBlobRequest{ContentHash: contentHash})
	if err != nil {
		if nf := grpcNotFound(err); nf != nil {
			return nf
		}
		return fmt.Errorf("delete failed: %w", err)
	}
	if !resp.Success {
		if resp.ErrorMessage != "" {
			return errors.New(resp.ErrorMessage)
		}
		return ErrBlobNotFound
	}

	c.logger.Info().Str("content_hash", contentHash).Msg("Blob deleted")
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

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	resp, err := c.rpc.GetBlobMetadata(ctx, &pb.GetBlobMetadataRequest{ContentHash: contentHash})
	if err != nil {
		return false, fmt.Errorf("metadata lookup failed: %w", err)
	}
	return resp.Exists, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return err
		}
	}
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

	p.mu.Lock()
	defer p.mu.Unlock()

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