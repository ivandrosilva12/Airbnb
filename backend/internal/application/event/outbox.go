package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Record is a persisted event awaiting (or having completed) delivery.
type Record struct {
	ID        uuid.UUID
	Name      string
	Payload   []byte
	CreatedAt time.Time
	Attempts  int
}

// OutboxStore persists events durably so they survive a crash between being
// recorded and being delivered. Append writes a new record; FetchUnprocessed
// returns records not yet marked processed (oldest first); MarkProcessed and
// MarkFailed close out a delivery attempt.
type OutboxStore interface {
	Append(ctx context.Context, r Record) error
	FetchUnprocessed(ctx context.Context, limit int) ([]Record, error)
	MarkProcessed(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID) error
}

// DeadLetterStore is an optional extension to OutboxStore implemented by stores
// that support a dead-letter state (S97 — WF-GAP-018). Records that exceed the
// retry budget are promoted to DLQ rather than retried forever; admins can list
// them via ListDeadLettered and pull them back to the live queue with Requeue.
//
// IncrementAttempt is called by the relay before each delivery so the budget is
// charged whether the subscriber returned an error or the process crashed.
// PendingCount feeds the depth metric.
//
// DurablePublisher type-asserts its store to this interface; if absent (e.g.
// the memory outbox in older tests), behaviour falls back to the original
// MarkFailed-once-and-stop semantics.
type DeadLetterStore interface {
	IncrementAttempt(ctx context.Context, id uuid.UUID) (int, error)
	DeadLetter(ctx context.Context, id uuid.UUID, reason string) error
	Requeue(ctx context.Context, id uuid.UUID) error
	ListDeadLettered(ctx context.Context, limit, offset int) ([]DeadLetteredRecord, error)
	PendingCount(ctx context.Context) (int, error)
}

// DeadLetteredRecord is a Record plus the columns that only matter once it has
// been dead-lettered. Used by the admin listing endpoint.
type DeadLetteredRecord struct {
	Record
	DeadLetteredAt time.Time
	LastError      string
}

// DepthObserver receives the pending-count gauge sample on every relay cycle.
// Wired in main.go to the Prometheus gauge; tests inject a recorder.
type DepthObserver func(count int)

// DLQObserver is called once per record promotion to the dead-letter state.
// Wired in main.go to a Prometheus CounterVec labeled by event name.
type DLQObserver func(eventName string, reason string)

// --- event (de)serialization registry --------------------------------------

var decoders = map[string]func([]byte) (Event, error){}

// Register associates an event name with a decoder so persisted records can be
// reconstructed into their concrete type for re-delivery. Call from init().
func Register(name string, decode func([]byte) (Event, error)) {
	decoders[name] = decode
}

// decode reconstructs an event from a record; unknown names are an error.
func decode(name string, payload []byte) (Event, error) {
	d, ok := decoders[name]
	if !ok {
		return nil, fmt.Errorf("event: no decoder registered for %q", name)
	}
	return d(payload)
}

