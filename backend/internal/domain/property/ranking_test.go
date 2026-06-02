package property

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func mustMoney(t *testing.T, cents int64) shared.Money {
	t.Helper()
	m, err := shared.NewMoney(cents, "EUR")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	return m
}

// listingWith builds a minimal Property fixture for ranking tests. Only the
// fields the score function reads are populated.
func listingWith(t *testing.T, opts ...func(*Property)) *Property {
	t.Helper()
	p := &Property{
		ID:            uuid.New(),
		HostID:        uuid.New(),
		Title:         "fixture",
		Type:          TypeApartment,
		Status:        StatusPublished,
		PricePerNight: mustMoney(t, 10000),
		CleaningFee:   mustMoney(t, 0),
		MaxGuests:     2,
		Photos:        nil,
		CreatedAt:     time.Now().UTC().Add(-90 * 24 * time.Hour), // outside cold-start
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func withRating(avg float64, count int) func(*Property) {
	return func(p *Property) { p.AverageRating = avg; p.ReviewCount = count }
}
func withSuperhost() func(*Property) { return func(p *Property) { p.HostIsSuperhost = true } }
func withPhotos(n int) func(*Property) {
	return func(p *Property) {
		p.Photos = make([]Photo, n)
		for i := range p.Photos {
			p.Photos[i] = Photo{ID: uuid.New(), Position: i}
		}
	}
}
func createdAt(t time.Time) func(*Property) { return func(p *Property) { p.CreatedAt = t } }

// TestRankScore_NilIsZero — a nil property must not panic and must score 0
// (defensive: the sort code paths assume valid pointers, but a future caller
// might pass nil and we'd rather see it bubble than crash).
func TestRankScore_NilIsZero(t *testing.T) {
	if got := RankScore(nil, time.Now()); got != 0 {
		t.Fatalf("RankScore(nil) = %v, want 0", got)
	}
}

// TestRankScore_VolumeWeightedRating proves a 4.5 with hundreds of reviews
// beats a perfect 5.0 with a single review — the most important
// anti-gaming property of the formula.
func TestRankScore_VolumeWeightedRating(t *testing.T) {
	now := time.Now().UTC()
	veteran := listingWith(t, withRating(4.5, 250))
	novice := listingWith(t, withRating(5.0, 1))
	if RankScore(veteran, now) <= RankScore(novice, now) {
		t.Fatalf("veteran (4.5 × 250) should outrank novice (5.0 × 1): %v vs %v",
			RankScore(veteran, now), RankScore(novice, now))
	}
}

// TestRankScore_SuperhostBoostsOverEqualPeer — two identical listings, only
// one is a Superhost; that one ranks higher.
func TestRankScore_SuperhostBoostsOverEqualPeer(t *testing.T) {
	now := time.Now().UTC()
	plain := listingWith(t, withRating(4.7, 20))
	hero := listingWith(t, withRating(4.7, 20), withSuperhost())
	if RankScore(hero, now) <= RankScore(plain, now) {
		t.Fatal("superhost should outrank equal non-superhost")
	}
}

// TestRankScore_ColdStartFloorsNewListings — a brand-new listing with 0
// reviews and no photos should still earn a non-zero score so it isn't
// dead-on-arrival in the search.
func TestRankScore_ColdStartFloorsNewListings(t *testing.T) {
	now := time.Now().UTC()
	newbie := listingWith(t, createdAt(now.Add(-1*24*time.Hour))) // 1 day old
	if RankScore(newbie, now) <= 0 {
		t.Fatal("cold-start bonus should give new listings a non-zero score")
	}
	stale := listingWith(t, createdAt(now.Add(-120*24*time.Hour))) // 4 months old
	if RankScore(stale, now) != 0 {
		t.Fatalf("stale empty listing should score 0, got %v", RankScore(stale, now))
	}
}

// TestRankScore_PhotosBumpCapsAt10 — listings with > 10 photos don't keep
// climbing forever (anti-spam: 100 thumbnails shouldn't beat genuine
// engagement signals).
func TestRankScore_PhotosBumpCapsAt10(t *testing.T) {
	now := time.Now().UTC()
	ten := listingWith(t, withPhotos(10))
	hundred := listingWith(t, withPhotos(100))
	if RankScore(ten, now) != RankScore(hundred, now) {
		t.Fatalf("photo bonus should cap at 10 photos: %v vs %v",
			RankScore(ten, now), RankScore(hundred, now))
	}
}
