package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	httpserver "github.com/hydra-db/hydra/server/http"
	"github.com/hydra-db/hydra/store"
)

func setupTestServer(t *testing.T) (*httpserver.Handlers, func()) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	handlers := httpserver.NewHandlers(s)

	cleanup := func() {
		s.Close()
	}

	return handlers, cleanup
}

func TestHealth(t *testing.T) {
	handlers, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handlers.Health(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp httpserver.HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp.Status)
	}
}

func TestAppendAndRead(t *testing.T) {
	handlers, cleanup := setupTestServer(t)
	defer cleanup()

	// Append event
	appendReq := httpserver.AppendRequest{
		EventID:         "test-event-1",
		Data:            []byte("hello"),
		ExpectedVersion: -1, // Any
	}
	body, _ := json.Marshal(appendReq)

	req := httptest.NewRequest(http.MethodPost, "/streams/alice", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handlers.AppendEvent(w, req, "alice")

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	// Read stream
	req = httptest.NewRequest(http.MethodGet, "/streams/alice", nil)
	w = httptest.NewRecorder()

	handlers.ReadStream(w, req, "alice")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Should have NDJSON content type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/x-ndjson" {
		t.Errorf("expected content-type 'application/x-ndjson', got '%s'", contentType)
	}
}

func TestAppendWrongVersion(t *testing.T) {
	handlers, cleanup := setupTestServer(t)
	defer cleanup()

	// First append
	appendReq := httpserver.AppendRequest{
		EventID:         "e1",
		Data:            []byte("first"),
		ExpectedVersion: 0, // NoStream
	}
	body, _ := json.Marshal(appendReq)

	req := httptest.NewRequest(http.MethodPost, "/streams/alice", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.AppendEvent(w, req, "alice")

	// Second append with wrong version
	appendReq = httpserver.AppendRequest{
		EventID:         "e2",
		Data:            []byte("second"),
		ExpectedVersion: 5, // Wrong version
	}
	body, _ = json.Marshal(appendReq)

	req = httptest.NewRequest(http.MethodPost, "/streams/alice", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handlers.AppendEvent(w, req, "alice")

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}
}
