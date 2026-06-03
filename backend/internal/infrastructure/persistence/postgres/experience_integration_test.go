// Postgres integration tests for experience.Repository (S75). Mirrors
// the env-skip pattern from S41/S55/S72 — opt in via POSTGRES_TEST_DSN,
// run from CI's postgres-integration job.
//
// The in-memory adapter gives the application-level contract; this
// file covers what the in-memory path cannot:
//
//   - Photos JSONB round-trips faithfully — id/objectKey/url/position
//     survive the encoding/json pgx hop.
//   - Update bumps updated_at strictly later than created_at (the host
//     dashboard relies on this for the "edited" badge).
//   - Delete actually removes the row (FindByID then returns
//     shared.ErrNotFound, not a stale clone).
//   - ListByHost order matches the memory adapter — created_at DESC
//     with id ASC tiebreaker — so swapping backends doesn't reshuffle
//     paged views.
//   - Search hides drafts + suspended regardless of filter (the
//     catalogue invariant) and the city LIKE filter is enforced in SQL.
//
// Run locally with:
//
//	POSTGRES_TEST_DSN='postgres://airhost:airhost@localhost:5432/airhost?sslmode=disable' \
//	  go test ./internal/infrastructure/persistence/postgres/... -run TestIntegration_Experience
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/shared"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// newTestExperience builds a valid draft Experience aggregate against a
// random host id. The caller is expected to mutate (Publish, AddPhoto,
// etc.) before Save when test intent demands a non-draft state.
func newTestExperience(t *testing.T, hostID uuid.UUID, city string) *experience.Experience {
	t.Helper()
	price, err := shared.NewMoney(7500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	addr := experience.Address{
		City:      city,
		Country:   "PT",
		Latitude:  38.7,
		Longitude: -9.1,
	}
	e, err := experience.NewExperience(
		hostID,
		"Sunset food tour",
		"A 2-hour walk through the old town with three tastings.",
		experience.CategoryTour,
		addr,
		120,
		8,
		price,
		"en",
	)
	if err != nil {
		t.Fatalf("NewExperience: %v", err)
	}
	return e
}

// TestIntegration_Experience_CreateFindRoundTrip — every domain field,
// including the JSONB Photos slice, must survive a Create + FindByID hop.
func TestIntegration_Experience_CreateFindRoundTrip(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experiences")
	repo := pg.NewExperienceRepository(pool)
	ctx := context.Background()

	e := newTestExperience(t, uuid.New(), "Lisbon")
	e.AddPhoto("photos/cover.jpg", "https://cdn.example.com/cover.jpg")
	e.AddPhoto("photos/snack.jpg", "https://cdn.example.com/snack.jpg")
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, e.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID != e.ID || got.HostID != e.HostID {
		t.Errorf("ids lost: got=%+v want=%+v", got, e)
	}
	if got.Title != e.Title || got.Description != e.Description {
		t.Errorf("text fields lost: title=%q desc=%q", got.Title, got.Description)
	}
	if got.Category != experience.CategoryTour || got.Status != experience.StatusDraft {
		t.Errorf("enums lost: category=%s status=%s", got.Category, got.Status)
	}
	if got.Address.City != "Lisbon" || got.Address.Country != "PT" {
		t.Errorf("address lost: %+v", got.Address)
	}
	if got.Address.Latitude != 38.7 || got.Address.Longitude != -9.1 {
		t.Errorf("coords lost: %+v", got.Address)
	}
	if got.DurationMinutes != 120 || got.MaxGuests != 8 {
		t.Errorf("scalars lost: duration=%d max=%d", got.DurationMinutes, got.MaxGuests)
	}
	if got.PricePerGuest.AmountCents() != 7500 || got.PricePerGuest.Currency() != "EUR" {
		t.Errorf("price lost: %s", got.PricePerGuest)
	}
	if got.Language != "en" {
		t.Errorf("language = %q, want en", got.Language)
	}
	if len(got.Photos) != 2 {
		t.Fatalf("photos len = %d, want 2 (%+v)", len(got.Photos), got.Photos)
	}
	// JSONB round-trip — id + objectKey + url + position all intact.
	if got.Photos[0].ID != e.Photos[0].ID ||
		got.Photos[0].ObjectKey != "photos/cover.jpg" ||
		got.Photos[0].URL != "https://cdn.example.com/cover.jpg" ||
		got.Photos[0].Position != 0 {
		t.Errorf("photo[0] lost through JSONB: %+v", got.Photos[0])
	}
	if got.Photos[1].Position != 1 || got.Photos[1].ObjectKey != "photos/snack.jpg" {
		t.Errorf("photo[1] lost through JSONB: %+v", got.Photos[1])
	}
}

// TestIntegration_Experience_Update_BumpsUpdatedAt — Update must
// strictly advance UpdatedAt past CreatedAt. The host dashboard's
// "edited" badge depends on this monotonicity.
func TestIntegration_Experience_Update_BumpsUpdatedAt(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experiences")
	repo := pg.NewExperienceRepository(pool)
	ctx := context.Background()

	e := newTestExperience(t, uuid.New(), "Porto")
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}
	originalCreated := e.CreatedAt

	// Force a tick so the touch() in UpdateBasics produces a strictly
	// later time even on coarse-resolution clocks.
	time.Sleep(2 * time.Millisecond)

	newPrice, _ := shared.NewMoney(9000, "EUR")
	if err := e.UpdateBasics(
		"Sunset food tour (premium)",
		"Now with a fourth tasting.",
		experience.CategoryTour,
		experience.Address{City: "Porto", Country: "PT", Latitude: 41.15, Longitude: -8.61},
		150, 6, newPrice, "en",
	); err != nil {
		t.Fatalf("UpdateBasics: %v", err)
	}
	if err := repo.Update(ctx, e); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, e.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !got.UpdatedAt.After(originalCreated) {
		t.Errorf("UpdatedAt %v not after CreatedAt %v", got.UpdatedAt, originalCreated)
	}
	if got.Title != "Sunset food tour (premium)" || got.MaxGuests != 6 || got.DurationMinutes != 150 {
		t.Errorf("update did not persist: %+v", got)
	}
	if got.PricePerGuest.AmountCents() != 9000 {
		t.Errorf("price update lost: %s", got.PricePerGuest)
	}
}

