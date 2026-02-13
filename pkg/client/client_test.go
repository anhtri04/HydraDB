package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHydraClient(t *testing.T) {
	client, err := NewHydraClient("http://localhost:8080", "localhost:9090")
	require.NoError(t, err)
	defer client.Close()

	assert.NotNil(t, client.HTTP)
	assert.NotNil(t, client.GRPC)
}
