package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type GRPCPool struct {
	connections chan *grpc.ClientConn
	target      string
	size        int
	closed      bool
	dialOpts    []grpc.DialOption
}

func NewGRPCPool(target string, size int, opts ...grpc.DialOption) (*GRPCPool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("pool size must be positive, got %d", size)
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	dialOpts = append(dialOpts, opts...)

	pool := &GRPCPool{
		connections: make(chan *grpc.ClientConn, size),
		target:      target,
		size:        size,
		dialOpts:    dialOpts,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < size; i++ {
		conn, err := grpc.DialContext(ctx, target, dialOpts...)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("failed to create connection %d: %w", i, err)
		}
		pool.connections <- conn
	}

	return pool, nil
}

// Temporary stub
func (p *GRPCPool) Close() error {
	p.closed = true
	close(p.connections)
	return nil
}
