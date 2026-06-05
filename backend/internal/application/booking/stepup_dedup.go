package bookingapp

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// stepUpDeduper collapses repeated KYCStepUpRequired emissions for the same
// (guest, listing, currency) tuple inside a configurable quiet window. The
// step-up gate trips on every retry attempt — a guest who clicks "Book" three
// times in a minute would otherwise produce three identical events (three
// notifications, three emails). The deduper records the timestamp of the
// last emission and only lets fresh ones through once the TTL has elapsed.
//
// The cache is per-process: a multi-instance deployment may emit one event
// per instance per window. That's the right trade-off here — the alternative
// (a shared row in Postgres keyed by tuple) would force a write before the
// gate-failure event, and a notification fanned twice across two instances
// is far less harmful than a payment write fanned twice. Subscribers (in-app
// notification, email) should still treat the event idempotently in case a
// future redelivery surfaces it more than once.
type stepUpDeduper struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
	now  func() time.Time
}

// newStepUpDeduper builds a deduper with the given TTL. ttl <= 0 disables
// the deduper entirely (every call to markFresh returns true), letting tests
// or operators bypass the quiet window without changing the wiring.
func newStepUpDeduper(ttl time.Duration, now func() time.Time) *stepUpDeduper {
	if now == nil {
		now = time.Now
	}
	return &stepUpDeduper{
		seen: make(map[string]time.Time),
		ttl:  ttl,
		now:  now,
	}
}

// markFresh reports whether a step-up event should be emitted for key now.
// Returns true when the key hasn't been seen, or when its last sighting is
// older than the configured TTL. Updates the seen-at timestamp on a true
// return. ttl <= 0 short-circuits to true (deduper disabled).
//
// Lazy cleanup: each call sweeps expired entries while holding the mutex,
// so the map can't grow unboundedly under steady traffic.
func (d *stepUpDeduper) markFresh(key string) bool {
	if d == nil || d.ttl <= 0 {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	// Cheap lazy sweep — removes anything older than TTL. The map only
	// grows with concurrently-active gate-failers, so this stays small
	// in practice (a few hundred entries at most).
	for k, ts := range d.seen {
		if now.Sub(ts) >= d.ttl {
			delete(d.seen, k)
		}
	}
	if ts, ok := d.seen[key]; ok && now.Sub(ts) < d.ttl {
		return false
	}
	d.seen[key] = now
	return true
}

// stepUpDedupKey builds the cache key. Currency is part of the tuple because
// the same guest revisiting a multi-currency listing with a different display
// currency is a legitimately distinct gate-failure (the threshold in the new
// currency may differ enough to be a separate news item).
func stepUpDedupKey(guestID, propertyID uuid.UUID, currency string) string {
	return fmt.Sprintf("%s|%s|%s", guestID, propertyID, currency)
}
