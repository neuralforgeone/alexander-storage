package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	pb "github.com/prn-tf/alexander-storage/internal/cluster/proto"
	"github.com/prn-tf/alexander-storage/internal/storage/filesystem"
)

func startTestGRPCServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	tempDir := filepath.Join(tmpDir, "temp")

	store, err := filesystem.NewStorage(filesystem.Config{
		DataDir: dataDir,
		TempDir: tempDir,
	}, zerolog.Nop())
	require.NoError(t, err)

	server, err := NewServer(ServerConfig{
		NodeID:  "test-node",
		Address: "127.0.0.1:0",
		Role:    NodeRoleHot,
	}, store, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, server.Start())

	return server.listener.Addr().String(), func() {
		_ = server.Stop()
	}
}

func sha256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestGRPCClientServer_Ping(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	client, err := NewClient(ClientConfig{Address: addr, NodeID: "test-node"}, zerolog.Nop())
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node, err := client.Ping(ctx)
	require.NoError(t, err)
	require.Equal(t, "test-node", node.ID)
	require.Equal(t, NodeStatusHealthy, node.Status)
}

func TestGRPCClientServer_TransferRetrieveDelete(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	client, err := NewClient(ClientConfig{Address: addr}, zerolog.Nop())
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	payload := "hello grpc cluster"
	hash := sha256Hex(payload)

	err = client.TransferBlob(ctx, hash, int64(len(payload)), strings.NewReader(payload))
	require.NoError(t, err)

	exists, err := client.BlobExists(ctx, hash)
	require.NoError(t, err)
	require.True(t, exists)

	rc, err := client.RetrieveBlob(ctx, hash)
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))

	err = client.DeleteBlob(ctx, hash)
	require.NoError(t, err)

	exists, err = client.BlobExists(ctx, hash)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestGRPCClientServer_DeleteBlobNotFound(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	client, err := NewClient(ClientConfig{Address: addr}, zerolog.Nop())
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	err = client.DeleteBlob(ctx, sha256Hex("missing"))
	require.ErrorIs(t, err, ErrBlobNotFound)
}

func TestGRPCClientServer_RetrieveRange(t *testing.T) {
	addr, cleanup := startTestGRPCServer(t)
	defer cleanup()

	client, err := NewClient(ClientConfig{Address: addr}, zerolog.Nop())
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	payload := "hello world range"
	hash := sha256Hex(payload)

	require.NoError(t, client.TransferBlob(ctx, hash, int64(len(payload)), strings.NewReader(payload)))

	rc, err := client.RetrieveBlobRange(ctx, hash, 6, 5)
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, "world", string(got))
}

func TestGRPCService_RegisterAndHeartbeat(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := filesystem.NewStorage(filesystem.Config{
		DataDir: filepath.Join(tmpDir, "data"),
		TempDir: filepath.Join(tmpDir, "temp"),
	}, zerolog.Nop())
	require.NoError(t, err)

	server, err := NewServer(ServerConfig{
		NodeID:  "coordinator",
		Address: "127.0.0.1:0",
		Role:    NodeRoleHot,
	}, store, zerolog.Nop())
	require.NoError(t, err)

	svc := NewGRPCService(server)
	ctx := context.Background()

	regResp, err := svc.RegisterNode(ctx, &pb.RegisterNodeRequest{
		NodeId:  "worker-1",
		Address: "127.0.0.1:9002",
		Role:    "warm",
	})
	require.NoError(t, err)
	require.True(t, regResp.Success)
	require.NotEmpty(t, regResp.ClusterNodes)

	hbResp, err := svc.Heartbeat(ctx, &pb.HeartbeatRequest{NodeId: "worker-1"})
	require.NoError(t, err)
	require.True(t, hbResp.Success)
}