// jsonDecoder builds a decoder that unmarshals JSON into *T and returns it.
func jsonDecoder[T Event]() func([]byte) (Event, error) {
	return func(b []byte) (Event, error) {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// NewRecord builds an outbox record from an event (JSON payload + a fresh id).
// Services append the record inside their write transaction so the event is
// persisted atomically with the domain change.
func NewRecord(e Event) (Record, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: uuid.New(), Name: e.EventName(), Payload: payload, CreatedAt: time.Now().UTC()}, nil
}

// --- durable publisher -------------------------------------------------------

// DurablePublisher records each event in an OutboxStore before fanning it out
// through the in-process Dispatcher, then marks it processed. If the process
// dies between Append and MarkProcessed, Recover re-delivers the record on the
// next startup (at-least-once; subscribers should tolerate a rare duplicate).
//
// Note: Append is not yet in the same DB transaction as the originating domain
// write, so the (small, in-process) window between the write committing and
// Append is not covered; closing it requires threading a unit-of-work through
// the repositories.
type DurablePublisher struct {
	store    OutboxStore
	dispatch *Dispatcher
	// maxAttempts is the per-record budget enforced during Recover (S97).
	// Once attempts >= maxAttempts the record is moved to dead-letter state
	// instead of being retried again. Zero means "unlimited" — the pre-S97
	// behaviour, kept for tests that don't care.
	maxAttempts int
	depthObs    DepthObserver
	dlqObs      DLQObserver
}

// NewDurablePublisher wraps a Dispatcher with outbox-backed durability.
func NewDurablePublisher(store OutboxStore, dispatch *Dispatcher) *DurablePublisher {
	return &DurablePublisher{store: store, dispatch: dispatch}
}

// WithMaxAttempts caps the number of delivery attempts before a record is
// promoted to dead-letter (S97 — WF-GAP-018). 0 disables the cap, matching the
// pre-S97 behaviour where MarkFailed was the only terminal state.
func (p *DurablePublisher) WithMaxAttempts(n int) *DurablePublisher {
	p.maxAttempts = n
	return p
}

// WithDepthObserver registers a callback the relay invokes with the pending
// count on each Recover cycle. Used to feed the Prometheus gauge.
func (p *DurablePublisher) WithDepthObserver(obs DepthObserver) *DurablePublisher {
	p.depthObs = obs
	return p
}

// WithDLQObserver registers a callback the relay invokes whenever a record is
// dead-lettered, labelled by event name and reason. Used to feed a counter.
func (p *DurablePublisher) WithDLQObserver(obs DLQObserver) *DurablePublisher {
	p.dlqObs = obs
	return p
}

// Publish records the event, fans it out synchronously (so callers and tests
// observe side effects immediately), then marks it processed.
func (p *DurablePublisher) Publish(ctx context.Context, e Event) {
	payload, err := json.Marshal(e)
	if err != nil {
		slog.Error("outbox: marshal failed; delivering without durability", "event", e.EventName(), "error", err)
		p.dispatch.Publish(ctx, e)
		return
	}
	rec := Record{ID: uuid.New(), Name: e.EventName(), Payload: payload, CreatedAt: time.Now().UTC()}
	if err := p.store.Append(ctx, rec); err != nil {
		slog.Error("outbox: append failed; delivering without durability", "event", e.EventName(), "error", err)
		p.dispatch.Publish(ctx, e)
		return
	}
	p.dispatch.Publish(ctx, e)
	if err := p.store.MarkProcessed(ctx, rec.ID); err != nil {
		slog.Warn("outbox: mark-processed failed; will be re-delivered on recovery", "event", e.EventName(), "error", err)
	}
}

// Recover re-delivers any records left unprocessed (e.g. by a crash mid-
// dispatch), reconstructing each event from its payload. Safe to call at
// startup and periodically.
//
// S97: when the store implements DeadLetterStore AND maxAttempts is set, each
// visit charges one attempt up-front; a record whose attempt count reaches
// the budget is moved to dead-letter and skipped. Undecodable payloads go
// straight to dead-letter rather than looping. After the pass, the depth
// observer (if set) is called with the post-Recover pending count for the
// Prometheus gauge.
func (p *DurablePublisher) Recover(ctx context.Context, limit int) (int, error) {
	records, err := p.store.FetchUnprocessed(ctx, limit)
	if err != nil {
		return 0, err
	}
	dlq, hasDLQ := p.store.(DeadLetterStore)
	var delivered int
	for _, r := range records {
		// Charge an attempt up-front so a panicking subscriber the dispatcher
		// recovers from still burns budget. If the post-increment count meets
		// or exceeds the cap, promote to DLQ and stop trying this record.
		if hasDLQ && p.maxAttempts > 0 {
			attempts, err := dlq.IncrementAttempt(ctx, r.ID)
			if err != nil {
				slog.Warn("outbox: increment-attempt failed; falling back to delivery", "id", r.ID, "error", err)
			} else if attempts > p.maxAttempts {
				reason := fmt.Sprintf("exceeded max attempts (%d)", p.maxAttempts)
				if err := dlq.DeadLetter(ctx, r.ID, reason); err != nil {
					slog.Error("outbox: dead-letter failed", "id", r.ID, "error", err)
					continue
				}
				if p.dlqObs != nil {
					p.dlqObs(r.Name, reason)
				}
				slog.Warn("outbox: record dead-lettered", "event", r.Name, "id", r.ID, "attempts", attempts)
				continue
			}
		}
		e, err := decode(r.Name, r.Payload)
		if err != nil {
			// Undecodable payloads short-circuit to DLQ when available;
			// otherwise fall back to the old MarkFailed terminal state.
			slog.Error("outbox: cannot decode record", "event", r.Name, "id", r.ID, "error", err)
			if hasDLQ {
				if dlErr := dlq.DeadLetter(ctx, r.ID, "undecodable: "+err.Error()); dlErr == nil && p.dlqObs != nil {
					p.dlqObs(r.Name, "undecodable")
				}
			} else {
				_ = p.store.MarkFailed(ctx, r.ID)
			}
			continue
		}
		p.dispatch.Publish(ctx, e)
		if err := p.store.MarkProcessed(ctx, r.ID); err != nil {
			slog.Warn("outbox: mark-processed failed during recovery", "id", r.ID, "error", err)
			continue
		}
		delivered++
	}
	// Sample the post-pass depth so the gauge reflects what's left to drain.
	if hasDLQ && p.depthObs != nil {
		if pending, err := dlq.PendingCount(ctx); err == nil {
			p.depthObs(pending)
		}
	}
	return delivered, nil
}

// DispatchRecords delivers a known set of just-committed records — exactly the
// events appended inside one transaction — fanning each out once and marking it
// processed. Unlike Recover, it does not scan the whole table, so concurrent
// transactions never re-dispatch each other's events. Anything whose
// mark-processed fails is picked up later by the recovery relay (at-least-once).
func (p *DurablePublisher) DispatchRecords(ctx context.Context, records []Record) {
	for _, r := range records {
		e, err := decode(r.Name, r.Payload)
		if err != nil {
			slog.Error("outbox: cannot decode record; marking failed", "event", r.Name, "id", r.ID, "error", err)
			_ = p.store.MarkFailed(ctx, r.ID)
			continue
		}
		p.dispatch.Publish(ctx, e)
		if err := p.store.MarkProcessed(ctx, r.ID); err != nil {
			slog.Warn("outbox: mark-processed failed; will be re-delivered on recovery", "id", r.ID, "error", err)
		}
	}
}

var _ Publisher = (*DurablePublisher)(nil)

// RecordingOutbox wraps an OutboxStore and remembers the records appended
// through it, so a unit of work can dispatch exactly those after the
// transaction commits (see DurablePublisher.DispatchRecords).
type RecordingOutbox struct {
	inner    OutboxStore
	recorded []Record
}

// NewRecordingOutbox wraps store to capture appended records.
func NewRecordingOutbox(store OutboxStore) *RecordingOutbox {
	return &RecordingOutbox{inner: store}
}

func (r *RecordingOutbox) Append(ctx context.Context, rec Record) error {
	if err := r.inner.Append(ctx, rec); err != nil {
		return err
	}
	r.recorded = append(r.recorded, rec)
	return nil
}

func (r *RecordingOutbox) FetchUnprocessed(ctx context.Context, limit int) ([]Record, error) {
	return r.inner.FetchUnprocessed(ctx, limit)
}
func (r *RecordingOutbox) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	return r.inner.MarkProcessed(ctx, id)
}
func (r *RecordingOutbox) MarkFailed(ctx context.Context, id uuid.UUID) error {
	return r.inner.MarkFailed(ctx, id)
}

