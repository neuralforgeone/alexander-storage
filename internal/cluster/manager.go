package cluster

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Manager implements ClusterManager using the local server and client pool.
type Manager struct {
	server *Server
	pool   *ClientPool
	logger zerolog.Logger
}

// NewManager creates a cluster manager backed by the given server.
func NewManager(server *Server, logger zerolog.Logger) *Manager {
	return &Manager{
		server: server,
		pool:   NewClientPool(logger),
		logger: logger.With().Str("component", "cluster-manager").Logger(),
	}
}

// RegisterSelf ensures this node is registered in the cluster registry.
func (m *Manager) RegisterSelf(ctx context.Context) error {
	self := &Node{
		ID:            m.server.config.NodeID,
		Address:       m.server.config.Address,
		Role:          m.server.config.Role,
		Status:        NodeStatusHealthy,
		LastHeartbeat: time.Now(),
		Stats:         m.server.getStorageStats(),
	}
	return m.server.RegisterNode(self)
}

// SendHeartbeat updates this node's heartbeat and storage stats.
func (m *Manager) SendHeartbeat(ctx context.Context) error {
	stats := m.server.getStorageStats()
	return m.server.UpdateHeartbeat(m.server.config.NodeID, stats)
}

// GetNodes returns all known nodes.
func (m *Manager) GetNodes(ctx context.Context) ([]*Node, error) {
	return m.server.GetNodes(), nil
}

// GetNode returns a node by ID.
func (m *Manager) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	return m.server.GetNode(nodeID)
}

// GetNodesByRole returns nodes filtered by role.
func (m *Manager) GetNodesByRole(ctx context.Context, role NodeRole) ([]*Node, error) {
	return m.server.GetNodesByRole(role), nil
}

// GetHealthyNodes returns healthy nodes.
func (m *Manager) GetHealthyNodes(ctx context.Context) ([]*Node, error) {
	return m.server.GetHealthyNodes(), nil
}

// GetBlobLocations returns blob replica locations.
func (m *Manager) GetBlobLocations(ctx context.Context, contentHash string) ([]*BlobLocation, error) {
	return m.server.GetBlobLocations(contentHash), nil
}

// RegisterBlobLocation registers where a blob is stored.
func (m *Manager) RegisterBlobLocation(ctx context.Context, location *BlobLocation) error {
	if location.SyncedAt.IsZero() {
		location.SyncedAt = time.Now()
	}
	return m.server.RegisterBlobLocation(location)
}

// RemoveBlobLocation removes a blob location entry.
func (m *Manager) RemoveBlobLocation(ctx context.Context, contentHash, nodeID string) error {
	return m.server.RemoveBlobLocation(contentHash, nodeID)
}

// GetClientForNode returns a gRPC client for the given node.
func (m *Manager) GetClientForNode(ctx context.Context, nodeID string) (NodeClient, error) {
	node, err := m.server.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	return m.pool.GetClient(nodeID, node.Address)
}

// Close shuts down pooled clients.
func (m *Manager) Close() error {
	return m.pool.Close()
}

var _ ClusterManager = (*Manager)(nil)