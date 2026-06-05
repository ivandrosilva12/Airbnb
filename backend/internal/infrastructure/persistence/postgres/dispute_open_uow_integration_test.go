package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/dispute"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// S139 — Postgres integration tests for the dispute OPEN UoW (sibling to
// S114's Resolve coverage). The application-layer / memory tests already
// prove that disputeapp.Service.Open routes the dispute write + the
// DisputeOpened event through the UoW via persistAndEmit (see
// backend/internal/application/dispute/service.go L186, L419-L445); this
// file closes the gap by exercising the actual pgx.Tx-bound
// DisputeRepository + outbox so the disputes INSERT and the outbox INSERT
// really do commit (or roll back) together against a live Postgres.
//
// Why a separate file from S114's dispute_uow_integration_test.go? S114
// covers the UPDATE flavor (the AdminResolve UPDATE on an already-saved
// dispute row) and asserts dispute.StatusResolved. S139 covers the INSERT
// flavor (a brand-new dispute being filed against a booking, no prior row),
// which is structurally different: the row count must grow from 0 → 1, and
// the event is dispute.opened (not dispute.resolved). Keeping the two in
// separate files mirrors the booking_cancellation / messaging UoW files,
// which each isolate a single write-shape under test.
//
// Each test:
//   - skips cleanly when POSTGRES_TEST_DSN is unset (the suite stays green
//     for dev machines without a database)
//   - reuses the withTestDB / truncate / newTestBooking helpers from
//     integration_test.go (TRUNCATE bookings CASCADE drops any stale
//     disputes rows along with their parent booking — so this file does
//     not need to truncate the disputes table itself)
//   - rolls its own UnitOfWork against the live pool so the closure sees a
//     pgx.Tx-bound DisputeRepository the same way disputeapp.persistAndEmit
//     does (it picks tx.Disputes when non-nil)
//
// Event-count rationale: investigation of
// backend/internal/application/dispute/service.go (Open at L138-L195 →
// persistAndEmit at L419-L445) shows the Open UoW emits EXACTLY ONE event
// — DisputeOpened — and performs exactly ONE disputes INSERT (via
// tx.Disputes.Save's UPSERT branch). So the atomicity contract this file
// protects is: one disputes INSERT + one DisputeOpened outbox row,
// committed or rolled back together.

