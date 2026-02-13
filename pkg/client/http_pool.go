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

// Temporary stub for Close (will be implemented in Task 4)
func (p *HTTPPool) Close() error {
	p.closed = true
	close(p.clients)
	return nil
}
