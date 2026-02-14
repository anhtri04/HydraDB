// cmd/stress/http_stress.go
package main

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydra-db/hydra/pkg/client"
)

const (
	targetURL    = "http://localhost:8080"
	poolSize     = 20
	numWorkers   = 1000
	testDuration = 30 * time.Second
)

func main() {
	fmt.Println("=== HTTP Connection Pool Stress Test ===")
	fmt.Printf("Pool Size:    %d\n", poolSize)
	fmt.Printf("Workers:      %d\n", numWorkers)
	fmt.Printf("Duration:     %v\n\n", testDuration)

	pool, err := client.NewHTTPPool(targetURL, poolSize)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var totalRequests, totalErrors int64
	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, pool, &totalRequests, &totalErrors)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Println("\n========== RESULTS ==========")
	fmt.Printf("Duration:        %v\n", duration)
	fmt.Printf("Total Requests:  %d\n", totalRequests)
	fmt.Printf("Errors:          %d (%.4f%%)\n", totalErrors, float64(totalErrors)*100/float64(totalRequests))
	fmt.Printf("Throughput:      %.0f req/sec\n", float64(totalRequests)/duration.Seconds())
}

func worker(id int, pool *client.HTTPPool, totalReqs, totalErrors *int64) {
	client := pool.Get()
	if client == nil {
		atomic.AddInt64(totalErrors, 1)
		return
	}
	pool.Put(client)

	streamID := fmt.Sprintf("http-stream-%d", id%100)
	start := time.Now()

	for time.Since(start) < testDuration {
		req, _ := http.NewRequest("POST", targetURL+"/streams/"+streamID, nil)
		resp, err := pool.Do(req)

		atomic.AddInt64(totalReqs, 1)
		if err != nil {
			atomic.AddInt64(totalErrors, 1)
		} else if resp != nil {
			resp.Body.Close()
		}
	}
}
