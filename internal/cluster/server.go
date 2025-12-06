// Package cluster provides gRPC-based inter-node communication for
// multi-node Alexander Storage deployments.
package cluster

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/storage"
)

// Common errors for the cluster package.
var (
	ErrNodeNotFound      = errors.New("node not found")
	ErrBlobNotFound      = errors.New("blob not found")
	ErrConnectionFailed  = errors.New("connection failed")
	ErrTransferFailed    = errors.New("transfer failed")
	ErrInvalidRole       = errors.New("invalid node role")
	ErrReplicationFailed = errors.New("replication failed")
	ErrNodeUnavailable   = errors.New("node unavailable")
	ErrInsufficientNodes = errors.New("insufficient nodes available")
)

// rangeReader is an optional interface for backends that support range retrieval.
type rangeReader interface {
	RetrieveRange(ctx context.Context, contentHash string, offset, length int64) (io.ReadCloser, error)
}

// ServerConfig contains configuration for the gRPC server.
type ServerConfig struct {
	// NodeID is the unique identifier for this node.
	NodeID string

	// Address is the gRPC listen address (host:port).
	Address string

	// Role is the storage tier of this node.
	Role NodeRole

	// MaxConcurrentTransfers limits simultaneous blob transfers.
	MaxConcurrentTransfers int

	// HeartbeatInterval is how often to send heartbeats.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is when a node is considered dead.
	HeartbeatTimeout time.Duration
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		MaxConcurrentTransfers: 10,
		HeartbeatInterval:      10 * time.Second,
		HeartbeatTimeout:       30 * time.Second,
	}
}

// Server implements the NodeService gRPC server.
type Server struct {
	config    ServerConfig
	logger    zerolog.Logger
	storage   storage.Backend
	startTime time.Time

	// Node registry
	nodesMu sync.RWMutex
	nodes   map[string]*Node

	// Blob location tracking
	locationsMu sync.RWMutex
	locations   map[string][]*BlobLocation // contentHash -> locations

	// Transfer semaphore
	transferSem chan struct{}

	// Shutdown
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// NewServer creates a new cluster gRPC server.
func NewServer(config ServerConfig, blobStorage storage.Backend, logger zerolog.Logger) (*Server, error) {
	if config.NodeID == "" {
		return nil, errors.New("node ID is required")
	}
	if config.Address == "" {
		return nil, errors.New("address is required")
	}
	if config.Role == "" {
		config.Role = NodeRoleHot
	}
	if config.MaxConcurrentTransfers <= 0 {
		config.MaxConcurrentTransfers = DefaultServerConfig().MaxConcurrentTransfers
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = DefaultServerConfig().HeartbeatInterval
	}
	if config.HeartbeatTimeout <= 0 {
		config.HeartbeatTimeout = DefaultServerConfig().HeartbeatTimeout
	}

	return &Server{
		config:      config,
		logger:      logger.With().Str("component", "cluster-server").Logger(),
		storage:     blobStorage,
		startTime:   time.Now(),
		nodes:       make(map[string]*Node),
		locations:   make(map[string][]*BlobLocation),
		transferSem: make(chan struct{}, config.MaxConcurrentTransfers),
		shutdownCh:  make(chan struct{}),
	}, nil
}

// Start begins the gRPC server.
// Note: Full gRPC implementation requires generated protobuf code.
// This is a placeholder for the server structure.
func (s *Server) Start() error {
	s.logger.Info().
		Str("node_id", s.config.NodeID).
		Str("address", s.config.Address).
		Str("role", string(s.config.Role)).
		Msg("Starting cluster server")

	// Register self
	self := &Node{
		ID:            s.config.NodeID,
		Address:       s.config.Address,
		Role:          s.config.Role,
		Status:        NodeStatusHealthy,
		LastHeartbeat: time.Now(),
	}

	s.nodesMu.Lock()
	s.nodes[s.config.NodeID] = self
	s.nodesMu.Unlock()

	// Start background tasks
	s.wg.Add(1)
	go s.heartbeatChecker()

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.logger.Info().Msg("Stopping cluster server")
	close(s.shutdownCh)
	s.wg.Wait()
	return nil
}

// heartbeatChecker monitors node health.
func (s *Server) heartbeatChecker() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			s.checkNodeHealth()
		}
	}
}

