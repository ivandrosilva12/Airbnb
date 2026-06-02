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
	// SplitPaymentCompleted is in the list because S31 routes it through the
	// transactional outbox — a missing decoder would silently strand any
	// completion event whose dispatch was interrupted by a crash.
	for _, name := range []string{
		BookingRequested{}.EventName(),
		BookingConfirmed{}.EventName(),
		BookingCancelled{}.EventName(),
		BookingCompleted{}.EventName(),
		BookingModified{}.EventName(),
		MessageSent{}.EventName(),
		IdentityVerified{}.EventName(),
		SplitPaymentCompleted{}.EventName(),
	} {
		if _, ok := decoders[name]; !ok {
			t.Fatalf("no decoder registered for %q", name)
		}
	}
}

// TestDurablePublisher_DispatchRecordsInterrupted_RecoveredOnNextStartup
// simulates the UoW path: a transaction appends to the outbox and commits,
// but the process crashes before DispatchRecords runs (or before it
// completes for some records). The next Recover() must re-deliver every
// unprocessed record. This is the at-least-once guarantee S31 / S35 lean
// on; without it a SplitPaymentCompleted event could be lost between the
// tx commit and the in-process dispatch, leaving the booking stuck pending.
func TestDurablePublisher_DispatchRecordsInterrupted_RecoveredOnNextStartup(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	var got []string
	d.Subscribe(func(_ context.Context, e Event) { got = append(got, e.EventName()) })
	p := NewDurablePublisher(store, d)

	// Simulate a transaction committing two events into the outbox, then a
	// crash before DispatchRecords ever runs. The RecordingOutbox captures
	// exactly the records that an in-flight tx would have appended.
	ctx := context.Background()
	recording := NewRecordingOutbox(store)
	rec1, _ := NewRecord(BookingRequested{BookingID: uuid.New(), PropertyTitle: "A"})
	rec2, _ := NewRecord(SplitPaymentCompleted{SplitPaymentID: uuid.New(), BookingID: uuid.New()})
	if err := recording.Append(ctx, rec1); err != nil {
		t.Fatalf("append rec1: %v", err)
	}
	if err := recording.Append(ctx, rec2); err != nil {
		t.Fatalf("append rec2: %v", err)
	}
	// Crash: DispatchRecords never called. The two records sit unprocessed.
	if len(got) != 0 {
		t.Fatalf("handler called before recovery: got %v", got)
	}
	left, _ := store.FetchUnprocessed(ctx, 10)
	if len(left) != 2 {
		t.Fatalf("unprocessed after simulated crash = %d, want 2", len(left))
	}

	// Startup: Recover re-delivers everything and marks each processed.
	n, err := p.Recover(ctx, 10)
	if err != nil || n != 2 {
		t.Fatalf("recover = %d err = %v, want 2 nil", n, err)
	}
	if len(got) != 2 {
		t.Fatalf("delivery after recover = %d, want 2 (got names: %v)", len(got), got)
	}
	left, _ = store.FetchUnprocessed(ctx, 10)
	if len(left) != 0 {
		t.Fatalf("unprocessed after recover = %d, want 0", len(left))
	}
}

// TestDurablePublisher_RecoverPreservesAppendOrder confirms recovery
// delivers events in the order they were appended. The instant-book flow
// relies on this: BookingRequested must arrive at the payment subscriber
// before BookingConfirmed so the authorize happens before the capture.
// If Recover delivered them backwards, capture would race against an
// authorize that hasn't landed yet.
func TestDurablePublisher_RecoverPreservesAppendOrder(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	var order []string
	d.Subscribe(func(_ context.Context, e Event) { order = append(order, e.EventName()) })
	p := NewDurablePublisher(store, d)

	ctx := context.Background()
	// Append three events in a specific order, then recover.
	recs := []Event{
		BookingRequested{BookingID: uuid.New()},
		BookingConfirmed{BookingID: uuid.New()},
		BookingCompleted{BookingID: uuid.New()},
	}
	for _, e := range recs {
		r, _ := NewRecord(e)
		if err := store.Append(ctx, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	n, err := p.Recover(ctx, 10)
	if err != nil || n != 3 {
		t.Fatalf("recover = %d err = %v, want 3 nil", n, err)
	}
	want := []string{
		BookingRequested{}.EventName(),
		BookingConfirmed{}.EventName(),
		BookingCompleted{}.EventName(),
	}
	if len(order) != len(want) {
		t.Fatalf("delivery count = %d, want %d (got: %v)", len(order), len(want), order)
	}
	for i, name := range want {
		if order[i] != name {
			t.Fatalf("delivery[%d] = %q, want %q (full order: %v)", i, order[i], name, order)
		}
	}
}
