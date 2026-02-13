package client

import "errors"

var (
	ErrPoolExhausted = errors.New("connection pool exhausted")
	ErrPoolClosed    = errors.New("connection pool is closed")
	ErrConnUnhealthy = errors.New("connection is unhealthy")
)
