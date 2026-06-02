package cache

import (
	"context"
	"testing"
	"time"
)

// TestMemory_RoundTrip_HitAndMiss confirms the basic put/get path: a
// freshly stored key returns the bytes (with a defensive copy, so a
// caller mutation doesn't poison the store), and an unknown key
// returns clean miss (false, nil).
func TestMemory_RoundTrip_HitAndMiss(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	if _, ok, err := c.Get(ctx, "absent"); ok || err != nil {
		t.Fatalf("clean miss expected, got ok=%v err=%v", ok, err)
	}

	want := []byte(`{"x":1}`)
	if err := c.Set(ctx, "k", want, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || string(got) != `{"x":1}` {
		t.Fatalf("Get: ok=%v err=%v got=%q", ok, err, got)
	}
	// Mutating the returned slice MUST NOT corrupt the stored value.
	got[0] = '!'
	got2, _, _ := c.Get(ctx, "k")
	if string(got2) != `{"x":1}` {
		t.Errorf("stored value corrupted by caller mutation: %q", got2)
	}
}

// TestMemory_TTLExpiry_TreatedAsMiss verifies expired entries return
// (nil, false, nil). The lazy-evict path means we don't pay the
// write-lock cost on the read; we just compare timestamps.
func TestMemory_TTLExpiry_TreatedAsMiss(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	// Set with a 10ms TTL; sleep past it.
	if err := c.Set(ctx, "k", []byte("v"), 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Errorf("expired entry should not hit")
	}
}

// TestMemory_Delete_RemovesKey is the invalidation contract — Delete
// is what the handler calls on mutation paths so a fresh read sees
// the new state.
func TestMemory_Delete_RemovesKey(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()
	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Errorf("Get after Delete should miss")
	}
	// Delete is idempotent — second call on a missing key is a no-op.
	if err := c.Delete(ctx, "k"); err != nil {
		t.Errorf("idempotent Delete returned %v", err)
	}
}

// TestMemory_NonPositiveTTL_FallsBackToOneMinute prevents a "Set with
// 0 stores forever" footgun. The fallback (1 minute) matches the
// Redis adapter so the two are interchangeable in tests.
func TestMemory_NonPositiveTTL_FallsBackToOneMinute(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()
	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Immediately readable.
	if _, ok, _ := c.Get(ctx, "k"); !ok {
		t.Errorf("entry not stored — TTL fallback did not fire")
	}
}

// TestNoop_AlwaysMisses pins the noop adapter contract: every Get is
// a miss, Set + Delete never error.
func TestNoop_AlwaysMisses(t *testing.T) {
	c := NewNoop()
	ctx := context.Background()
	if _, ok, _ := c.Get(ctx, "x"); ok {
		t.Errorf("noop Get should miss")
	}
	if err := c.Set(ctx, "x", []byte("y"), time.Minute); err != nil {
		t.Errorf("noop Set returned %v", err)
	}
	if _, ok, _ := c.Get(ctx, "x"); ok {
		t.Errorf("noop should not store anything")
	}
	if err := c.Delete(ctx, "x", "y"); err != nil {
		t.Errorf("noop Delete returned %v", err)
	}
}
