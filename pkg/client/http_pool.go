package client

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

type HTTPPool struct {
	clients chan *http.Client
	baseURL string
	size    int
	closed  bool
}

func NewHTTPPool(baseURL string, size int) (*HTTPPool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("pool size must be positive, got %d", size)
	}

	pool := &HTTPPool{
		clients: make(chan *http.Client, size),
		baseURL: baseURL,
		size:    size,
		closed:  false,
	}

	transport := &http.Transport{
		MaxIdleConns:        size,
		MaxIdleConnsPerHost: size,
		MaxConnsPerHost:     size,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	for i := 0; i < size; i++ {
		client := &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
		pool.clients <- client
	}

	return pool, nil
}

// Get retrieves an HTTP client from the pool
// Blocks until a client is available or pool is closed
func (p *HTTPPool) Get() *http.Client {
	if p.closed {
		return nil
	}

	client, ok := <-p.clients
	if !ok {
		return nil // Pool is closed
	}

	return client
}

// Put returns an HTTP client to the pool
func (p *HTTPPool) Put(client *http.Client) {
	if p.closed || client == nil {
		return
	}

	// Non-blocking send - if pool is full, drop the client
	select {
	case p.clients <- client:
		// Returned to pool
	default:
		// Pool is full (shouldn't happen), close this client
	}
}

// Temporary stub for Close (will be implemented in Task 4)
func (p *HTTPPool) Close() error {
	p.closed = true
	close(p.clients)
	return nil
}
