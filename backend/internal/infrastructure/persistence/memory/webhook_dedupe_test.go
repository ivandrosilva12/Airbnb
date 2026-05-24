package memory

import (
	"context"
	"testing"
	"time"
)

func TestWebhookEventRepository_RecordSeenAndRetention(t *testing.T) {
	ctx := context.Background()
	r := NewWebhookEventRepository()

	if seen, _ := r.Seen(ctx, "stripe", "evt_1"); seen {
		t.Fatal("event should not be seen before recording")
	}
	_ = r.Record(ctx, "stripe", "evt_1")
	if seen, _ := r.Seen(ctx, "stripe", "evt_1"); !seen {
		t.Fatal("event should be seen after recording")
	}
	// Different provider with the same id is a distinct key.
	if seen, _ := r.Seen(ctx, "gpayangola", "evt_1"); seen {
		t.Fatal("same id under a different provider must not collide")
	}

	// Backdate one entry and add a fresh one, then prune the old.
	r.seen[dedupeKey("stripe", "evt_1")] = time.Now().UTC().Add(-48 * time.Hour)
	_ = r.Record(ctx, "stripe", "evt_2") // now-ish

	deleted, err := r.DeleteOlderThan(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if seen, _ := r.Seen(ctx, "stripe", "evt_1"); seen {
		t.Fatal("old entry should have been pruned")
	}
	if seen, _ := r.Seen(ctx, "stripe", "evt_2"); !seen {
		t.Fatal("recent entry should remain")
	}
}