// TestDisputeOpenUoW_WritesAtomicallyWithOutbox_Integration — the happy
// path. We seed a booking (the FK target for the new dispute), then run
// the UoW exactly as disputeapp.persistAndEmit does for the Open call
// site: tx.Disputes.Save on a brand-new dispute aggregate, append one
// DisputeOpened record to tx.Outbox. After commit the disputes row must
// be present (count grew by 1 for this booking), it must read back with
// status="open", AND the outbox must hold exactly one matching pending
// event keyed by event_name='dispute.opened'.
func TestDisputeOpenUoW_WritesAtomicallyWithOutbox_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	// Seed the FK chain: host + property + guest (via newTestBooking),
	// then persist the booking row OUTSIDE the test UoW so the FK target
	// of the new dispute exists before the UoW runs. We seed via a
	// separate UoW (the only path that goes through tx.Bookings.Create)
	// to keep the test honest about how bookings actually land in the DB.
	b, _ := newTestBooking(t, pool)
	if err := pg.NewUnitOfWork(pool, nil).Run(ctx, func(tx port.Tx) error {
		return tx.Bookings.Create(ctx, b)
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	// Capture baselines BEFORE the UoW so we assert only NEW rows are
	// counted. withTestDB truncates bookings CASCADE (which also drops
	// any stale disputes rows) plus outbox, so these baselines are 0 in
	// practice — pinning them explicitly keeps the test resilient
	// against any future helper that seeds rows during setup.
	var outboxBefore, disputesBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE booking_id = $1`, b.ID,
	).Scan(&disputesBefore); err != nil {
		t.Fatalf("baseline disputes count: %v", err)
	}

	// Build a brand-new dispute aggregate. The opener is the booking's
	// guest (the typical refund-request direction). The aggregate starts
	// in StatusOpen — see dispute.New at backend/internal/domain/dispute/
	// dispute.go L135 — so a successful UoW commit must read back with
	// the same status.
	d, err := dispute.New(b.ID, b.GuestID, dispute.KindRefund, "broken AC; refund requested", 2500, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}

	// Run the OPEN UoW exactly the way disputeapp.persistAndEmit does:
	// tx.Disputes.Save on the brand-new aggregate (INSERT path inside
	// the UPSERT), then append one DisputeOpened record to the outbox.
	// The PropertyID/HostID payload fields are incidental for this test
	// (we filter the outbox by DisputeID, not by them) — uuid.New() is
	// fine.
	uow := pg.NewUnitOfWork(pool, nil)
	if err := uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Disputes.Save(ctx, d); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.DisputeOpened{
			DisputeID:  d.ID,
			BookingID:  b.ID,
			PropertyID: uuid.New(),
			HostID:     uuid.New(),
			GuestID:    b.GuestID,
			OpenerID:   b.GuestID,
			Kind:       string(d.Kind),
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		t.Fatalf("uow run: %v", err)
	}

	// Assertion 1: exactly ONE new disputes row landed for this booking.
	// We round-trip the count through the table so any silent
	// column-shape regression in Save's INSERT branch also surfaces.
	var disputesAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE booking_id = $1`, b.ID,
	).Scan(&disputesAfter); err != nil {
		t.Fatalf("disputes count: %v", err)
	}
	if disputesAfter-disputesBefore != 1 {
		t.Fatalf("disputes rows for booking %s grew by %d, want 1 (the UoW commit must have inserted the new dispute)",
			b.ID, disputesAfter-disputesBefore)
	}

	// Belt-and-braces: the specific dispute id we built reloads with the
	// expected status. This guards against a case where SOME dispute row
	// landed (passing assertion 1) but not the one we wrote.
	reloaded, err := pg.NewDisputeRepository(pool).FindByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("reload dispute: %v", err)
	}
	if reloaded.Status != dispute.StatusOpen {
		t.Errorf("reloaded dispute status = %q, want %q (a freshly opened dispute must persist as open)",
			reloaded.Status, dispute.StatusOpen)
	}
	if reloaded.Kind != dispute.KindRefund {
		t.Errorf("reloaded dispute kind = %q, want %q", reloaded.Kind, dispute.KindRefund)
	}
	if reloaded.DecidedAt != nil {
		t.Errorf("reloaded dispute decided_at = %v, want NULL (Open must not stamp decided_at)", *reloaded.DecidedAt)
	}

	// Assertion 2: exactly ONE new outbox row carrying this dispute's
	// id, with event_name='dispute.opened', in pending state. The Open
	// path emits exactly one event (DisputeOpened) — see service.go
	// L176-L188. If a future change adds a second event this assertion
	// will fail loudly so the test stays honest about what the
	// production path actually emits.
	var pending int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'DisputeID' = $2
		   AND processed_at IS NULL
		   AND failed_at IS NULL
		   AND dead_lettered_at IS NULL`,
		(event.DisputeOpened{}).EventName(), d.ID.String(),
	).Scan(&pending); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending DisputeOpened rows for dispute %s = %d, want 1 (the UoW commit must have appended exactly one DisputeOpened record)",
			d.ID, pending)
	}

	// Assertion 3: the OVERALL outbox grew by exactly 1. Guards against
	// a future change that adds an extra event to the Open path but
	// happens to use a different event_name or payload key (so
	// assertion 2 would still see 1). Together the two assertions pin
	// the contract: one and only one new outbox row per Open.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter-outboxBefore != 1 {
		t.Fatalf("outbox grew by %d after Open, want 1 (the Open UoW currently emits exactly one event — update this test if disputeapp.Open starts emitting more)",
			outboxAfter-outboxBefore)
	}
}

// TestDisputeOpenUoW_RollbackOnError_Integration — same setup, but the
// UoW closure returns an error after both writes. After rollback the
// disputes table must hold ZERO new rows for this booking (since
// baseline is 0) AND no NEW outbox rows may carry this dispute's id.
// This is the contract S89 / WF-GAP-013 exists to enforce — without the
// UoW a partial commit could ship a DisputeOpened event for a dispute
// that was never persisted (the notification fires for a case the
// system can't show anyone), or persist the dispute while silently
// dropping the open notification (no admin or host ever hears about it).
func TestDisputeOpenUoW_RollbackOnError_Integration(t *testing.T) {
	pool := withTestDB(t)
	ctx := context.Background()

	b, _ := newTestBooking(t, pool)
	if err := pg.NewUnitOfWork(pool, nil).Run(ctx, func(tx port.Tx) error {
		return tx.Bookings.Create(ctx, b)
	}); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	// Baselines: how many rows EXIST before the UoW runs. We'll assert
	// these counts are unchanged after the forced rollback.
	var outboxBefore, disputesBefore int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxBefore); err != nil {
		t.Fatalf("baseline outbox count: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE booking_id = $1`, b.ID,
	).Scan(&disputesBefore); err != nil {
		t.Fatalf("baseline disputes count: %v", err)
	}

	// Build the new dispute aggregate OUTSIDE the closure (its id is
	// fixed by dispute.New so we can assert by-id afterwards even though
	// no row should land).
	d, err := dispute.New(b.ID, b.GuestID, dispute.KindRefund, "broken AC; refund requested", 2500, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}

	wantErr := errors.New("simulated mid-tx failure after dispute + outbox write")
	uow := pg.NewUnitOfWork(pool, nil)
	err = uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Disputes.Save(ctx, d); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.DisputeOpened{
			DisputeID:  d.ID,
			BookingID:  b.ID,
			PropertyID: uuid.New(),
			HostID:     uuid.New(),
			GuestID:    b.GuestID,
			OpenerID:   b.GuestID,
			Kind:       string(d.Kind),
		})
		if err != nil {
			return err
		}
		if err := tx.Outbox.Append(ctx, rec); err != nil {
			return err
		}
		return wantErr // force rollback AFTER both writes
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("uow run err = %v, want %v", err, wantErr)
	}

	// Assertion 1: disputes count for this booking is unchanged from
	// the baseline. The Save INSERT inside the failed UoW must have
	// rolled back with the transaction.
	var disputesAfter int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE booking_id = $1`, b.ID,
	).Scan(&disputesAfter); err != nil {
		t.Fatalf("disputes count: %v", err)
	}
	if disputesAfter != disputesBefore {
		t.Fatalf("disputes rows for booking %s after rollback = %d, want %d (Save must have rolled back)",
			b.ID, disputesAfter, disputesBefore)
	}

	// Belt-and-braces: the specific dispute id is not findable. Catches
	// a hypothetical bug where some other dispute landed but ours did
	// not (or vice versa) — the count check alone could mask that.
	var ourDisputeRows int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM disputes WHERE id = $1`, d.ID,
	).Scan(&ourDisputeRows); err != nil {
		t.Fatalf("count by dispute id: %v", err)
	}
	if ourDisputeRows != 0 {
		t.Fatalf("disputes row for our dispute id %s after rollback = %d, want 0 (the rolled-back Save must leave no trace)",
			d.ID, ourDisputeRows)
	}

	// Assertion 2: NO new outbox row carries this dispute id. The
	// Append inside the rolled-back tx must not have left a trace,
	// otherwise the relay would later ship a DisputeOpened event for a
	// dispute that doesn't exist in the DB (the exact split-brain the
	// UoW exists to prevent).
	var leakedForDispute int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox
		 WHERE event_name = $1
		   AND payload->>'DisputeID' = $2`,
		(event.DisputeOpened{}).EventName(), d.ID.String(),
	).Scan(&leakedForDispute); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if leakedForDispute != 0 {
		t.Fatalf("DisputeOpened rows for dispute %s after rollback = %d, want 0 (Append must have rolled back)",
			d.ID, leakedForDispute)
	}

	// Assertion 3: the TOTAL outbox row count is unchanged. Belt-and-
	// braces against a future change to event payloads (so payload
	// filtering in assertion 2 misses) and a future helper that seeds
	// outbox state during setup.
	var outboxAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outboxAfter); err != nil {
		t.Fatalf("outbox total: %v", err)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("outbox count after rollback = %d, want %d (no NEW rows must have leaked through the rolled-back tx)",
			outboxAfter, outboxBefore)
	}
}

// Compile-time guards: the test file references the dispute domain
// package and the DisputeOpened event constructor; keep explicit
// references so a future "unused import" reflow doesn't accidentally
// strip them.
var _ = dispute.StatusOpen
var _ = (event.DisputeOpened{}).EventName
var _ uuid.UUID
