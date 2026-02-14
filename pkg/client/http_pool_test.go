package client

import (
	"net/http"
	"time"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPPool_CreatesCorrectNumberOfClients(t *testing.T) {
	pool, err := NewHTTPPool("http://localhost:8080", 5)
	require.NoError(t, err)
	defer pool.Close()

	assert.Equal(t, 5, pool.size)
	assert.Equal(t, "http://localhost:8080", pool.baseURL)
	assert.Equal(t, 5, len(pool.clients))
}

func TestNewHTTPPool_InvalidSize(t *testing.T) {
	pool, err := NewHTTPPool("http://localhost:8080", 0)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestHTTPPool_GetPut(t *testing.T) {
	pool, err := NewHTTPPool("http://localhost:8080", 2)
	require.NoError(t, err)
	defer pool.Close()

	// Get a client
	client1 := pool.Get()
	assert.NotNil(t, client1)
	assert.Equal(t, 1, len(pool.clients)) // One remaining

	// Get another
	client2 := pool.Get()
	assert.NotNil(t, client2)
	assert.Equal(t, 0, len(pool.clients)) // None remaining

	// Put them back
	pool.Put(client1)
	assert.Equal(t, 1, len(pool.clients))

	pool.Put(client2)
	assert.Equal(t, 2, len(pool.clients))
}

func TestHTTPPool_GetFromClosedPool(t *testing.T) {
	pool, err := NewHTTPPool("http://localhost:8080", 2)
	require.NoError(t, err)

	pool.Close()

	client := pool.Get()
	assert.Nil(t, client)
}

func TestHTTPPool_Close(t *testing.T) {
	pool, err := NewHTTPPool("http://localhost:8080", 2)
	require.NoError(t, err)

	// Close should not panic
	err = pool.Close()
	assert.NoError(t, err)
	assert.True(t, pool.closed)

	// Second close should return error
	err = pool.Close()
	assert.Error(t, err)
}

func TestHTTPPool_Do(t *testing.T) {
	// This test requires a running server
	// Skip if server not available
	client := &http.Client{Timeout: 1 * time.Second}
	_, err := client.Get("http://localhost:8080/health")
	if err != nil {
		t.Skip("Server not available:", err)
	}

	pool, err := NewHTTPPool("http://localhost:8080", 2)
	require.NoError(t, err)
	defer pool.Close()

	req, err := http.NewRequest("GET", "http://localhost:8080/health", nil)
	require.NoError(t, err)

	resp, err := pool.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
