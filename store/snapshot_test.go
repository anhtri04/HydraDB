package store_test

import (
	"testing"
	"time"

	"github.com/hydra-db/hydra/store"
)

func TestSnapshotStore_SaveAndLoad(t *testing.T) {
	ss := store.NewSnapshotStore(nil)

	// Save snapshot
	ss.Save("user-1", 100, []byte(`{"balance": 500}`))

	// Load snapshot
	snap := ss.Load("user-1")
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.Version != 100 {
		t.Errorf("expected version 100, got %d", snap.Version)
	}
	if string(snap.State) != `{"balance": 500}` {
		t.Errorf("wrong state: %s", snap.State)
	}
}

func TestSnapshotStore_LoadNonExistent(t *testing.T) {
	ss := store.NewSnapshotStore(nil)

	snap := ss.Load("non-existent")
	if snap != nil {
		t.Error("expected nil for non-existent stream")
	}
}

func TestSnapshotStore_OverwriteSnapshot(t *testing.T) {
	ss := store.NewSnapshotStore(nil)

	ss.Save("user-1", 100, []byte("old"))
	ss.Save("user-1", 200, []byte("new"))

	snap := ss.Load("user-1")
	if snap.Version != 200 {
		t.Error("snapshot should be overwritten")
	}
	if string(snap.State) != "new" {
		t.Error("snapshot state should be updated")
	}
}

func TestSnapshotStore_Delete(t *testing.T) {
	ss := store.NewSnapshotStore(nil)

	ss.Save("user-1", 100, []byte("data"))
	ss.Delete("user-1")

	snap := ss.Load("user-1")
	if snap != nil {
		t.Error("snapshot should be deleted")
	}
}

func TestSnapshotStore_Count(t *testing.T) {
	ss := store.NewSnapshotStore(nil)

	if ss.Count() != 0 {
		t.Error("expected 0 snapshots initially")
	}

	ss.Save("user-1", 100, []byte("data"))
	ss.Save("user-2", 100, []byte("data"))

	if ss.Count() != 2 {
		t.Errorf("expected 2 snapshots, got %d", ss.Count())
	}
}

func TestSnapshotStrategy_EveryNEvents(t *testing.T) {
	strategy := &store.EveryNEvents{N: 10}

	// Should not snapshot at version 9
	if strategy.ShouldSnapshot("user-1", 9, "") {
		t.Error("should not snapshot at version 9")
	}
	// Should snapshot at version 10
	if !strategy.ShouldSnapshot("user-1", 10, "") {
		t.Error("should snapshot at version 10")
	}
	// Should snapshot at version 20
	if !strategy.ShouldSnapshot("user-1", 20, "") {
		t.Error("should snapshot at version 20")
	}
	// Should not snapshot at version 0
	if strategy.ShouldSnapshot("user-1", 0, "") {
		t.Error("should not snapshot at version 0")
	}
}

func TestSnapshotStrategy_OnEventTypes(t *testing.T) {
	strategy := store.NewOnEventTypes("OrderCompleted", "PaymentReceived")

	if strategy.ShouldSnapshot("order-1", 1, "OrderCreated") {
		t.Error("should not snapshot for OrderCreated")
	}
	if !strategy.ShouldSnapshot("order-1", 2, "OrderCompleted") {
		t.Error("should snapshot for OrderCompleted")
	}
	if !strategy.ShouldSnapshot("order-1", 3, "PaymentReceived") {
		t.Error("should snapshot for PaymentReceived")
	}
}

func TestSnapshotStrategy_TimeBased(t *testing.T) {
	strategy := store.NewTimeBased(50 * time.Millisecond)

	// First call should trigger snapshot
	if !strategy.ShouldSnapshot("user-1", 1, "") {
		t.Error("first call should trigger snapshot")
	}

	// Immediate second call should not
	if strategy.ShouldSnapshot("user-1", 2, "") {
		t.Error("immediate second call should not trigger")
	}

	// After interval, should trigger again
	time.Sleep(60 * time.Millisecond)
	if !strategy.ShouldSnapshot("user-1", 3, "") {
		t.Error("should trigger after interval")
	}
}

func TestSnapshotStrategy_AfterEachEvent(t *testing.T) {
	strategy := &store.AfterEachEvent{}

	if !strategy.ShouldSnapshot("user-1", 1, "") {
		t.Error("AfterEachEvent should always return true")
	}
	if !strategy.ShouldSnapshot("user-1", 100, "") {
		t.Error("AfterEachEvent should always return true")
	}
}

func TestSnapshotStrategy_Never(t *testing.T) {
	strategy := &store.Never{}

	if strategy.ShouldSnapshot("user-1", 1, "") {
		t.Error("Never should always return false")
	}
	if strategy.ShouldSnapshot("user-1", 100, "Critical") {
		t.Error("Never should always return false")
	}
}

func TestSnapshotStrategy_Composite(t *testing.T) {
	strategy := &store.Composite{
		Strategies: []store.SnapshotStrategy{
			&store.EveryNEvents{N: 100},
			store.NewOnEventTypes("Critical"),
		},
	}

	// Should trigger on event type
	if !strategy.ShouldSnapshot("user-1", 5, "Critical") {
		t.Error("should trigger for Critical event")
	}

	// Should trigger on every 100
	if !strategy.ShouldSnapshot("user-1", 100, "Normal") {
		t.Error("should trigger at version 100")
	}

	// Should not trigger otherwise
	if strategy.ShouldSnapshot("user-1", 50, "Normal") {
		t.Error("should not trigger at version 50 with Normal event")
	}
}

func TestSnapshotStore_WithStrategy(t *testing.T) {
	strategy := &store.EveryNEvents{N: 5}
	ss := store.NewSnapshotStore(strategy)

	// Should delegate to strategy
	if ss.ShouldSnapshot("user-1", 4, "") {
		t.Error("should not snapshot at version 4")
	}
	if !ss.ShouldSnapshot("user-1", 5, "") {
		t.Error("should snapshot at version 5")
	}
}
