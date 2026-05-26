package reportapp_test

import (
	"context"
	"errors"
	"testing"

	reportapp "github.com/airhost/backend/internal/application/report"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/report"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func storeProperty(t *testing.T, props *memory.PropertyRepository, hostID uuid.UUID) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Porto", Country: "PT", Latitude: 41.1, Longitude: -8.6}
	p, err := property.NewProperty(hostID, "Studio", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	_ = props.Create(context.Background(), p)
	return p
}

func TestReport_FileReviewReport(t *testing.T) {
	ctx := context.Background()
	reports := memory.NewReportRepository()
	props := memory.NewPropertyRepository()
	reviews := memory.NewReviewRepository()
	svc := reportapp.NewService(reports, props, reviews)

	prop := storeProperty(t, props, uuid.New())
	rv, err := review.NewPropertyReview(uuid.New(), prop.ID, uuid.New(), 5, "Spammy text")
	if err != nil {
		t.Fatalf("new review: %v", err)
	}
	if err := reviews.Create(ctx, rv); err != nil {
		t.Fatalf("store review: %v", err)
	}
	reporter := uuid.New()

	r, err := svc.FileReviewReport(ctx, reporter, rv.ID, "spam", "fake review")
	if err != nil {
		t.Fatalf("file review report: %v", err)
	}
	if r.TargetType != report.TargetReview || r.TargetID != rv.ID {
		t.Fatalf("target = %s/%s, want review/%s", r.TargetType, r.TargetID, rv.ID)
	}
	// The reviewed listing is recorded so the queue can show its title.
	if r.PropertyID != prop.ID {
		t.Fatalf("propertyID = %s, want %s", r.PropertyID, prop.ID)
	}

	// A duplicate open report on the same review is rejected.
	if _, err := svc.FileReviewReport(ctx, reporter, rv.ID, "spam", ""); err == nil {
		t.Fatal("expected duplicate review report to be rejected")
	}
	// Reporting an unknown review fails.
	if _, err := svc.FileReviewReport(ctx, reporter, uuid.New(), "spam", ""); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown review, got %v", err)
	}

	// The report appears in the moderation queue enriched with the listing title.
	open, err := svc.ListOpen(ctx, shared.NewPage(0, 0))
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	found := false
	for _, e := range open.Items {
		if e.Report.ID == r.ID {
			found = true
			if e.PropertyTitle != prop.Title {
				t.Errorf("title = %q, want %q", e.PropertyTitle, prop.Title)
			}
		}
	}
	if !found {
		t.Error("review report not found in the moderation queue")
	}
}

func TestReport_FileListResolve(t *testing.T) {
	ctx := context.Background()
	reports := memory.NewReportRepository()
	props := memory.NewPropertyRepository()
	svc := reportapp.NewService(reports, props, memory.NewReviewRepository())

	prop := storeProperty(t, props, uuid.New())
	reporter := uuid.New()
	admin := uuid.New()

	// Reporting an unknown listing fails.
	if _, err := svc.File(ctx, reporter, uuid.New(), "spam", ""); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// File a valid report.
	r, err := svc.File(ctx, reporter, prop.ID, "scam", "Looks fake")
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if r.Status != report.StatusOpen {
		t.Fatalf("status = %s, want open", r.Status)
	}

	// A duplicate open report from the same reporter is rejected.
	if _, err := svc.File(ctx, reporter, prop.ID, "spam", ""); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error on duplicate, got %v", err)
	}

	// An invalid reason is rejected.
	if _, err := svc.File(ctx, uuid.New(), prop.ID, "nonsense", ""); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error on bad reason, got %v", err)
	}

	// The open queue lists it, enriched with the listing title.
	open, err := svc.ListOpen(ctx, shared.NewPage(0, 0))
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if open.Total != 1 {
		t.Fatalf("open total = %d, want 1", open.Total)
	}
	if open.Items[0].PropertyTitle != "Studio" {
		t.Fatalf("enriched title = %q, want Studio", open.Items[0].PropertyTitle)
	}

	// Resolve it.
	resolved, err := svc.Resolve(ctx, admin, r.ID, "Listing suspended")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != report.StatusResolved || resolved.ResolverID != admin {
		t.Fatalf("unexpected resolved state: %+v", resolved)
	}

	// The queue is now empty.
	open, _ = svc.ListOpen(ctx, shared.NewPage(0, 0))
	if open.Total != 0 {
		t.Fatalf("open total after resolve = %d, want 0", open.Total)
	}

	// A decided report cannot be moderated again.
	if _, err := svc.Dismiss(ctx, admin, r.ID, ""); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error moderating a decided report, got %v", err)
	}

	// Once the prior report is resolved, the reporter can file again.
	if _, err := svc.File(ctx, reporter, prop.ID, "inappropriate", ""); err != nil {
		t.Fatalf("re-file after resolution: %v", err)
	}
}
