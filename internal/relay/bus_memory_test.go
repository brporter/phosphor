package relay

import (
	"context"
	"testing"
	"time"
)

func TestMemoryMessageBus_PublishSubscribe(t *testing.T) {
	bus := NewMemoryMessageBus()
	ctx := context.Background()

	ch, unsub, err := bus.Subscribe(ctx, "test-channel")
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	defer unsub()

	msg := []byte("hello world")
	if err := bus.Publish(ctx, "test-channel", msg); err != nil {
		t.Fatalf("Publish error: %v", err)
	}

	select {
	case got := <-ch:
		if string(got) != string(msg) {
			t.Errorf("received %q, want %q", got, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestMemoryMessageBus_MultipleSubscribers(t *testing.T) {
	bus := NewMemoryMessageBus()
	ctx := context.Background()

	ch1, unsub1, _ := bus.Subscribe(ctx, "broadcast")
	defer unsub1()
	ch2, unsub2, _ := bus.Subscribe(ctx, "broadcast")
	defer unsub2()

	msg := []byte("to all")
	bus.Publish(ctx, "broadcast", msg)

	for i, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != "to all" {
				t.Errorf("subscriber %d received %q, want %q", i, got, msg)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d timed out", i)
		}
	}
}

func TestMemoryMessageBus_Unsubscribe(t *testing.T) {
	bus := NewMemoryMessageBus()
	ctx := context.Background()

	ch, unsub, _ := bus.Subscribe(ctx, "ephemeral")

	// Unsubscribe
	unsub()

	// Publish after unsubscribe — channel should be closed
	bus.Publish(ctx, "ephemeral", []byte("ghost"))

	// Channel should be closed (drained to empty, then closed)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received message after unsubscribe, expected closed channel")
		}
	case <-time.After(500 * time.Millisecond):
		// Channel closed, reads would return zero value immediately
	}
}

func TestMemoryMessageBus_DifferentChannels(t *testing.T) {
	bus := NewMemoryMessageBus()
	ctx := context.Background()

	chA, unsubA, _ := bus.Subscribe(ctx, "channel-a")
	defer unsubA()
	chB, unsubB, _ := bus.Subscribe(ctx, "channel-b")
	defer unsubB()

	bus.Publish(ctx, "channel-a", []byte("for A"))

	select {
	case got := <-chA:
		if string(got) != "for A" {
			t.Errorf("channel-a received %q, want %q", got, "for A")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel-a timed out")
	}

	// channel-b should not have received anything
	select {
	case msg := <-chB:
		t.Errorf("channel-b received unexpected message: %q", msg)
	default:
		// expected — no message on channel-b
	}
}

func TestMemoryMessageBus_DoubleUnsubscribe(t *testing.T) {
	bus := NewMemoryMessageBus()
	ctx := context.Background()

	_, unsub, _ := bus.Subscribe(ctx, "test")

	// Should not panic
	unsub()
	unsub()
}
