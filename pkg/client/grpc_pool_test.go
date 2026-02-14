package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGRPCPool_CreatesCorrectNumberOfConnections(t *testing.T) {
	pool, err := NewGRPCPool("localhost:9090", 5)
	require.NoError(t, err)
	defer pool.Close()

	assert.Equal(t, 5, pool.size)
	assert.Equal(t, "localhost:9090", pool.target)
	assert.Equal(t, 5, len(pool.connections))
}

func TestGRPCPool_GetPut(t *testing.T) {
	pool, err := NewGRPCPool("localhost:9090", 2)
	require.NoError(t, err)
	defer pool.Close()

	conn1 := pool.Get()
	assert.NotNil(t, conn1)

	conn2 := pool.Get()
	assert.NotNil(t, conn2)

	pool.Put(conn1)
	pool.Put(conn2)
}

func TestGRPCPool_Close(t *testing.T) {
	pool, err := NewGRPCPool("localhost:9090", 2)
	require.NoError(t, err)

	err = pool.Close()
	assert.NoError(t, err)
	assert.True(t, pool.closed)

	err = pool.Close()
	assert.Error(t, err)
}
