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
		PaymentAuthorized{}.EventName(),
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

// failingMarkStore wraps a memoryOutbox and always returns an error from
// MarkProcessed, so the relay sees the delivery as never having succeeded —
// the exact scenario where the S97 retry budget should eventually give up
// and dead-letter the record.
type failingMarkStore struct {
	*memoryOutbox
}

func (s *failingMarkStore) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	return errors.New("simulated downstream failure")
}

func TestDurablePublisher_DeadLettersAfterMaxAttempts(t *testing.T) {
	store := &failingMarkStore{memoryOutbox: &memoryOutbox{
		done:     map[uuid.UUID]bool{},
		attempts: map[uuid.UUID]int{},
		dlq:      map[uuid.UUID]DeadLetteredRecord{},
	}}
	d := NewDispatcher()
	d.Subscribe(func(_ context.Context, _ Event) {})
	var dlqCalls []string
	p := NewDurablePublisher(store, d).
		WithMaxAttempts(3).
		WithDLQObserver(func(name, _ string) { dlqCalls = append(dlqCalls, name) })

	ctx := context.Background()
	rec, _ := NewRecord(BookingRequested{BookingID: uuid.New()})
	if err := store.Append(ctx, rec); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Four Recover passes: attempts 1, 2, 3 deliver, attempt 4 promotes to DLQ.
	for i := 0; i < 4; i++ {
		if _, err := p.Recover(ctx, 10); err != nil {
			t.Fatalf("recover %d: %v", i, err)
		}
	}

	dl, _ := store.ListDeadLettered(ctx, 10, 0)
	if len(dl) != 1 {
		t.Fatalf("dead-lettered records = %d, want 1 (after %d failed attempts)", len(dl), 4)
	}
	wantName := BookingRequested{}.EventName()
	if len(dlqCalls) != 1 || dlqCalls[0] != wantName {
		t.Fatalf("dlq observer calls = %v, want one for %q", dlqCalls, wantName)
	}

	// Requeue + verify it leaves DLQ AND resets attempts.
	if err := store.Requeue(ctx, rec.ID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	dl, _ = store.ListDeadLettered(ctx, 10, 0)
	if len(dl) != 0 {
		t.Fatalf("after requeue dlq = %d, want 0", len(dl))
	}
}

// fakeLatencyMetric records every ObserveDispatchLatency call so the test can
// assert one observation fires per successful Recover dispatch (S154).
type fakeLatencyMetric struct {
	mu       sync.Mutex
	observed []float64
}

func (f *fakeLatencyMetric) ObserveDispatchLatency(seconds float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed = append(f.observed, seconds)
}

func (f *fakeLatencyMetric) snapshot() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]float64, len(f.observed))
	copy(out, f.observed)
	return out
}

// TestDurablePublisher_LatencyMetricObservedOnEachSuccessfulRecoverDispatch
// proves the S154 histogram fires once per record the recovery loop publishes,
// and that the observed value is non-negative (i.e. wall-clock since CreatedAt).
func TestDurablePublisher_LatencyMetricObservedOnEachSuccessfulRecoverDispatch(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	d.Subscribe(func(_ context.Context, _ Event) {})
	metric := &fakeLatencyMetric{}
	p := NewDurablePublisher(store, d).WithLatencyMetric(metric)

	ctx := context.Background()
	// Append three records — Recover should dispatch all three and the
	// metric should see one observation per record (not per cycle).
	for _, e := range []Event{
		BookingRequested{BookingID: uuid.New()},
		BookingConfirmed{BookingID: uuid.New()},
		BookingCompleted{BookingID: uuid.New()},
	} {
		r, _ := NewRecord(e)
		if err := store.Append(ctx, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	n, err := p.Recover(ctx, 10)
	if err != nil || n != 3 {
		t.Fatalf("recover = %d err = %v, want 3 nil", n, err)
	}
	got := metric.snapshot()
	if len(got) != 3 {
		t.Fatalf("latency observations = %d, want 3 (got: %v)", len(got), got)
	}
	for i, v := range got {
		if v < 0 {
			t.Fatalf("observation[%d] = %v, want >= 0 (wall-clock since CreatedAt)", i, v)
		}
	}

	// A second Recover with nothing left to deliver must not observe again.
	if _, err := p.Recover(ctx, 10); err != nil {
		t.Fatalf("second recover: %v", err)
	}
	if got2 := metric.snapshot(); len(got2) != 3 {
		t.Fatalf("observations after empty recover = %d, want still 3", len(got2))
	}
}

// TestDurablePublisher_LatencyMetricNilSafe proves a publisher without
// WithLatencyMetric still works — guarding against a nil sink is required
// because depth/DLQ observers are independent of the histogram (S154).
func TestDurablePublisher_LatencyMetricNilSafe(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	d.Subscribe(func(_ context.Context, _ Event) {})
	p := NewDurablePublisher(store, d) // no WithLatencyMetric

	ctx := context.Background()
	r, _ := NewRecord(BookingRequested{BookingID: uuid.New()})
	if err := store.Append(ctx, r); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := p.Recover(ctx, 10); err != nil {
		t.Fatalf("recover with nil metric: %v", err)
	}
}

func TestDurablePublisher_DepthObserverSamplesPending(t *testing.T) {
	store := NewMemoryOutbox()
	d := NewDispatcher()
	d.Subscribe(func(_ context.Context, _ Event) {})
	var lastDepth int
	p := NewDurablePublisher(store, d).
		WithMaxAttempts(5).
		WithDepthObserver(func(n int) { lastDepth = n })

	ctx := context.Background()
	// Two records — first will be delivered + marked processed; second sits
	// behind the 30-second grace window only enforced by the postgres impl,
	// so the in-memory FetchUnprocessed picks it up too.
	r1, _ := NewRecord(BookingRequested{BookingID: uuid.New()})
	r2, _ := NewRecord(BookingConfirmed{BookingID: uuid.New()})
	_ = store.Append(ctx, r1)
	_ = store.Append(ctx, r2)

	if _, err := p.Recover(ctx, 10); err != nil {
		t.Fatalf("recover: %v", err)
	}
	// Both delivered and marked processed → 0 pending.
	if lastDepth != 0 {
		t.Fatalf("post-recover depth = %d, want 0", lastDepth)
	}
}
