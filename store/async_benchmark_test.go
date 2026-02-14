package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hydra-db/hydra/store"
)

func BenchmarkAppend_SyncEveryWrite(b *testing.B) {
	dir := b.TempDir()
	s, err := store.Open(dir, store.WithDurability(store.DefaultDurabilityConfig()))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), store.ExpectedVersionAny)
	}
}

func BenchmarkAppend_Async10ms(b *testing.B) {
	dir := b.TempDir()
	config := store.WithAsync(10*time.Millisecond, 1000)
	s, err := store.Open(dir, store.WithDurability(config))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), store.ExpectedVersionAny)
	}
}

func BenchmarkAppend_Async100ms(b *testing.B) {
	dir := b.TempDir()
	config := store.WithAsync(100*time.Millisecond, 10000)
	s, err := store.Open(dir, store.WithDurability(config))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), store.ExpectedVersionAny)
	}
}
