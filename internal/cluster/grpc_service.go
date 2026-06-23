package cluster

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/prn-tf/alexander-storage/internal/cluster/proto"
	"github.com/prn-tf/alexander-storage/internal/storage"
)

const grpcChunkSize = 32 * 1024

// GRPCService implements pb.NodeServiceServer by delegating to cluster.Server.
type GRPCService struct {
	pb.UnimplementedNodeServiceServer
	server *Server
}

// NewGRPCService creates a gRPC service adapter for the cluster server.
func NewGRPCService(server *Server) *GRPCService {
	return &GRPCService{server: server}
}

// Ping returns node health information.
func (g *GRPCService) Ping(ctx context.Context, _ *pb.PingRequest) (*pb.PingResponse, error) {
	node, err := g.server.Ping(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ping failed: %v", err)
	}
	if node == nil {
		return nil, status.Error(codes.Internal, "node info unavailable")
	}

	resp := &pb.PingResponse{
		NodeId:        node.ID,
		Role:          string(node.Role),
		Status:        string(node.Status),
		UptimeSeconds: int64(timeSince(g.server.startTime).Seconds()),
	}
	if node.Stats != nil {
		resp.StorageStats = &pb.StorageStats{
			TotalBytes: node.Stats.TotalBytes,
			UsedBytes:  node.Stats.UsedBytes,
			FreeBytes:  node.Stats.FreeBytes,
			BlobCount:  node.Stats.BlobCount,
		}
	}
	return resp, nil
}

// TransferBlob receives a streamed blob and stores it.
func (g *GRPCService) TransferBlob(stream pb.NodeService_TransferBlobServer) error {
	ctx := stream.Context()

	var metadata *pb.BlobMetadata
	var buf bytes.Buffer

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "receive failed: %v", err)
		}

		switch payload := req.Payload.(type) {
		case *pb.TransferBlobRequest_Metadata:
			metadata = payload.Metadata
		case *pb.TransferBlobRequest_DataChunk:
			if _, err := buf.Write(payload.DataChunk); err != nil {
				return status.Errorf(codes.Internal, "buffer write failed: %v", err)
			}
		}
	}

	if metadata == nil || metadata.ContentHash == "" {
		return status.Error(codes.InvalidArgument, "missing blob metadata")
	}

	reader := bytes.NewReader(buf.Bytes())
	if err := g.server.TransferBlob(ctx, metadata.ContentHash, metadata.Size, reader); err != nil {
		return status.Errorf(codes.Internal, "transfer failed: %v", err)
	}

	return stream.SendAndClose(&pb.TransferBlobResponse{
		Success:     true,
		ContentHash: metadata.ContentHash,
	})
}

// RetrieveBlob streams blob content to the caller.
func (g *GRPCService) RetrieveBlob(req *pb.RetrieveBlobRequest, stream pb.NodeService_RetrieveBlobServer) error {
	ctx := stream.Context()
	if req.ContentHash == "" {
		return status.Error(codes.InvalidArgument, "content hash required")
	}

	var reader io.ReadCloser
	var err error
	if req.Offset > 0 || req.Length > 0 {
		reader, err = g.server.RetrieveBlobRange(ctx, req.ContentHash, req.Offset, req.Length)
	} else {
		reader, err = g.server.RetrieveBlob(ctx, req.ContentHash)
	}
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) || errors.Is(err, storage.ErrBlobNotFound) {
			return status.Error(codes.NotFound, "blob not found")
		}
		return status.Errorf(codes.Internal, "retrieve failed: %v", err)
	}
	defer reader.Close()

	size, sizeErr := g.server.storage.GetSize(ctx, req.ContentHash)
	if sizeErr != nil && req.Length == 0 {
		size = 0
	}
	if req.Length > 0 {
		size = req.Length
	}

	if err := stream.Send(&pb.RetrieveBlobResponse{
		Payload: &pb.RetrieveBlobResponse_Metadata{
			Metadata: &pb.BlobMetadata{
				ContentHash: req.ContentHash,
				Size:        size,
				BlobType:    "single",
			},
		},
	}); err != nil {
		return err
	}

	chunk := make([]byte, grpcChunkSize)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			if err := stream.Send(&pb.RetrieveBlobResponse{
				Payload: &pb.RetrieveBlobResponse_DataChunk{DataChunk: chunk[:n]},
			}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return status.Errorf(codes.Internal, "read failed: %v", readErr)
		}
	}
}