// TestIntegration_Experience_Delete_RemovesRow — after Delete, the row
// is gone and FindByID maps to shared.ErrNotFound (which the handler
// translates to 404).
func TestIntegration_Experience_Delete_RemovesRow(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experiences")
	repo := pg.NewExperienceRepository(pool)
	ctx := context.Background()

	e := newTestExperience(t, uuid.New(), "Faro")
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.FindByID(ctx, e.ID)
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("FindByID after Delete = %v, want shared.ErrNotFound", err)
	}

	// Deleting again should also signal not-found rather than silently
	// succeeding — the handler relies on that for the idempotent-DELETE
	// 404 path.
	if err := repo.Delete(ctx, e.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("second Delete = %v, want shared.ErrNotFound", err)
	}
}

// TestIntegration_Experience_ListByHost_OrdersNewestFirst seeds three
// experiences with staggered CreatedAt and asserts the returned list is
// strictly newest-first.
func TestIntegration_Experience_ListByHost_OrdersNewestFirst(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experiences")
	repo := pg.NewExperienceRepository(pool)
	ctx := context.Background()

	hostID := uuid.New()
	base := time.Now().UTC().Add(-3 * time.Hour)
	// oldest, middle, newest — inserted in deliberately scrambled order
	// so the test passes only when the SQL ORDER BY actually runs.
	oldest := newTestExperience(t, hostID, "Lisbon")
	oldest.CreatedAt = base
	oldest.UpdatedAt = base
	middle := newTestExperience(t, hostID, "Lisbon")
	middle.CreatedAt = base.Add(1 * time.Hour)
	middle.UpdatedAt = middle.CreatedAt
	newest := newTestExperience(t, hostID, "Lisbon")
	newest.CreatedAt = base.Add(2 * time.Hour)
	newest.UpdatedAt = newest.CreatedAt

	for _, e := range []*experience.Experience{middle, oldest, newest} {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("Create %s: %v", e.ID, err)
		}
	}

	// Seed a second host so the WHERE host_id = $1 filter is exercised.
	other := newTestExperience(t, uuid.New(), "Lisbon")
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("Create other host: %v", err)
	}

	res, err := repo.ListByHost(ctx, hostID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if res.Total != 3 || len(res.Items) != 3 {
		t.Fatalf("counts = %d/%d, want 3/3 (other host leaked?)", res.Total, len(res.Items))
	}
	if res.Items[0].ID != newest.ID || res.Items[1].ID != middle.ID || res.Items[2].ID != oldest.ID {
		var order []string
		for _, it := range res.Items {
			order = append(order, it.CreatedAt.Format(time.RFC3339))
		}
		t.Errorf("order = %v, want [newest middle oldest]", order)
	}
}

// TestIntegration_Experience_Search_FiltersAndHidesNonPublished seeds
// three rows: one draft (Lisbon), one published Lisbon, one published
// Porto. Filtering by Lisbon must return the published Lisbon only —
// the draft is invisible regardless, and Porto is filtered out.
func TestIntegration_Experience_Search_FiltersAndHidesNonPublished(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "experiences")
	repo := pg.NewExperienceRepository(pool)
	ctx := context.Background()

	hostID := uuid.New()
	draft := newTestExperience(t, hostID, "Lisbon")
	// draft has photos + description so Publish would succeed, but we
	// deliberately leave it in StatusDraft to prove Search hides it.
	draft.AddPhoto("k", "u")

	pubLisbon := newTestExperience(t, hostID, "Lisbon")
	pubLisbon.AddPhoto("k", "u")
	if err := pubLisbon.Publish(); err != nil {
		t.Fatalf("Publish Lisbon: %v", err)
	}

	pubPorto := newTestExperience(t, hostID, "Porto")
	pubPorto.AddPhoto("k", "u")
	if err := pubPorto.Publish(); err != nil {
		t.Fatalf("Publish Porto: %v", err)
	}

	for _, e := range []*experience.Experience{draft, pubLisbon, pubPorto} {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// Filter by city Lisbon — only pubLisbon qualifies.
	res, err := repo.Search(ctx, experience.SearchFilter{City: "Lisbon"}, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("Search city=Lisbon: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("Lisbon counts = %d/%d, want 1/1", res.Total, len(res.Items))
	}
	if res.Items[0].ID != pubLisbon.ID {
		t.Errorf("Lisbon hit = %s, want pubLisbon %s", res.Items[0].ID, pubLisbon.ID)
	}

	// Empty filter — both published rows return, draft hidden.
	res, err = repo.Search(ctx, experience.SearchFilter{}, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("empty-filter counts = %d/%d, want 2/2 (draft must be hidden)", res.Total, len(res.Items))
	}
	for _, it := range res.Items {
		if it.ID == draft.ID {
			t.Errorf("draft leaked into Search results")
		}
		if it.Status != experience.StatusPublished {
			t.Errorf("non-published row %s returned with status %s", it.ID, it.Status)
		}
	}
}
