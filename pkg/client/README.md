# Hydra Client

Connection pooling client for Hydra Event Store supporting both HTTP and gRPC protocols.

## Features

- **HTTP Connection Pool**: Keep-alive connections for REST API
- **gRPC Connection Pool**: HTTP/2 multiplexed connections for high throughput
- **Health Checking**: Automatic unhealthy connection replacement
- **Thread-Safe**: Safe for concurrent use by multiple goroutines

## Quick Start

```go
// Simple unified client
client, err := client.NewHydraClient("http://localhost:8080", "localhost:9090")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Append event (uses gRPC internally)
err = client.Append(ctx, "my-stream", "event-1", []byte(`{"data":"value"}`))
```

## HTTP Pool Only

```go
pool, err := client.NewHTTPPool("http://localhost:8080", 20)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

// Use in worker
req, _ := http.NewRequest("GET", "http://localhost:8080/health", nil)
resp, err := pool.Do(req)
```

## gRPC Pool Only

```go
pool, err := client.NewGRPCPool("localhost:9090", 10)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

// Use in worker
conn := pool.Get()
defer pool.Put(conn)

grpcClient := pb.NewEventStoreClient(conn)
resp, err := grpcClient.Append(ctx, req)
```

## Performance

| Protocol | Pool Size | 1000 CCU | Throughput |
|----------|-----------|----------|------------|
| HTTP | 20 | < 1% errors | ~5,000 req/s |
| gRPC | 10 | < 0.1% errors | ~50,000 req/s |

## Configuration

### HTTP Pool

```go
type HTTPPool struct {
    size int  // Number of connections (default: 20)
}
```

### gRPC Pool

```go
type GRPCPool struct {
    size int  // Number of connections (default: 10)
}
```

## Testing

Run stress tests:
```bash
# HTTP
$ go run cmd/stress/http_stress.go

# gRPC
$ go run cmd/stress/grpc_stress.go
```
