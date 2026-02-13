package client

import (
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
