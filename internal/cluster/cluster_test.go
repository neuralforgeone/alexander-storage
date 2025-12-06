package cluster

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestMockClient_Ping(t *testing.T) {
	client := NewMockClient("node-1", "localhost:9001", NodeRoleHot)

	ctx := context.Background()
	node, err := client.Ping(ctx)
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "node-1", node.ID)
	require.Equal(t, "localhost:9001", node.Address)
	require.Equal(t, NodeRoleHot, node.Role)
	require.Equal(t, NodeStatusHealthy, node.Status)
}

func TestMockClient_TransferAndRetrieve(t *testing.T) {
	client := NewMockClient("node-1", "localhost:9001", NodeRoleHot)
	ctx := context.Background()

	// Transfer a blob
	data := "hello world"
	reader := strings.NewReader(data)
	err := client.TransferBlob(ctx, "hash1", int64(len(data)), reader)
	require.NoError(t, err)

	// Check existence
	exists, err := client.BlobExists(ctx, "hash1")
	require.NoError(t, err)
	require.True(t, exists)

	// Retrieve the blob
	rc, err := client.RetrieveBlob(ctx, "hash1")
	require.NoError(t, err)
	defer rc.Close()

	retrieved, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, data, string(retrieved))
}

func TestMockClient_BlobNotFound(t *testing.T) {
	client := NewMockClient("node-1", "localhost:9001", NodeRoleHot)
	ctx := context.Background()

	// Check non-existent blob
	exists, err := client.BlobExists(ctx, "nonexistent")
	require.NoError(t, err)
	require.False(t, exists)

	// Try to retrieve non-existent blob
	_, err = client.RetrieveBlob(ctx, "nonexistent")
	require.Error(t, err)
}

func TestMockClient_DeleteBlob(t *testing.T) {
	client := NewMockClient("node-1", "localhost:9001", NodeRoleHot)
	ctx := context.Background()

	// Transfer a blob
	data := "test data"
	reader := strings.NewReader(data)
	err := client.TransferBlob(ctx, "hash1", int64(len(data)), reader)
	require.NoError(t, err)

	// Verify it exists
	exists, err := client.BlobExists(ctx, "hash1")
	require.NoError(t, err)
	require.True(t, exists)

	// Delete it
	err = client.DeleteBlob(ctx, "hash1")
	require.NoError(t, err)

	// Verify it's gone
	exists, err = client.BlobExists(ctx, "hash1")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestMockClient_RetrieveBlobRange(t *testing.T) {
	client := NewMockClient("node-1", "localhost:9001", NodeRoleHot)
	ctx := context.Background()

	// Transfer a blob
	data := "hello world 12345"
	reader := strings.NewReader(data)
	err := client.TransferBlob(ctx, "hash1", int64(len(data)), reader)
	require.NoError(t, err)

	// Retrieve a range
	rc, err := client.RetrieveBlobRange(ctx, "hash1", 6, 5)
	require.NoError(t, err)
	defer rc.Close()

	retrieved, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, "world", string(retrieved))
}

func TestClientPool(t *testing.T) {
	logger := zerolog.Nop()

	// Create a pool
	pool := NewClientPool(logger)

	// Get client for a node
	client1, err := pool.GetClient("node-1", "localhost:9001")
	require.NoError(t, err)
	require.NotNil(t, client1)

	// Get same client again (should be cached)
	client2, err := pool.GetClient("node-1", "localhost:9001")
	require.NoError(t, err)
	require.Same(t, client1, client2)

	// Remove client
	pool.RemoveClient("node-1")

	// Close pool
	pool.Close()
}

func TestServerConfig_Default(t *testing.T) {
	config := DefaultServerConfig()

	require.Equal(t, 10*time.Second, config.HeartbeatInterval)
	require.Equal(t, 30*time.Second, config.HeartbeatTimeout)
	require.Equal(t, 10, config.MaxConcurrentTransfers)
}

func TestNodeRole_String(t *testing.T) {
	require.Equal(t, "hot", string(NodeRoleHot))
	require.Equal(t, "warm", string(NodeRoleWarm))
	require.Equal(t, "cold", string(NodeRoleCold))
}

func TestNodeStatus_String(t *testing.T) {
	require.Equal(t, "healthy", string(NodeStatusHealthy))
	require.Equal(t, "degraded", string(NodeStatusDegraded))
	require.Equal(t, "unhealthy", string(NodeStatusUnhealthy))
	require.Equal(t, "unknown", string(NodeStatusUnknown))
}

func TestBlobLocation(t *testing.T) {
	now := time.Now()
	loc := &BlobLocation{
		ContentHash: "abc123",
		NodeID:      "node-1",
		IsPrimary:   true,
		SyncedAt:    now,
	}

	require.Equal(t, "abc123", loc.ContentHash)
	require.Equal(t, "node-1", loc.NodeID)
	require.True(t, loc.IsPrimary)
	require.Equal(t, now, loc.SyncedAt)
}

func TestStorageStats(t *testing.T) {
	stats := &StorageStats{
		TotalBytes: 1000000,
		UsedBytes:  250000,
		FreeBytes:  750000,
		BlobCount:  100,
	}

	require.Equal(t, int64(1000000), stats.TotalBytes)
	require.Equal(t, int64(250000), stats.UsedBytes)
	require.Equal(t, int64(750000), stats.FreeBytes)
	require.Equal(t, int64(100), stats.BlobCount)
}

func TestNode(t *testing.T) {
	now := time.Now()
	node := &Node{
		ID:            "node-1",
		Address:       "localhost:9001",
		Role:          NodeRoleHot,
		Status:        NodeStatusHealthy,
		LastHeartbeat: now,
		Stats: &StorageStats{
			TotalBytes: 1000000,
			UsedBytes:  250000,
			FreeBytes:  750000,
			BlobCount:  100,
		},
	}

	require.Equal(t, "node-1", node.ID)
	require.Equal(t, "localhost:9001", node.Address)
	require.Equal(t, NodeRoleHot, node.Role)
	require.Equal(t, NodeStatusHealthy, node.Status)
	require.Equal(t, now, node.LastHeartbeat)
	require.NotNil(t, node.Stats)
}

func TestReplicationStatus(t *testing.T) {
	now := time.Now()
	status := &ReplicationStatus{
		ContentHash:  "abc123",
		ReplicaCount: 2,
		DesiredCount: 3,
		Locations: []*BlobLocation{
			{ContentHash: "abc123", NodeID: "node-1", IsPrimary: true, SyncedAt: now},
			{ContentHash: "abc123", NodeID: "node-2", IsPrimary: false, SyncedAt: now},
		},
		IsSufficient: false,
	}

	require.Equal(t, "abc123", status.ContentHash)
	require.Equal(t, 2, status.ReplicaCount)
	require.Equal(t, 3, status.DesiredCount)
	require.Len(t, status.Locations, 2)
	require.False(t, status.IsSufficient)
}
