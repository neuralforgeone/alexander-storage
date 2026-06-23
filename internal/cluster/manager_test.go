package cluster

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/prn-tf/alexander-storage/internal/storage/filesystem"
)

func TestManager_RegisterSelfAndHeartbeat(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := filesystem.NewStorage(filesystem.Config{
		DataDir: filepath.Join(tmpDir, "data"),
		TempDir: filepath.Join(tmpDir, "temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	server, err := NewServer(ServerConfig{
		NodeID:  "mgr-node",
		Address: "127.0.0.1:0",
		Role:    NodeRoleHot,
	}, store, zerolog.Nop())
	require.NoError(t, err)

	mgr := NewManager(server, zerolog.Nop())
	ctx := context.Background()

	require.NoError(t, mgr.RegisterSelf(ctx))
	require.NoError(t, mgr.SendHeartbeat(ctx))

	nodes, err := mgr.GetNodes(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, nodes)

	node, err := mgr.GetNode(ctx, "mgr-node")
	require.NoError(t, err)
	require.Equal(t, NodeRoleHot, node.Role)
}

func TestManager_BlobLocations(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := filesystem.NewStorage(filesystem.Config{
		DataDir: filepath.Join(tmpDir, "data"),
		TempDir: filepath.Join(tmpDir, "temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	server, err := NewServer(ServerConfig{NodeID: "n1", Address: "127.0.0.1:0", Role: NodeRoleHot}, store, zerolog.Nop())
	require.NoError(t, err)
	mgr := NewManager(server, zerolog.Nop())
	ctx := context.Background()

	loc := &BlobLocation{ContentHash: "abc", NodeID: "n1", IsPrimary: true, SyncedAt: time.Now()}
	require.NoError(t, mgr.RegisterBlobLocation(ctx, loc))

	locs, err := mgr.GetBlobLocations(ctx, "abc")
	require.NoError(t, err)
	require.Len(t, locs, 1)

	require.NoError(t, mgr.RemoveBlobLocation(ctx, "abc", "n1"))
	locs, err = mgr.GetBlobLocations(ctx, "abc")
	require.NoError(t, err)
	require.Empty(t, locs)
}

func TestManager_GetClientForNode_RemoteGRPC(t *testing.T) {
	tmpDir := t.TempDir()
	remoteStore, err := filesystem.NewStorage(filesystem.Config{
		DataDir: filepath.Join(tmpDir, "remote-data"),
		TempDir: filepath.Join(tmpDir, "remote-temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	remoteServer, err := NewServer(ServerConfig{
		NodeID:  "remote-node",
		Address: "127.0.0.1:0",
		Role:    NodeRoleWarm,
	}, remoteStore, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, remoteServer.Start())
	defer remoteServer.Stop()

	remoteAddr := remoteServer.listener.Addr().String()

	coordStore, err := filesystem.NewStorage(filesystem.Config{
		DataDir: filepath.Join(tmpDir, "coord-data"),
		TempDir: filepath.Join(tmpDir, "coord-temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	coordServer, err := NewServer(ServerConfig{
		NodeID:  "coordinator",
		Address: "127.0.0.1:0",
		Role:    NodeRoleHot,
	}, coordStore, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, coordServer.Start())
	defer coordServer.Stop()

	mgr := NewManager(coordServer, zerolog.Nop())
	defer mgr.Close()

	ctx := context.Background()
	require.NoError(t, mgr.RegisterSelf(ctx))
	require.NoError(t, coordServer.RegisterNode(&Node{
		ID:            "remote-node",
		Address:       remoteAddr,
		Role:          NodeRoleWarm,
		Status:        NodeStatusHealthy,
		LastHeartbeat: time.Now(),
	}))

	client, err := mgr.GetClientForNode(ctx, "remote-node")
	require.NoError(t, err)

	payload := "manager remote grpc transfer"
	hash := sha256Hex(payload)
	require.NoError(t, client.TransferBlob(ctx, hash, int64(len(payload)), strings.NewReader(payload)))

	rc, err := client.RetrieveBlob(ctx, hash)
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
}