// Recorded returns the records appended through this wrapper.
func (r *RecordingOutbox) Recorded() []Record { return r.recorded }

var _ OutboxStore = (*RecordingOutbox)(nil)

// memoryOutbox is a process-local OutboxStore used in tests and as a fallback.
// Implements DeadLetterStore so tests can exercise the DLQ semantics without
// standing up Postgres.
type memoryOutbox struct {
	mu       sync.Mutex
	records  []Record
	done     map[uuid.UUID]bool
	attempts map[uuid.UUID]int
	dlq      map[uuid.UUID]DeadLetteredRecord
}

// NewMemoryOutbox returns an in-memory OutboxStore.
func NewMemoryOutbox() OutboxStore {
	return &memoryOutbox{
		done:     map[uuid.UUID]bool{},
		attempts: map[uuid.UUID]int{},
		dlq:      map[uuid.UUID]DeadLetteredRecord{},
	}
}

func (m *memoryOutbox) Append(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, r)
	return nil
}

func (m *memoryOutbox) FetchUnprocessed(_ context.Context, limit int) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.records {
		if m.done[r.ID] {
			continue
		}
		if _, dl := m.dlq[r.ID]; dl {
			// Dead-lettered records are excluded from the recovery scan.
			continue
		}
		// Reflect current attempt count so the caller sees up-to-date data.
		r.Attempts = m.attempts[r.ID]
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *memoryOutbox) MarkProcessed(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done[id] = true
	return nil
}

func (m *memoryOutbox) MarkFailed(ctx context.Context, id uuid.UUID) error {
	// In memory, a failed (undecodable) record is treated as terminal so it does
	// not loop forever.
	return m.MarkProcessed(ctx, id)
}

// IncrementAttempt bumps the attempt counter for a record and returns the new
// value. S97 — used by the relay before each delivery so the budget is charged
// whether the dispatch succeeded, failed, or the process crashed.
func (m *memoryOutbox) IncrementAttempt(_ context.Context, id uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[id]++
	return m.attempts[id], nil
}

// DeadLetter moves a record to dead-letter state with a human-readable reason.
func (m *memoryOutbox) DeadLetter(_ context.Context, id uuid.UUID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.ID == id {
			m.dlq[id] = DeadLetteredRecord{
				Record:         r,
				DeadLetteredAt: time.Now().UTC(),
				LastError:      reason,
			}
			return nil
		}
	}
	return nil
}

// Requeue clears the dead-letter state and resets the attempt counter so the
// next Recover cycle picks the record up again.
func (m *memoryOutbox) Requeue(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.dlq, id)
	delete(m.attempts, id)
	return nil
}

// ListDeadLettered returns dead-lettered records paginated by limit+offset.
func (m *memoryOutbox) ListDeadLettered(_ context.Context, limit, offset int) ([]DeadLetteredRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Iterate the records in append order so the listing has a stable shape.
	var out []DeadLetteredRecord
	for _, r := range m.records {
		dl, ok := m.dlq[r.ID]
		if !ok {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		out = append(out, dl)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// PendingCount returns the number of records not yet processed, failed, or
// dead-lettered — i.e. what the recovery scan would still pick up.
func (m *memoryOutbox) PendingCount(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.records {
		if m.done[r.ID] {
			continue
		}
		if _, dl := m.dlq[r.ID]; dl {
			continue
		}
		n++
	}
	return n, nil
}

var _ DeadLetterStore = (*memoryOutbox)(nil)
