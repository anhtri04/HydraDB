package client

import "net/http"

type HTTPPool struct {
	clients chan *http.Client
	baseURL string
	size    int
	closed  bool
}
