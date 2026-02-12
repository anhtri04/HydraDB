package log_test

import (
	"path/filepath"
	"testing"

	"github.com/hydra-db/hydra/log"
)

func TestAppend_ReturnsPositionZeroForFirstRecord(t *testing.T) {
	// Setup: create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Open log
	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Append a record
	data := []byte("hello")
	pos, err := l.Append(data)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// First record should be at position 0
	if pos != 0 {
		t.Errorf("expected position 0, got %d", pos)
	}
}
