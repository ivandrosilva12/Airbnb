package realtime_test

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/infrastructure/realtime"
	"github.com/google/uuid"
)

func TestHub_DeliversToSubscriberOnly(t *testing.T) {
	hub := realtime.NewHub()
	alice := uuid.New()
	bob := uuid.New()

	ch, cancel := hub.Subscribe(alice)
	defer cancel()

	hub.Publish(bob, `{"type":"notification"}`) // not for alice
	hub.Publish(alice, `{"type":"message"}`)

	select {
	case got := <-ch:
		if got != `{"type":"message"}` {
			t.Fatalf("payload = %q, want the message payload", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a delivered payload")
	}

	// Bob's payload must not leak into Alice's channel.
	select {
	case got := <-ch:
		t.Fatalf("unexpected extra payload %q", got)
	default:
	}
}

func TestHub_CancelRemovesSubscription(t *testing.T) {
	hub := realtime.NewHub()
	user := uuid.New()

	_, cancel := hub.Subscribe(user)
	if n := hub.Connections(user); n != 1 {
		t.Fatalf("connections = %d, want 1", n)
	}
	cancel()
	if n := hub.Connections(user); n != 0 {
		t.Fatalf("connections after cancel = %d, want 0", n)
	}
	// Publishing to a user with no live connections is a no-op (must not panic).
	hub.Publish(user, "ignored")
}

func TestHub_DropsWhenBufferFull(t *testing.T) {
	hub := realtime.NewHub()
	user := uuid.New()
	_, cancel := hub.Subscribe(user)
	defer cancel()

	// The buffer is 16; publishing far more must not block (slow/absent reader).
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hub.Publish(user, "x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full buffer")
	}
}
