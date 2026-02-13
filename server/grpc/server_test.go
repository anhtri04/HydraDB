package grpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/hydra-db/hydra/server/grpc/proto"
	"github.com/hydra-db/hydra/pubsub"
	"github.com/hydra-db/hydra/store"
)

// getFreePort finds an available TCP port
func getFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type testServer struct {
	store       *store.Store
	broadcaster *pubsub.Broadcaster
	grpcServer  *Server
	port        int
	cleanup     func()
}

func setupTestServer(t *testing.T) *testServer {
	// Create temp directory for the log
	tmpDir := t.TempDir()

	// Open store (log expects a directory)
	s, err := store.Open(tmpDir)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}

	// Create broadcaster
	broadcaster := pubsub.NewBroadcaster()
	s.SetBroadcaster(broadcaster)

	// Get free port
	port, err := getFreePort()
	if err != nil {
		s.Close()
		t.Fatalf("Failed to get free port: %v", err)
	}

	// Create and start gRPC server
	grpcSrv := NewServer(s, broadcaster, port)
	go func() {
		if err := grpcSrv.Start(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	cleanup := func() {
		grpcSrv.Stop()
		s.Close()
	}

	return &testServer{
		store:       s,
		broadcaster: broadcaster,
		grpcServer:  grpcSrv,
		port:        port,
		cleanup:     cleanup,
	}
}

func (ts *testServer) dial() (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return grpc.DialContext(ctx, fmt.Sprintf("localhost:%d", ts.port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func TestHealth(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}
}

func TestAppend(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test append to new stream
	resp, err := client.Append(ctx, &pb.AppendRequest{
		StreamId:        "test-stream",
		EventId:         "event-1",
		Data:            []byte(`{"test":"data"}`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	if resp.Position < 0 {
		t.Errorf("Expected non-negative position, got %d", resp.Position)
	}
	if resp.Version != 0 {
		t.Errorf("Expected version 0 for first event, got %d", resp.Version)
	}
}

func TestAppendWrongVersion(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Append first event
	_, err = client.Append(ctx, &pb.AppendRequest{
		StreamId:        "test-stream",
		EventId:         "event-1",
		Data:            []byte(`{"test":"data1"}`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("First append failed: %v", err)
	}

	// Try append with wrong expected version
	_, err = client.Append(ctx, &pb.AppendRequest{
		StreamId:        "test-stream",
		EventId:         "event-2",
		Data:            []byte(`{"test":"data2"}`),
		ExpectedVersion: 0, // Should fail - current version is 0 (first event at version 0)
	})
	if err == nil {
		t.Fatal("Expected error for wrong expected version, got nil")
	}
}

func TestReadStream(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Append events (use ExpectedVersionAny = -1 for simplicity)
	for i := 0; i < 3; i++ {
		_, err := client.Append(ctx, &pb.AppendRequest{
			StreamId:        "test-stream",
			EventId:         fmt.Sprintf("evt-%d", i),
			Data:            []byte(fmt.Sprintf(`{"index":%d}`, i)),
			ExpectedVersion: -1, // ExpectedVersionAny
		})
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	// Read stream
	stream, err := client.ReadStream(ctx, &pb.ReadStreamRequest{
		StreamId:    "test-stream",
		FromVersion: 0,
	})
	if err != nil {
		t.Fatalf("ReadStream failed: %v", err)
	}

	var events []*pb.Event
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	for i, event := range events {
		if event.StreamId != "test-stream" {
			t.Errorf("Event %d: expected stream_id 'test-stream', got '%s'", i, event.StreamId)
		}
		if event.Version != int64(i) {
			t.Errorf("Event %d: expected version %d, got %d", i, i, event.Version)
		}
	}
}

func TestReadAll(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Append events to different streams
	streams := []string{"stream-a", "stream-b", "stream-c"}
	for _, stream := range streams {
		_, err := client.Append(ctx, &pb.AppendRequest{
			StreamId:        stream,
			EventId:         "event-1",
			Data:            []byte(fmt.Sprintf(`{"stream":"%s"}`, stream)),
			ExpectedVersion: 0,
		})
		if err != nil {
			t.Fatalf("Append to %s failed: %v", stream, err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Read all
	stream, err := client.ReadAll(ctx, &pb.ReadAllRequest{
		FromPosition: 0,
	})
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	var events []*pb.Event
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}
}

func TestSubscribeToAll(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start subscription
	stream, err := client.SubscribeToAll(ctx, &pb.SubscribeToAllRequest{
		FromPosition: 0,
	})
	if err != nil {
		t.Fatalf("SubscribeToAll failed: %v", err)
	}

	// Collect events in background
	eventCh := make(chan *pb.Event, 10)
	errCh := make(chan error, 1)
	go func() {
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			eventCh <- event
		}
	}()

	// Give subscription time to catch up
	time.Sleep(100 * time.Millisecond)

	// Append an event
	_, err = client.Append(ctx, &pb.AppendRequest{
		StreamId:        "test-stream",
		EventId:         "live-event",
		Data:            []byte(`{"type":"live"}`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Wait for event
	select {
	case event := <-eventCh:
		if event.StreamId != "test-stream" {
			t.Errorf("Expected stream 'test-stream', got '%s'", event.StreamId)
		}
	case err := <-errCh:
		t.Fatalf("Recv error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestSubscribeToStream(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	conn, err := ts.dial()
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start subscription before appending
	stream, err := client.SubscribeToStream(ctx, &pb.SubscribeToStreamRequest{
		StreamId:    "test-stream",
		FromVersion: 0,
	})
	if err != nil {
		t.Fatalf("SubscribeToStream failed: %v", err)
	}

	// Collect events in background
	eventCh := make(chan *pb.Event, 10)
	errCh := make(chan error, 1)
	go func() {
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			eventCh <- event
		}
	}()

	// Give subscription time to set up
	time.Sleep(100 * time.Millisecond)

	// Append events to the stream (use ExpectedVersionAny = -1 for simplicity)
	for i := 0; i < 3; i++ {
		_, err = client.Append(ctx, &pb.AppendRequest{
			StreamId:        "test-stream",
			EventId:         fmt.Sprintf("evt-%d", i),
			Data:            []byte(fmt.Sprintf(`{"index":%d}`, i)),
			ExpectedVersion: -1, // ExpectedVersionAny
		})
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	// Wait for events
	for i := 0; i < 3; i++ {
		select {
		case event := <-eventCh:
			if event.Version != int64(i) {
				t.Errorf("Event %d: expected version %d, got %d", i, i, event.Version)
			}
		case err := <-errCh:
			t.Fatalf("Recv error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for event %d", i)
		}
	}
}
