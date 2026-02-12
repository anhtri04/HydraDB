package grpc

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/hydra-db/hydra/server/grpc/proto"
	"github.com/hydra-db/hydra/pubsub"
	"github.com/hydra-db/hydra/store"
)

// Server is the gRPC server for the event store
type Server struct {
	pb.UnimplementedEventStoreServer
	store       *store.Store
	broadcaster *pubsub.Broadcaster
	grpcServer  *grpc.Server
	port        int
}

// NewServer creates a new gRPC server
func NewServer(s *store.Store, b *pubsub.Broadcaster, port int) *Server {
	return &Server{
		store:       s,
		broadcaster: b,
		grpcServer:  grpc.NewServer(),
		port:        port,
	}
}

// Start starts the gRPC server (blocking)
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	pb.RegisterEventStoreServer(s.grpcServer, s)

	log.Printf("gRPC server starting on :%d", s.port)
	return s.grpcServer.Serve(lis)
}

// Stop gracefully stops the gRPC server
func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
}

// Health implements the Health RPC
func (s *Server) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Status: "ok"}, nil
}

// Append implements the Append RPC
func (s *Server) Append(ctx context.Context, req *pb.AppendRequest) (*pb.AppendResponse, error) {
	result, err := s.store.Append(req.StreamId, req.EventId, req.Data, req.ExpectedVersion)
	if err != nil {
		switch err {
		case store.ErrWrongExpectedVersion:
			return nil, status.Errorf(codes.FailedPrecondition, "wrong expected version, current: %d", s.store.StreamVersion(req.StreamId))
		case store.ErrStreamExists:
			return nil, status.Error(codes.AlreadyExists, "stream already exists")
		case store.ErrStreamNotFound:
			return nil, status.Error(codes.NotFound, "stream not found")
		default:
			return nil, status.Errorf(codes.Internal, "internal error: %v", err)
		}
	}

	return &pb.AppendResponse{
		Position: result.Position,
		Version:  result.Version,
	}, nil
}

// ReadStream implements the ReadStream RPC (server streaming)
func (s *Server) ReadStream(req *pb.ReadStreamRequest, stream pb.EventStore_ReadStreamServer) error {
	var events []store.Event
	var err error

	if req.FromVersion > 0 {
		events, err = s.store.ReadStreamFrom(req.StreamId, req.FromVersion)
	} else {
		events, err = s.store.ReadStream(req.StreamId)
	}

	if err != nil {
		return status.Errorf(codes.Internal, "failed to read stream: %v", err)
	}

	for _, event := range events {
		if err := stream.Send(&pb.Event{
			Position: event.GlobalPosition,
			StreamId: event.StreamID,
			Version:  event.StreamVersion,
			Data:     event.Data,
		}); err != nil {
			return err
		}
	}

	return nil
}

// ReadAll implements the ReadAll RPC (server streaming)
func (s *Server) ReadAll(req *pb.ReadAllRequest, stream pb.EventStore_ReadAllServer) error {
	events, err := s.store.ReadAll()
	if err != nil {
		return status.Errorf(codes.Internal, "failed to read all: %v", err)
	}

	for _, event := range events {
		// Skip events before requested position
		if event.GlobalPosition < req.FromPosition {
			continue
		}

		if err := stream.Send(&pb.Event{
			Position: event.GlobalPosition,
			StreamId: event.StreamID,
			Version:  event.StreamVersion,
			Data:     event.Data,
		}); err != nil {
			return err
		}
	}

	return nil
}

// SubscribeToAll implements catch-up + live subscription to all events
func (s *Server) SubscribeToAll(req *pb.SubscribeToAllRequest, stream pb.EventStore_SubscribeToAllServer) error {
	// Subscribe to live events
	sub := s.broadcaster.Subscribe(nil, 1000)
	defer s.broadcaster.Unsubscribe(sub.ID)

	// Phase 1: Catch-up
	events, err := s.store.ReadAll()
	if err != nil {
		return status.Errorf(codes.Internal, "failed to read events: %v", err)
	}

	var lastPosition int64 = -1
	for _, event := range events {
		if event.GlobalPosition < req.FromPosition {
			continue
		}
		if err := stream.Send(&pb.Event{
			Position: event.GlobalPosition,
			StreamId: event.StreamID,
			Version:  event.StreamVersion,
			Data:     event.Data,
		}); err != nil {
			return err
		}
		lastPosition = event.GlobalPosition
	}

	// Phase 2: Live
	for {
		select {
		case event, ok := <-sub.EventChan:
			if !ok {
				return nil
			}
			if event.GlobalPosition <= lastPosition {
				continue
			}
			if err := stream.Send(&pb.Event{
				Position: event.GlobalPosition,
				StreamId: event.StreamID,
				Version:  event.StreamVersion,
				Data:     event.Data,
			}); err != nil {
				return err
			}
			lastPosition = event.GlobalPosition

		case <-stream.Context().Done():
			return nil
		}
	}
}

// SubscribeToStream implements catch-up + live subscription to a specific stream
func (s *Server) SubscribeToStream(req *pb.SubscribeToStreamRequest, stream pb.EventStore_SubscribeToStreamServer) error {
	// Subscribe to live events for this stream
	sub := s.broadcaster.Subscribe(&req.StreamId, 1000)
	defer s.broadcaster.Unsubscribe(sub.ID)

	// Phase 1: Catch-up
	var events []store.Event
	var err error
	if req.FromVersion > 0 {
		events, err = s.store.ReadStreamFrom(req.StreamId, req.FromVersion)
	} else {
		events, err = s.store.ReadStream(req.StreamId)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "failed to read stream: %v", err)
	}

	var lastVersion int64 = req.FromVersion - 1
	for _, event := range events {
		if err := stream.Send(&pb.Event{
			Position: event.GlobalPosition,
			StreamId: event.StreamID,
			Version:  event.StreamVersion,
			Data:     event.Data,
		}); err != nil {
			return err
		}
		lastVersion = event.StreamVersion
	}

	// Phase 2: Live
	for {
		select {
		case event, ok := <-sub.EventChan:
			if !ok {
				return nil
			}
			if event.StreamVersion <= lastVersion {
				continue
			}
			if err := stream.Send(&pb.Event{
				Position: event.GlobalPosition,
				StreamId: event.StreamID,
				Version:  event.StreamVersion,
				Data:     event.Data,
			}); err != nil {
				return err
			}
			lastVersion = event.StreamVersion

		case <-stream.Context().Done():
			return nil
		}
	}
}
