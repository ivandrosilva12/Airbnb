// Postgres integration tests for the outbox dead-letter-queue methods
// added in S97 (WF-GAP-018). The memory outbox already has unit-level
// coverage in event/outbox_test.go; this file rounds-trips the *real*
// Postgres queries so the SQL — including the partial indexes added in
// migration 0055 — actually behaves the way the relay expects.
//
// Skipped unless POSTGRES_TEST_DSN is set (see integration_test.go).
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// withTestDBForOutbox mirrors withTestDB but only touches the outbox
// table — the DLQ tests do not need the booking / property / user FK
// chain, so truncating those tables would be pointless work (and would
// race with other integration tests that run in parallel).
func withTestDBForOutbox(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := withTestDB(t)
	truncate(t, pool, "outbox")
	return pool
}

// appendOutboxRecord inserts a fresh record straight through the
// OutboxRepository. The DLQ tests don't care about the payload shape —
// only the row's state transitions — so a minimal Record is enough.
func appendOutboxRecord(t *testing.T, repo *pg.OutboxRepository, name string) event.Record {
	t.Helper()
	rec := event.Record{
		ID:        uuid.New(),
		Name:      name,
		Payload:   []byte(`{}`),
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.Append(context.Background(), rec); err != nil {
		t.Fatalf("append outbox record: %v", err)
	}
	return rec
}

// TestOutboxDLQ_IncrementAttempt_Integration — three back-to-back
// IncrementAttempt calls must return 1/2/3 in order, and the attempts
// column in the DB must match the returned value each time. Locks the
// "RETURNING attempts" contract the relay relies on for the per-record
// budget check.
func TestOutboxDLQ_IncrementAttempt_Integration(t *testing.T) {
	pool := withTestDBForOutbox(t)
	ctx := context.Background()
	repo := pg.NewOutboxRepository(pool)

	rec := appendOutboxRecord(t, repo, "test.event.increment")

	for want := 1; want <= 3; want++ {
		got, err := repo.IncrementAttempt(ctx, rec.ID)
		if err != nil {
			t.Fatalf("IncrementAttempt #%d: %v", want, err)
		}
		if got != want {
			t.Errorf("IncrementAttempt #%d returned %d, want %d", want, got, want)
		}
		var col int
		if err := pool.QueryRow(ctx,
			`SELECT attempts FROM outbox WHERE id = $1`, rec.ID,
		).Scan(&col); err != nil {
			t.Fatalf("read attempts column: %v", err)
		}
		if col != want {
			t.Errorf("attempts column = %d after #%d, want %d", col, want, want)
		}
	}
}

// TestOutboxDLQ_DeadLetterAndList_Integration — DeadLetter two records
// with distinct reasons, then ListDeadLettered must return both, newest
// first (dead_lettered_at DESC), with the right last_error strings on
// each.
func TestOutboxDLQ_DeadLetterAndList_Integration(t *testing.T) {
	pool := withTestDBForOutbox(t)
	ctx := context.Background()
	repo := pg.NewOutboxRepository(pool)

	first := appendOutboxRecord(t, repo, "test.event.dlq.first")
	second := appendOutboxRecord(t, repo, "test.event.dlq.second")

	if err := repo.DeadLetter(ctx, first.ID, "reason-first"); err != nil {
		t.Fatalf("DeadLetter first: %v", err)
	}
	// Sleep so the two dead_lettered_at timestamps are unambiguously
	// distinct — Postgres now() has microsecond resolution, but two calls
	// in the same statement-batch can land identically.
	time.Sleep(10 * time.Millisecond)
	if err := repo.DeadLetter(ctx, second.ID, "reason-second"); err != nil {
		t.Fatalf("DeadLetter second: %v", err)
	}

	list, err := repo.ListDeadLettered(ctx, 50, 0)
	if err != nil {
		t.Fatalf("ListDeadLettered: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	// Newest first: second was dead-lettered after first.
	if list[0].Record.ID != second.ID {
		t.Errorf("list[0].ID = %v, want %v (second, newest)", list[0].Record.ID, second.ID)
	}
	if list[0].LastError != "reason-second" {
		t.Errorf("list[0].LastError = %q, want %q", list[0].LastError, "reason-second")
	}
	if list[1].Record.ID != first.ID {
		t.Errorf("list[1].ID = %v, want %v (first)", list[1].Record.ID, first.ID)
	}
	if list[1].LastError != "reason-first" {
		t.Errorf("list[1].LastError = %q, want %q", list[1].LastError, "reason-first")
	}
	// dead_lettered_at on each row must be populated and ordered DESC.
	if !list[0].DeadLetteredAt.After(list[1].DeadLetteredAt) &&
		!list[0].DeadLetteredAt.Equal(list[1].DeadLetteredAt) {
		t.Errorf("list[0].DeadLetteredAt (%v) not >= list[1].DeadLetteredAt (%v)",
			list[0].DeadLetteredAt, list[1].DeadLetteredAt)
	}
}

// TestOutboxDLQ_RequeueClearsState_Integration — once a record has been
// dead-lettered and its attempt counter pumped, Requeue must clear the
// DLQ state + reset attempts to 0 so FetchUnprocessed picks it up again
// on the next relay cycle. We force created_at into the past so the
// 30-second grace window in FetchUnprocessed does not hide the row.
func TestOutboxDLQ_RequeueClearsState_Integration(t *testing.T) {
	pool := withTestDBForOutbox(t)
	ctx := context.Background()
	repo := pg.NewOutboxRepository(pool)

	// Insert the record with a backdated created_at so FetchUnprocessed's
	// grace clause (created_at < now() - interval '30 seconds') does not
	// suppress it. The OutboxRepository.Append helper would stamp now(),
	// so we insert directly here.
	id := uuid.New()
	backdated := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := pool.Exec(ctx,
		`INSERT INTO outbox (id, event_name, payload, created_at, attempts)
		 VALUES ($1, $2, $3::jsonb, $4, $5)`,
		id, "test.event.requeue", `{}`, backdated, 0,
	); err != nil {
		t.Fatalf("seed backdated record: %v", err)
	}

	if err := repo.DeadLetter(ctx, id, "burnt out"); err != nil {
		t.Fatalf("DeadLetter: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := repo.IncrementAttempt(ctx, id); err != nil {
			t.Fatalf("IncrementAttempt #%d: %v", i+1, err)
		}
	}

	// Sanity: while dead-lettered, FetchUnprocessed must NOT see the row.
	pending, err := repo.FetchUnprocessed(ctx, 50)
	if err != nil {
		t.Fatalf("FetchUnprocessed (pre-requeue): %v", err)
	}
	for _, r := range pending {
		if r.ID == id {
			t.Fatalf("FetchUnprocessed returned dead-lettered record %v before requeue", id)
		}
	}

	if err := repo.Requeue(ctx, id); err != nil {
		t.Fatalf("Requeue: %v", err)
	}

	// Post-Requeue: attempts reset to 0, dead_lettered_at / last_error /
	// failed_at all cleared (verify via direct column read; the Record
	// struct does not expose all of these).
	var attempts int
	var deadAt, failedAt *time.Time
	var lastErr *string
	if err := pool.QueryRow(ctx,
		`SELECT attempts, dead_lettered_at, failed_at, last_error
		   FROM outbox WHERE id = $1`, id,
	).Scan(&attempts, &deadAt, &failedAt, &lastErr); err != nil {
		t.Fatalf("read columns post-requeue: %v", err)
	}
	if attempts != 0 {
		t.Errorf("attempts post-requeue = %d, want 0", attempts)
	}
	if deadAt != nil {
		t.Errorf("dead_lettered_at post-requeue = %v, want NULL", *deadAt)
	}
	if failedAt != nil {
		t.Errorf("failed_at post-requeue = %v, want NULL", *failedAt)
	}
	if lastErr != nil {
		t.Errorf("last_error post-requeue = %v, want NULL", *lastErr)
	}

	// FetchUnprocessed must now find the record again.
	pending, err = repo.FetchUnprocessed(ctx, 50)
	if err != nil {
		t.Fatalf("FetchUnprocessed (post-requeue): %v", err)
	}
	var found bool
	for _, r := range pending {
		if r.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FetchUnprocessed did not return requeued record %v", id)
	}
}

// TestOutboxDLQ_PendingCountExcludesDLQ_Integration — PendingCount must
// only count rows still awaiting delivery; processed and dead-lettered
// rows must be excluded. With three records (one processed, one
// dead-lettered, one untouched), the count must be exactly 1.
func TestOutboxDLQ_PendingCountExcludesDLQ_Integration(t *testing.T) {
	pool := withTestDBForOutbox(t)
	ctx := context.Background()
	repo := pg.NewOutboxRepository(pool)

	a := appendOutboxRecord(t, repo, "test.event.pending.a")
	b := appendOutboxRecord(t, repo, "test.event.pending.b")
	_ = appendOutboxRecord(t, repo, "test.event.pending.c") // the lone survivor

	if err := repo.DeadLetter(ctx, a.ID, "irreparable"); err != nil {
		t.Fatalf("DeadLetter a: %v", err)
	}
	if err := repo.MarkProcessed(ctx, b.ID); err != nil {
		t.Fatalf("MarkProcessed b: %v", err)
	}

	n, err := repo.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if n != 1 {
		t.Errorf("PendingCount = %d, want 1 (a=DLQ, b=processed, c=pending)", n)
	}
}
