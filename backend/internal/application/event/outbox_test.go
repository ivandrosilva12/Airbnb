package event

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestDurablePublisher_RecordsDispatchesAndMarksProcessed(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	var got int
	d.Subscribe(func(_ context.Context, _ Event) { got++ })

	p := NewDurablePublisher(store, d)
	p.Publish(context.Background(), MessageSent{ConversationID: uuid.New(), SenderID: uuid.New(), RecipientID: uuid.New()})

	if got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	// The happy path marks the record processed, so nothing is left to recover.
	left, _ := store.FetchUnprocessed(context.Background(), 10)
	if len(left) != 0 {
		t.Fatalf("unprocessed = %d, want 0", len(left))
	}
}

// failOnceMarkStore drops the first MarkProcessed call to simulate a crash
// between dispatch and marking, leaving a record for recovery to re-deliver.
type failOnceMarkStore struct {
	OutboxStore
	mu     sync.Mutex
	failed bool
}

func (s *failOnceMarkStore) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.failed {
		s.failed = true
		return errors.New("simulated crash before mark")
	}
	return s.OutboxStore.MarkProcessed(ctx, id)
}

func TestDurablePublisher_RecoversUnprocessed(t *testing.T) {
	store := &failOnceMarkStore{OutboxStore: NewMemoryOutbox()}
	d := NewDispatcher()
	var delivered int
	d.Subscribe(func(_ context.Context, _ Event) { delivered++ })
	p := NewDurablePublisher(store, d)

	// Publish: dispatched once, but the mark failed, so it stays unprocessed.
	p.Publish(context.Background(), BookingConfirmed{BookingID: uuid.New(), PropertyTitle: "Loft", GuestID: uuid.New()})
	if delivered != 1 {
		t.Fatalf("initial delivery = %d, want 1", delivered)
	}
	left, _ := store.FetchUnprocessed(context.Background(), 10)
	if len(left) != 1 {
		t.Fatalf("unprocessed after failed mark = %d, want 1", len(left))
	}

	// Recovery re-delivers it (at-least-once) and now marks it processed.
	n, err := p.Recover(context.Background(), 10)
	if err != nil || n != 1 {
		t.Fatalf("recover = %d err = %v, want 1 nil", n, err)
	}
	if delivered != 2 {
		t.Fatalf("delivery after recover = %d, want 2", delivered)
	}
	left, _ = store.FetchUnprocessed(context.Background(), 10)
	if len(left) != 0 {
		t.Fatalf("unprocessed after recover = %d, want 0", len(left))
	}
}

func TestDurablePublisher_DecoderRegisteredForAllEvents(t *testing.T) {
	// Every event the app publishes must be reconstructable for recovery.
	for _, name := range []string{
		BookingRequested{}.EventName(),
		BookingConfirmed{}.EventName(),
		BookingCancelled{}.EventName(),
		BookingCompleted{}.EventName(),
		BookingModified{}.EventName(),
		MessageSent{}.EventName(),
		IdentityVerified{}.EventName(),
	} {
		if _, ok := decoders[name]; !ok {
			t.Fatalf("no decoder registered for %q", name)
		}
	}
}