// checkNodeHealth marks nodes as unhealthy if heartbeat times out.
func (s *Server) checkNodeHealth() {
	s.nodesMu.Lock()
	defer s.nodesMu.Unlock()

	now := time.Now()
	for _, node := range s.nodes {
		if node.ID == s.config.NodeID {
			continue // Skip self
		}
		if now.Sub(node.LastHeartbeat) > s.config.HeartbeatTimeout {
			if node.Status != NodeStatusUnhealthy {
				s.logger.Warn().
					Str("node_id", node.ID).
					Time("last_heartbeat", node.LastHeartbeat).
					Msg("Node marked unhealthy due to heartbeat timeout")
				node.Status = NodeStatusUnhealthy
			}
		}
	}
}

// GetSelfInfo returns this node's information.
func (s *Server) GetSelfInfo() *Node {
	s.nodesMu.RLock()
	defer s.nodesMu.RUnlock()

	node := s.nodes[s.config.NodeID]
	if node == nil {
		return nil
	}

	// Create a copy with updated stats
	nodeCopy := *node
	nodeCopy.Stats = s.getStorageStats()
	return &nodeCopy
}

// getStorageStats retrieves current storage statistics.
func (s *Server) getStorageStats() *StorageStats {
	// This would ideally come from the storage backend
	// For now, return placeholder stats
	return &StorageStats{
		TotalBytes: 1024 * 1024 * 1024 * 100, // 100GB placeholder
		UsedBytes:  0,
		FreeBytes:  1024 * 1024 * 1024 * 100,
		BlobCount:  0,
	}
}

// Ping handles the Ping RPC.
func (s *Server) Ping(ctx context.Context) (*Node, error) {
	return s.GetSelfInfo(), nil
}

// TransferBlob handles incoming blob transfers.
func (s *Server) TransferBlob(ctx context.Context, contentHash string, size int64, reader io.Reader) error {
	// Acquire transfer semaphore
	select {
	case s.transferSem <- struct{}{}:
		defer func() { <-s.transferSem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	s.logger.Debug().
		Str("content_hash", contentHash).
		Int64("size", size).
		Msg("Receiving blob transfer")

	// Store the blob
	storedHash, err := s.storage.Store(ctx, reader, size)
	if err != nil {
		s.logger.Error().Err(err).Str("content_hash", contentHash).Msg("Failed to store transferred blob")
		return ErrTransferFailed
	}

	// Verify hash matches
	if storedHash != contentHash {
		s.logger.Error().
			Str("expected_hash", contentHash).
			Str("actual_hash", storedHash).
			Msg("Hash mismatch after transfer")
		// Clean up the mismatched blob
		_ = s.storage.Delete(ctx, storedHash)
		return ErrTransferFailed
	}

	s.logger.Info().
		Str("content_hash", contentHash).
		Int64("size", size).
		Msg("Blob transfer completed")

	return nil
}

// RetrieveBlob retrieves a blob for transfer to another node.
func (s *Server) RetrieveBlob(ctx context.Context, contentHash string) (io.ReadCloser, error) {
	reader, err := s.storage.Retrieve(ctx, contentHash)
	if err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return reader, nil
}

// RetrieveBlobRange retrieves a range of bytes from a blob.
func (s *Server) RetrieveBlobRange(ctx context.Context, contentHash string, offset, length int64) (io.ReadCloser, error) {
	// Check if storage supports range retrieval
	if rangeStorage, ok := s.storage.(rangeReader); ok {
		return rangeStorage.RetrieveRange(ctx, contentHash, offset, length)
	}

	// Fall back to full retrieval and seek
	reader, err := s.storage.Retrieve(ctx, contentHash)
	if err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}

	// Seek to offset
	if seeker, ok := reader.(io.Seeker); ok {
		_, err = seeker.Seek(offset, io.SeekStart)
		if err != nil {
			reader.Close()
			return nil, err
		}
	} else {
		// Discard bytes up to offset
		_, err = io.CopyN(io.Discard, reader, offset)
		if err != nil {
			reader.Close()
			return nil, err
		}
	}

	if length > 0 {
		return &limitedReadCloser{
			Reader: io.LimitReader(reader, length),
			Closer: reader,
		}, nil
	}

	return reader, nil
}

// DeleteBlob deletes a blob from this node.
func (s *Server) DeleteBlob(ctx context.Context, contentHash string) error {
	err := s.storage.Delete(ctx, contentHash)
	if err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			return ErrBlobNotFound
		}
		return err
	}
	return nil
}

