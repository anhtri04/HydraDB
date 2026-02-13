package client

import (
	"context"
	"fmt"

	pb "github.com/hydra-db/hydra/server/grpc/proto"
)

type HydraClient struct {
	HTTP *HTTPPool
	GRPC *GRPCPool
}

type Config struct {
	HTTPAddr     string
	GRPCAddr     string
	HTTPPoolSize int
	GRPCPoolSize int
}

func DefaultConfig() *Config {
	return &Config{
		HTTPAddr:     "http://localhost:8080",
		GRPCAddr:     "localhost:9090",
		HTTPPoolSize: 20,
		GRPCPoolSize: 10,
	}
}

func NewHydraClient(httpAddr, grpcAddr string) (*HydraClient, error) {
	cfg := DefaultConfig()
	cfg.HTTPAddr = httpAddr
	cfg.GRPCAddr = grpcAddr
	return NewHydraClientWithConfig(cfg)
}

func NewHydraClientWithConfig(cfg *Config) (*HydraClient, error) {
	httpPool, err := NewHTTPPool(cfg.HTTPAddr, cfg.HTTPPoolSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP pool: %w", err)
	}

	grpcPool, err := NewGRPCPool(cfg.GRPCAddr, cfg.GRPCPoolSize)
	if err != nil {
		httpPool.Close()
		return nil, fmt.Errorf("failed to create gRPC pool: %w", err)
	}

	return &HydraClient{
		HTTP: httpPool,
		GRPC: grpcPool,
	}, nil
}

func (c *HydraClient) Close() error {
	c.HTTP.Close()
	c.GRPC.Close()
	return nil
}

func (c *HydraClient) Append(ctx context.Context, streamID, eventID string, data []byte) error {
	conn := c.GRPC.Get()
	if conn == nil {
		return ErrPoolClosed
	}
	defer c.GRPC.Put(conn)

	client := pb.NewEventStoreClient(conn)
	_, err := client.Append(ctx, &pb.AppendRequest{
		StreamId:        streamID,
		EventId:         eventID,
		Data:            data,
		ExpectedVersion: -1,
	})
	return err
}
