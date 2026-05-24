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
}

// NewDurablePublisher wraps a Dispatcher with outbox-backed durability.
func NewDurablePublisher(store OutboxStore, dispatch *Dispatcher) *DurablePublisher {
	return &DurablePublisher{store: store, dispatch: dispatch}
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
func (p *DurablePublisher) Recover(ctx context.Context, limit int) (int, error) {
	records, err := p.store.FetchUnprocessed(ctx, limit)
	if err != nil {
		return 0, err
	}
	var delivered int
	for _, r := range records {
		e, err := decode(r.Name, r.Payload)
		if err != nil {
			slog.Error("outbox: cannot decode record; marking failed", "event", r.Name, "id", r.ID, "error", err)
			_ = p.store.MarkFailed(ctx, r.ID)
			continue
		}
		p.dispatch.Publish(ctx, e)
		if err := p.store.MarkProcessed(ctx, r.ID); err != nil {
			slog.Warn("outbox: mark-processed failed during recovery", "id", r.ID, "error", err)
			continue
		}
		delivered++
	}
	return delivered, nil
}

var _ Publisher = (*DurablePublisher)(nil)

// memoryOutbox is a process-local OutboxStore used in tests and as a fallback.
type memoryOutbox struct {
	mu      sync.Mutex
	records []Record
	done    map[uuid.UUID]bool
}

// NewMemoryOutbox returns an in-memory OutboxStore.
func NewMemoryOutbox() OutboxStore {
	return &memoryOutbox{done: map[uuid.UUID]bool{}}
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
		if !m.done[r.ID] {
			out = append(out, r)
			if limit > 0 && len(out) >= limit {
				break
			}
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
