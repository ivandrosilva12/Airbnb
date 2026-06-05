package blockapp_test

import (
	"context"
	"testing"
	"time"

	blockapp "github.com/airhost/backend/internal/application/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func storeProperty(t *testing.T, props *memory.PropertyRepository, hostID uuid.UUID) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(hostID, "Flat", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	_ = props.Create(context.Background(), p)
	return p
}

func days(n int) time.Time { return time.Now().UTC().AddDate(0, 0, n) }

// seedConfirmedBooking puts a confirmed reservation into the booking repo so
// the iCal-import conflict path (S168) has something to collide with.
func seedConfirmedBooking(t *testing.T, repo booking.Repository, propertyID uuid.UUID, checkIn, checkOut time.Time) {
	t.Helper()
	dates, err := booking.NewDateRange(checkIn, checkOut)
	if err != nil {
		t.Fatalf("new date range: %v", err)
	}
	b := &booking.Booking{
		ID:         uuid.New(),
		PropertyID: propertyID,
		GuestID:    uuid.New(),
		Dates:      dates,
		Guests:     1,
		Status:     booking.StatusConfirmed,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), b); err != nil {
		t.Fatalf("create booking: %v", err)
	}
}

func TestBlock_CreateListDeleteWithOwnership(t *testing.T) {
	ctx := context.Background()
	blocks := memory.NewBlockRepository()
	props := memory.NewPropertyRepository()
	svc := blockapp.NewService(blocks, props)

	hostID := uuid.New()
	prop := storeProperty(t, props, hostID)

	// A non-owner cannot block.
	if _, err := svc.Create(ctx, uuid.New(), prop.ID, days(10), days(12), ""); err != shared.ErrForbidden {
		t.Fatalf("non-owner block err = %v, want ErrForbidden", err)
	}

	// The host blocks a range.
	b, err := svc.Create(ctx, hostID, prop.ID, days(10), days(12), "Maintenance")
	if err != nil {
		t.Fatalf("create block: %v", err)
	}

	// Overlapping block is rejected.
	if _, err := svc.Create(ctx, hostID, prop.ID, days(11), days(14), ""); err == nil {
		t.Fatal("expected overlapping block to be rejected")
	}

	// Listed for the host.
	res, err := svc.ListForHost(ctx, hostID, prop.ID, shared.NewPage(10, 0))
	if err != nil || res.Total != 1 {
		t.Fatalf("list = %d (err %v), want 1", res.Total, err)
	}

	// A stranger cannot delete it.
	if err := svc.Delete(ctx, uuid.New(), b.ID); err != shared.ErrForbidden {
		t.Fatalf("non-owner delete err = %v, want ErrForbidden", err)
	}
	// The host can.
	if err := svc.Delete(ctx, hostID, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// TestImport_SkipsBookingConflict exercises the S168 path: an iCal range
// landing on top of a confirmed reservation is reported back to the host as
// a conflict instead of being silently dropped.
func TestImport_SkipsBookingConflict(t *testing.T) {
	ctx := context.Background()
	blocks := memory.NewBlockRepository()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	svc := blockapp.NewService(blocks, props).WithBookings(bookings)

	hostID := uuid.New()
	prop := storeProperty(t, props, hostID)

	// Confirmed reservation already covers nights 10..15.
	seedConfirmedBooking(t, bookings, prop.ID, days(10), days(15))

	// The host imports a feed whose only event lands on those same nights.
	result, err := svc.Import(ctx, hostID, prop.ID, []blockapp.ImportRange{
		{From: days(10), To: days(15), Reason: "Booked elsewhere"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Created != 0 {
		t.Fatalf("created = %d, want 0 (range fully shadowed by confirmed booking)", result.Created)
	}
	if result.SkippedBlockOverlap != 0 {
		t.Fatalf("skippedBlockOverlap = %d, want 0", result.SkippedBlockOverlap)
	}
	if got := len(result.SkippedBookingConflict); got != 1 {
		t.Fatalf("len(SkippedBookingConflict) = %d, want 1", got)
	}
	got := result.SkippedBookingConflict[0]
	if !got.From.Equal(truncateDay(days(10))) || !got.To.Equal(truncateDay(days(15))) {
		t.Fatalf("conflict range = %s..%s, want %s..%s",
			got.From.Format("2006-01-02"), got.To.Format("2006-01-02"),
			truncateDay(days(10)).Format("2006-01-02"), truncateDay(days(15)).Format("2006-01-02"))
	}

	// And no block landed on the calendar.
	listed, _ := svc.ListForHost(ctx, hostID, prop.ID, shared.NewPage(10, 0))
	if listed.Total != 0 {
		t.Fatalf("blocks created behind the host's back = %d, want 0", listed.Total)
	}
}

// TestImport_MixedConflicts walks a feed with three ranges — fresh, block
// dupe, and booking conflict — and asserts each ends up in the right bucket.
func TestImport_MixedConflicts(t *testing.T) {
	ctx := context.Background()
	blocks := memory.NewBlockRepository()
	props := memory.NewPropertyRepository()
	bookings := memory.NewBookingRepository()
	svc := blockapp.NewService(blocks, props).WithBookings(bookings)

	hostID := uuid.New()
	prop := storeProperty(t, props, hostID)

	// Existing block on 20..22 — a feed event there is a no-op.
	if _, err := svc.Create(ctx, hostID, prop.ID, days(20), days(22), "Maintenance"); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	// Confirmed booking on 30..33 — a feed event there is a conflict.
	seedConfirmedBooking(t, bookings, prop.ID, days(30), days(33))

	// Feed: one fresh range, one block-dupe, one booking-conflict.
	result, err := svc.Import(ctx, hostID, prop.ID, []blockapp.ImportRange{
		{From: days(10), To: days(12), Reason: "Fresh"},
		{From: days(20), To: days(22), Reason: "Dupe of existing block"},
		{From: days(30), To: days(33), Reason: "Already booked"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("created = %d, want 1", result.Created)
	}
	if result.SkippedBlockOverlap != 1 {
		t.Fatalf("skippedBlockOverlap = %d, want 1", result.SkippedBlockOverlap)
	}
	if got := len(result.SkippedBookingConflict); got != 1 {
		t.Fatalf("len(SkippedBookingConflict) = %d, want 1", got)
	}
	if got := result.SkippedBookingConflict[0]; !got.From.Equal(truncateDay(days(30))) {
		t.Fatalf("conflict.From = %s, want %s", got.From.Format("2006-01-02"), truncateDay(days(30)).Format("2006-01-02"))
	}

	// Only the fresh + the seed block are on the calendar (= 2).
	listed, _ := svc.ListForHost(ctx, hostID, prop.ID, shared.NewPage(10, 0))
	if listed.Total != 2 {
		t.Fatalf("final block count = %d, want 2 (seed + fresh import)", listed.Total)
	}
}

// TestImport_NoBookingsWired keeps the pre-S168 contract: when no booking
// repo is attached the importer simply doesn't check, so a calendar that
// shadows a reservation falls through as a normal create (the caller has
// opted out of conflict surfacing).
func TestImport_NoBookingsWired(t *testing.T) {
	ctx := context.Background()
	blocks := memory.NewBlockRepository()
	props := memory.NewPropertyRepository()
	svc := blockapp.NewService(blocks, props) // note: no WithBookings

	hostID := uuid.New()
	prop := storeProperty(t, props, hostID)

	result, err := svc.Import(ctx, hostID, prop.ID, []blockapp.ImportRange{
		{From: days(10), To: days(12), Reason: "Fresh"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Created != 1 || len(result.SkippedBookingConflict) != 0 {
		t.Fatalf("result = %+v, want Created=1, no conflicts", result)
	}
}

// truncateDay matches the day-precision DateRange uses internally so test
// equality checks line up with the values returned in SkippedBookingConflict.
func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
