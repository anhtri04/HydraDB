// cmd/stress/grpc_stress.go
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydra-db/hydra/pkg/client"
	pb "github.com/hydra-db/hydra/server/grpc/proto"
)

const (
	targetURL    = "localhost:9090"
	poolSize     = 10
	numWorkers   = 1000
	testDuration = 30 * time.Second
)

func main() {
	fmt.Println("=== gRPC Connection Pool Stress Test ===")
	fmt.Printf("Pool Size:    %d\n", poolSize)
	fmt.Printf("Workers:      %d\n", numWorkers)
	fmt.Printf("Duration:     %v\n\n", testDuration)

	pool, err := client.NewGRPCPool(targetURL, poolSize)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var totalRequests, totalErrors, totalAppends int64
	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, pool, &totalRequests, &totalErrors, &totalAppends)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Println("\n========== RESULTS ==========")
	fmt.Printf("Duration:        %v\n", duration)
	fmt.Printf("Total Requests:  %d\n", totalRequests)
	fmt.Printf("Errors:          %d (%.4f%%)\n", totalErrors, float64(totalErrors)*100/float64(totalRequests))
	fmt.Printf("Appends:         %d\n", totalAppends)
	fmt.Printf("Throughput:      %.0f req/sec\n", float64(totalRequests)/duration.Seconds())
}

func worker(id int, pool *client.GRPCPool, totalReqs, totalErrors, totalAppends *int64) {
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	streamID := fmt.Sprintf("grpc-stream-%d", id%100)
	start := time.Now()

	for time.Since(start) < testDuration {
		conn := pool.Get()
		if conn == nil {
			atomic.AddInt64(totalErrors, 1)
			continue
		}

		client := pb.NewEventStoreClient(conn)
		_, err := client.Append(ctx, &pb.AppendRequest{
			StreamId:        streamID,
			EventId:         fmt.Sprintf("evt-%d-%d", id, time.Now().UnixNano()),
			Data:            []byte(`{"pool":"test"}`),
			ExpectedVersion: -1,
		})

		pool.Put(conn)

		atomic.AddInt64(totalReqs, 1)
		if err != nil {
			atomic.AddInt64(totalErrors, 1)
		} else {
			atomic.AddInt64(totalAppends, 1)
		}
	}
}
