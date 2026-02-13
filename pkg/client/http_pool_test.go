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
