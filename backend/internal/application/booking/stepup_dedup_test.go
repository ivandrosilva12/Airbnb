package bookingapp

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestStepUpDeduper_FirstHitFresh — first call for a key returns true.
func TestStepUpDeduper_FirstHitFresh(t *testing.T) {
	d := newStepUpDeduper(time.Hour, func() time.Time { return time.Unix(1_700_000_000, 0) })
	key := stepUpDedupKey(uuid.New(), uuid.New(), "EUR")
	if !d.markFresh(key) {
		t.Fatal("first markFresh should return true")
	}
}

// TestStepUpDeduper_RepeatWithinTTL — second call within TTL returns false.
func TestStepUpDeduper_RepeatWithinTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &now
	d := newStepUpDeduper(time.Hour, func() time.Time { return *clock })
	key := stepUpDedupKey(uuid.New(), uuid.New(), "EUR")
	if !d.markFresh(key) {
		t.Fatal("first call should be fresh")
	}
	*clock = now.Add(30 * time.Minute) // still within TTL
	if d.markFresh(key) {
		t.Fatal("repeat call within TTL should be deduped")
	}
}

// TestStepUpDeduper_FreshAgainAfterTTL — once TTL elapses, the key fires again.
func TestStepUpDeduper_FreshAgainAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &now
	d := newStepUpDeduper(time.Hour, func() time.Time { return *clock })
	key := stepUpDedupKey(uuid.New(), uuid.New(), "EUR")
	if !d.markFresh(key) {
		t.Fatal("first call should be fresh")
	}
	*clock = now.Add(2 * time.Hour) // past TTL
	if !d.markFresh(key) {
		t.Fatal("after TTL elapses the key should be fresh again")
	}
}

// TestStepUpDeduper_DistinctKeysIndependent — different keys don't collide.
func TestStepUpDeduper_DistinctKeysIndependent(t *testing.T) {
	d := newStepUpDeduper(time.Hour, func() time.Time { return time.Unix(1_700_000_000, 0) })
	guestA, guestB, listing := uuid.New(), uuid.New(), uuid.New()
	if !d.markFresh(stepUpDedupKey(guestA, listing, "EUR")) {
		t.Fatal("guestA first hit should be fresh")
	}
	if !d.markFresh(stepUpDedupKey(guestB, listing, "EUR")) {
		t.Fatal("guestB on the same listing should be a distinct tuple")
	}
	if !d.markFresh(stepUpDedupKey(guestA, listing, "USD")) {
		t.Fatal("guestA with a different currency should be a distinct tuple")
	}
}

// TestStepUpDeduper_DisabledWhenTTLZero — ttl=0 short-circuits to always-fresh.
func TestStepUpDeduper_DisabledWhenTTLZero(t *testing.T) {
	d := newStepUpDeduper(0, func() time.Time { return time.Unix(1_700_000_000, 0) })
	key := stepUpDedupKey(uuid.New(), uuid.New(), "EUR")
	for i := 0; i < 5; i++ {
		if !d.markFresh(key) {
			t.Fatalf("ttl=0 should never dedupe (got false at attempt %d)", i)
		}
	}
}

// TestStepUpDeduper_NilReceiverFresh — defensive: nil deduper is always-fresh.
func TestStepUpDeduper_NilReceiverFresh(t *testing.T) {
	var d *stepUpDeduper
	if !d.markFresh("any") {
		t.Fatal("nil deduper should return fresh (no dedupe state to consult)")
	}
}