// DeleteBlob removes a blob from this node.
func (g *GRPCService) DeleteBlob(ctx context.Context, req *pb.DeleteBlobRequest) (*pb.DeleteBlobResponse, error) {
	if req.ContentHash == "" {
		return nil, status.Error(codes.InvalidArgument, "content hash required")
	}

	if err := g.server.DeleteBlob(ctx, req.ContentHash); err != nil {
		if errors.Is(err, ErrBlobNotFound) || errors.Is(err, storage.ErrBlobNotFound) {
			return nil, status.Error(codes.NotFound, "blob not found")
		}
		return nil, status.Errorf(codes.Internal, "delete failed: %v", err)
	}

	return &pb.DeleteBlobResponse{Success: true}, nil
}

// GetBlobMetadata returns metadata for a blob.
func (g *GRPCService) GetBlobMetadata(ctx context.Context, req *pb.GetBlobMetadataRequest) (*pb.GetBlobMetadataResponse, error) {
	if req.ContentHash == "" {
		return nil, status.Error(codes.InvalidArgument, "content hash required")
	}

	exists, err := g.server.BlobExists(ctx, req.ContentHash)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "metadata lookup failed: %v", err)
	}
	if !exists {
		return &pb.GetBlobMetadataResponse{Exists: false}, nil
	}

	size, err := g.server.storage.GetSize(ctx, req.ContentHash)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "size lookup failed: %v", err)
	}

	return &pb.GetBlobMetadataResponse{
		Exists: true,
		Metadata: &pb.BlobMetadata{
			ContentHash: req.ContentHash,
			Size:        size,
			BlobType:    "single",
		},
	}, nil
}

// ListBlobs streams blob metadata from the storage backend.
func (g *GRPCService) ListBlobs(req *pb.ListBlobsRequest, stream pb.NodeService_ListBlobsServer) error {
	lister, ok := g.server.storage.(storage.BlobLister)
	if !ok {
		return status.Error(codes.Unimplemented, "storage backend does not support listing")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 1000
	}

	entries, err := lister.ListBlobs(stream.Context(), req.Prefix, limit)
	if err != nil {
		return status.Errorf(codes.Internal, "list blobs failed: %v", err)
	}

	for i, entry := range entries {
		resp := &pb.ListBlobsResponse{
			Metadata: &pb.BlobMetadata{
				ContentHash: entry.ContentHash,
				Size:        entry.Size,
				BlobType:    "single",
			},
		}
		if i == len(entries)-1 {
			resp.NextCursor = ""
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

// RegisterNode registers a remote node with this coordinator.
func (g *GRPCService) RegisterNode(ctx context.Context, req *pb.RegisterNodeRequest) (*pb.RegisterNodeResponse, error) {
	if req.NodeId == "" || req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "node id and address required")
	}

	node := &Node{
		ID:      req.NodeId,
		Address: req.Address,
		Role:    NodeRole(req.Role),
	}
	if req.StorageStats != nil {
		node.Stats = &StorageStats{
			TotalBytes: req.StorageStats.TotalBytes,
			UsedBytes:  req.StorageStats.UsedBytes,
			FreeBytes:  req.StorageStats.FreeBytes,
			BlobCount:  req.StorageStats.BlobCount,
		}
	}

	if err := g.server.RegisterNode(node); err != nil {
		return &pb.RegisterNodeResponse{Success: false, ErrorMessage: err.Error()}, nil
	}

	nodes := g.server.GetNodes()
	clusterNodes := make([]*pb.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		clusterNodes = append(clusterNodes, &pb.NodeInfo{
			NodeId:            n.ID,
			Address:           n.Address,
			Role:              string(n.Role),
			Status:            string(n.Status),
			LastHeartbeatUnix: n.LastHeartbeat.Unix(),
		})
	}

	return &pb.RegisterNodeResponse{
		Success:      true,
		ClusterNodes: clusterNodes,
	}, nil
}

// Heartbeat updates node status from a remote heartbeat.
func (g *GRPCService) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node id required")
	}

	var stats *StorageStats
	if req.StorageStats != nil {
		stats = &StorageStats{
			TotalBytes: req.StorageStats.TotalBytes,
			UsedBytes:  req.StorageStats.UsedBytes,
			FreeBytes:  req.StorageStats.FreeBytes,
			BlobCount:  req.StorageStats.BlobCount,
		}
	}

	if err := g.server.UpdateHeartbeat(req.NodeId, stats); err != nil {
		return &pb.HeartbeatResponse{Success: false}, nil
	}

	return &pb.HeartbeatResponse{Success: true}, nil
}

func timeSince(start time.Time) time.Duration {
	return time.Since(start)
}