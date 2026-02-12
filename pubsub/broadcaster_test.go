package pubsub_test

import (
	"testing"
	"time"

	"github.com/hydra-db/hydra/pubsub"
)

func TestBroadcaster_SubscribeAndPublish(t *testing.T) {
	b := pubsub.NewBroadcaster()

	// Subscribe to all streams
	sub := b.Subscribe(nil, 10)
	defer b.Unsubscribe(sub.ID)

	// Publish an event
	event := pubsub.Event{
		GlobalPosition: 100,
		StreamID:       "alice",
		StreamVersion:  0,
		Data:           []byte("hello"),
	}
	b.Publish(event)

	// Should receive the event
	select {
	case received := <-sub.EventChan:
		if received.StreamID != "alice" {
			t.Errorf("expected streamID 'alice', got '%s'", received.StreamID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestBroadcaster_StreamFilter(t *testing.T) {
	b := pubsub.NewBroadcaster()

	// Subscribe only to "alice" stream
	aliceStream := "alice"
	sub := b.Subscribe(&aliceStream, 10)
	defer b.Unsubscribe(sub.ID)

	// Publish to bob - should not receive
	b.Publish(pubsub.Event{StreamID: "bob", Data: []byte("bob event")})

	// Publish to alice - should receive
	b.Publish(pubsub.Event{StreamID: "alice", Data: []byte("alice event")})

	// Should only receive alice event
	select {
	case received := <-sub.EventChan:
		if received.StreamID != "alice" {
			t.Errorf("expected 'alice', got '%s'", received.StreamID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for alice event")
	}

	// Channel should be empty (bob event was filtered)
	select {
	case <-sub.EventChan:
		t.Error("should not receive bob event")
	case <-time.After(50 * time.Millisecond):
		// Expected - no more events
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := pubsub.NewBroadcaster()

	sub1 := b.Subscribe(nil, 10)
	sub2 := b.Subscribe(nil, 10)
	defer b.Unsubscribe(sub1.ID)
	defer b.Unsubscribe(sub2.ID)

	if b.SubscriberCount() != 2 {
		t.Errorf("expected 2 subscribers, got %d", b.SubscriberCount())
	}

	// Publish event
	b.Publish(pubsub.Event{StreamID: "test", Data: []byte("data")})

	// Both should receive
	for i, sub := range []*pubsub.Subscriber{sub1, sub2} {
		select {
		case <-sub.EventChan:
			// Good
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d: timeout", i+1)
		}
	}
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	b := pubsub.NewBroadcaster()

	sub := b.Subscribe(nil, 10)
	if b.SubscriberCount() != 1 {
		t.Error("expected 1 subscriber")
	}

	b.Unsubscribe(sub.ID)
	if b.SubscriberCount() != 0 {
		t.Error("expected 0 subscribers after unsubscribe")
	}
}

func TestBroadcaster_SlowSubscriberDoesNotBlock(t *testing.T) {
	b := pubsub.NewBroadcaster()

	// Tiny buffer
	sub := b.Subscribe(nil, 1)
	defer b.Unsubscribe(sub.ID)

	// Fill the buffer
	b.Publish(pubsub.Event{StreamID: "test", Data: []byte("1")})

	// This should not block (event dropped)
	done := make(chan bool)
	go func() {
		b.Publish(pubsub.Event{StreamID: "test", Data: []byte("2")})
		b.Publish(pubsub.Event{StreamID: "test", Data: []byte("3")})
		done <- true
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("Publish blocked on slow subscriber")
	}
}