// BlobExists checks if a blob exists on this node.
func (s *Server) BlobExists(ctx context.Context, contentHash string) (bool, error) {
	return s.storage.Exists(ctx, contentHash)
}

// RegisterNode registers a remote node.
func (s *Server) RegisterNode(node *Node) error {
	if node.ID == "" {
		return errors.New("node ID is required")
	}

	s.nodesMu.Lock()
	defer s.nodesMu.Unlock()

	node.LastHeartbeat = time.Now()
	if node.Status == "" {
		node.Status = NodeStatusHealthy
	}

	s.nodes[node.ID] = node
	s.logger.Info().
		Str("node_id", node.ID).
		Str("address", node.Address).
		Str("role", string(node.Role)).
		Msg("Node registered")

	return nil
}

// UpdateHeartbeat updates a node's heartbeat time.
func (s *Server) UpdateHeartbeat(nodeID string, stats *StorageStats) error {
	s.nodesMu.Lock()
	defer s.nodesMu.Unlock()

	node, exists := s.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.LastHeartbeat = time.Now()
	if stats != nil {
		node.Stats = stats
	}
	if node.Status == NodeStatusUnhealthy {
		node.Status = NodeStatusHealthy
		s.logger.Info().Str("node_id", nodeID).Msg("Node recovered, marked healthy")
	}

	return nil
}

// GetNodes returns all registered nodes.
func (s *Server) GetNodes() []*Node {
	s.nodesMu.RLock()
	defer s.nodesMu.RUnlock()

	result := make([]*Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodeCopy := *node
		result = append(result, &nodeCopy)
	}
	return result
}

// GetNode returns a specific node by ID.
func (s *Server) GetNode(nodeID string) (*Node, error) {
	s.nodesMu.RLock()
	defer s.nodesMu.RUnlock()

	node, exists := s.nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	nodeCopy := *node
	return &nodeCopy, nil
}

// GetNodesByRole returns all nodes with the specified role.
func (s *Server) GetNodesByRole(role NodeRole) []*Node {
	s.nodesMu.RLock()
	defer s.nodesMu.RUnlock()

	var result []*Node
	for _, node := range s.nodes {
		if node.Role == role {
			nodeCopy := *node
			result = append(result, &nodeCopy)
		}
	}
	return result
}

// GetHealthyNodes returns all healthy nodes.
func (s *Server) GetHealthyNodes() []*Node {
	s.nodesMu.RLock()
	defer s.nodesMu.RUnlock()

	var result []*Node
	for _, node := range s.nodes {
		if node.Status == NodeStatusHealthy {
			nodeCopy := *node
			result = append(result, &nodeCopy)
		}
	}
	return result
}

// RegisterBlobLocation registers where a blob is stored.
func (s *Server) RegisterBlobLocation(location *BlobLocation) error {
	if location.ContentHash == "" || location.NodeID == "" {
		return errors.New("content hash and node ID are required")
	}

	s.locationsMu.Lock()
	defer s.locationsMu.Unlock()

	locations := s.locations[location.ContentHash]

	// Check if already registered
	for i, loc := range locations {
		if loc.NodeID == location.NodeID {
			locations[i] = location
			return nil
		}
	}

	s.locations[location.ContentHash] = append(locations, location)
	return nil
}

// GetBlobLocations returns all locations for a blob.
func (s *Server) GetBlobLocations(contentHash string) []*BlobLocation {
	s.locationsMu.RLock()
	defer s.locationsMu.RUnlock()

	locations := s.locations[contentHash]
	if len(locations) == 0 {
		return nil
	}

	result := make([]*BlobLocation, len(locations))
	for i, loc := range locations {
		locCopy := *loc
		result[i] = &locCopy
	}
	return result
}

// RemoveBlobLocation removes a blob location.
func (s *Server) RemoveBlobLocation(contentHash, nodeID string) error {
	s.locationsMu.Lock()
	defer s.locationsMu.Unlock()

	locations := s.locations[contentHash]
	for i, loc := range locations {
		if loc.NodeID == nodeID {
			s.locations[contentHash] = append(locations[:i], locations[i+1:]...)
			return nil
		}
	}
	return nil
}

// limitedReadCloser wraps a limited reader with a closer.
type limitedReadCloser struct {
	io.Reader
	io.Closer
}
