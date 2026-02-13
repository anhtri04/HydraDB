// Realistic load test: 100 connections, high throughput per connection
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/hydra-db/hydra/server/grpc/proto"
)

const (
	targetURL     = "localhost:9090"
	numWorkers    = 100   // 100 concurrent connections (realistic)
	opsPerWorker  = 1000  // 1000 operations per connection
	testDuration  = 30 * time.Second
)

func main() {
	fmt.Printf("Realistic Load Test: %d workers x %d ops, %v duration\n\n", numWorkers, opsPerWorker, testDuration)

	var totalReqs, totalErrors, totalAppends int64
	start := time.Now()

	var wg sync.WaitGroup
	results := make(chan result, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i, &wg, results)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		totalReqs += r.requests
		totalErrors += r.errors
		totalAppends += r.appends
	}

	duration := time.Since(start)

	fmt.Println("========== RESULTS ==========")
	fmt.Printf("Duration:        %v\n", duration)
	fmt.Printf("Total Requests:  %d\n", totalReqs)
	fmt.Printf("Total Errors:    %d (%.2f%%)\n", totalErrors, float64(totalErrors)*100/float64(totalReqs))
	fmt.Printf("Appends:         %d\n", totalAppends)
	fmt.Printf("Throughput:      %.0f req/sec\n", float64(totalReqs)/duration.Seconds())
	fmt.Printf("Avg Latency:     ~%.2f ms (estimated)\n", float64(duration.Milliseconds())/float64(totalReqs)*float64(numWorkers))
}

type result struct {
	requests int64
	errors   int64
	appends  int64
}

func worker(id int, wg *sync.WaitGroup, results chan<- result) {
	defer wg.Done()

	conn, err := grpc.Dial(targetURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Printf("Worker %d: connect failed: %v\n", id, err)
		results <- result{errors: opsPerWorker}
		return
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var r result
	streamID := fmt.Sprintf("stream-%d", id%10)

	for i := 0; i < opsPerWorker; i++ {
		select {
		case <-ctx.Done():
			results <- r
			return
		default:
		}

		// 80% append, 20% read
		if i%5 == 0 {
			// Read
			_, err := client.ReadStream(ctx, &pb.ReadStreamRequest{
				StreamId:    streamID,
				FromVersion: 0,
			})
			if err != nil {
				r.errors++
			} else {
				r.requests++
			}
		} else {
			// Append
			_, err := client.Append(ctx, &pb.AppendRequest{
				StreamId:        streamID,
				EventId:         fmt.Sprintf("evt-%d-%d", id, i),
				Data:            []byte(`{"load":"test"}`),
				ExpectedVersion: -1,
			})
			if err != nil {
				r.errors++
			} else {
				r.requests++
				r.appends++
			}
		}
	}

	results <- r
}
