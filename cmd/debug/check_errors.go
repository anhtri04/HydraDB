package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/hydra-db/hydra/server/grpc/proto"
)

const targetURL = "localhost:9090"

func main() {
	conn, err := grpc.Dial(targetURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := pb.NewEventStoreClient(conn)

	var wg sync.WaitGroup
	errorCounts := make(map[string]int)
	var mu sync.Mutex

	// 100 workers doing 50 requests each
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				_, err := client.Append(ctx, &pb.AppendRequest{
					StreamId:        fmt.Sprintf("stream-%d", id%10),
					EventId:         fmt.Sprintf("evt-%d-%d", id, j),
					Data:            []byte(`{"test":"data"}`),
					ExpectedVersion: -1,
				})
				cancel()

				if err != nil {
					st, _ := status.FromError(err)
					mu.Lock()
					errorCounts[st.Code().String()+": "+st.Message()]++
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	fmt.Println("Error breakdown:")
	for msg, count := range errorCounts {
		fmt.Printf("  %s: %d\n", msg, count)
	}
	if len(errorCounts) == 0 {
		fmt.Println("  No errors!")
	}
}
