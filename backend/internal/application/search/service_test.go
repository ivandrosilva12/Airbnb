package searchapp_test

import (
	"context"
	"testing"

	searchapp "github.com/airhost/backend/internal/application/search"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func publish(t *testing.T, repo *memory.PropertyRepository, title string, lat, lng float64) {
	t.Helper()
	publishPriced(t, repo, title, lat, lng, 10000)
}

func publishPriced(t *testing.T, repo *memory.PropertyRepository, title string, lat, lng float64, priceCents int64) *property.Property {
	t.Helper()
	price, _ := shared.NewMoney(priceCents, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: title, Country: "PT", Latitude: lat, Longitude: lng}
	p, err := property.NewProperty(uuid.New(), title, "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	p.AddPhoto("k", "http://x/k.jpg")
	if err := p.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("store: %v", err)
	}
	return p
}

func TestSearch_SortByPriceAndRating(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository(), memory.NewBlockRepository())

	cheap := publishPriced(t, props, "Cheap", 38.7, -9.1, 5000)
	mid := publishPriced(t, props, "Mid", 38.7, -9.1, 8000)
	pricey := publishPriced(t, props, "Pricey", 38.7, -9.1, 12000)
	_ = mid

	// Give the pricey one a rating so it sorts first by rating.
	if err := props.UpdateRating(ctx, pricey.ID, 4.8, 10); err != nil {
		t.Fatalf("update rating: %v", err)
	}

	page := shared.NewPage(20, 0)

	asc, _ := svc.Search(ctx, property.SearchCriteria{Page: page, Sort: property.SortPriceAsc}, nil)
	if asc.Items[0].ID != cheap.ID {
		t.Fatalf("price_asc first = %s, want Cheap", asc.Items[0].Title)
	}

	desc, _ := svc.Search(ctx, property.SearchCriteria{Page: page, Sort: property.SortPriceDesc}, nil)
	if desc.Items[0].ID != pricey.ID {
		t.Fatalf("price_desc first = %s, want Pricey", desc.Items[0].Title)
	}

	rating, _ := svc.Search(ctx, property.SearchCriteria{Page: page, Sort: property.SortRating}, nil)
	if rating.Items[0].ID != pricey.ID {
		t.Fatalf("rating first = %s, want Pricey (highest rated)", rating.Items[0].Title)
	}
}

// TestSearch_SortRanked verifies S63's composite ranking surfaces the
// well-reviewed Superhost listing above plain peers — exactly the order
// guests would expect when no explicit sort is chosen. Asserting through
// the public Search API (not RankScore directly) catches breakage between
// the score function and the memory repo's sort hook.
func TestSearch_SortRanked(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository(), memory.NewBlockRepository())

	plain := publishPriced(t, props, "Plain", 38.7, -9.1, 10000)
	rated := publishPriced(t, props, "Rated", 38.7, -9.1, 10000)
	hero := publishPriced(t, props, "Hero", 38.7, -9.1, 10000)

	if err := props.UpdateRating(ctx, rated.ID, 4.6, 30); err != nil {
		t.Fatalf("rate rated: %v", err)
	}
	if err := props.UpdateRating(ctx, hero.ID, 4.8, 80); err != nil {
		t.Fatalf("rate hero: %v", err)
	}
	// SetHostSuperhost fans the flag onto all of the host's listings; we
	// promote only the hero's host so the order proves the boost alone
	// can lift a listing past an equally-rated peer.
	if err := props.SetHostSuperhost(ctx, hero.HostID, true); err != nil {
		t.Fatalf("hero superhost: %v", err)
	}

	page := shared.NewPage(20, 0)
	ranked, err := svc.Search(ctx, property.SearchCriteria{Page: page, Sort: property.SortRanked}, nil)
	if err != nil {
		t.Fatalf("ranked search: %v", err)
	}
	if len(ranked.Items) != 3 {
		t.Fatalf("ranked items = %d, want 3", len(ranked.Items))
	}
	if ranked.Items[0].ID != hero.ID {
		t.Fatalf("ranked[0] = %s, want Hero (superhost + best rating)", ranked.Items[0].Title)
	}
	if ranked.Items[1].ID != rated.ID {
		t.Fatalf("ranked[1] = %s, want Rated", ranked.Items[1].Title)
	}
	if ranked.Items[2].ID != plain.ID {
		t.Fatalf("ranked[2] = %s, want Plain", ranked.Items[2].Title)
	}
}

func TestSearch_KeywordQuery(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository(), memory.NewBlockRepository())

	publish(t, props, "Seaside Villa", 38.7, -9.1)
	publish(t, props, "Mountain Cabin", 40.0, -8.0)

	page := shared.NewPage(20, 0)

	// Case-insensitive title match keeps only the villa.
	res, err := svc.Search(ctx, property.SearchCriteria{Page: page, Query: "villa"}, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Total != 1 || res.Items[0].Title != "Seaside Villa" {
		t.Fatalf("query 'villa' = %d items, want 1 (Seaside Villa)", res.Total)
	}

	// A term that matches nothing returns no listings.
	none, _ := svc.Search(ctx, property.SearchCriteria{Page: page, Query: "treehouse"}, nil)
	if none.Total != 0 {
		t.Fatalf("query 'treehouse' = %d, want 0", none.Total)
	}
}

func TestSearch_PriceRoomsInstantBook(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository(), memory.NewBlockRepository())

	mk := func(title string, price int64, bedrooms int, instant bool) *property.Property {
		money, _ := shared.NewMoney(price, "EUR")
		cleaning, _ := shared.NewMoney(0, "EUR")
		addr := property.Address{City: title, Country: "PT", Latitude: 38.7, Longitude: -9.1}
		p, err := property.NewProperty(uuid.New(), title, "", property.TypeApartment, addr, money, cleaning, 4, bedrooms, bedrooms, 1, nil)
		if err != nil {
			t.Fatalf("new property: %v", err)
		}
		p.AddPhoto("k", "http://x/k.jpg")
		if instant {
			p.SetInstantBook(true)
		}
		if err := p.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		if err := props.Create(ctx, p); err != nil {
			t.Fatalf("store: %v", err)
		}
		return p
	}

	mk("Studio", 5000, 1, false)
	family := mk("Family", 12000, 3, true)
	page := shared.NewPage(20, 0)

	pr, _ := svc.Search(ctx, property.SearchCriteria{Page: page, MinPrice: 8000, MaxPrice: 15000}, nil)
	if pr.Total != 1 || pr.Items[0].ID != family.ID {
		t.Fatalf("price range 80-150 = %d items, want 1 (Family)", pr.Total)
	}

	br, _ := svc.Search(ctx, property.SearchCriteria{Page: page, MinBedrooms: 2}, nil)
	if br.Total != 1 || br.Items[0].ID != family.ID {
		t.Fatalf("bedrooms>=2 = %d items, want 1 (Family)", br.Total)
	}

	ib, _ := svc.Search(ctx, property.SearchCriteria{Page: page, InstantBookOnly: true}, nil)
	if ib.Total != 1 || ib.Items[0].ID != family.ID {
		t.Fatalf("instant-book only = %d items, want 1 (Family)", ib.Total)
	}
}

func TestSearch_GeoRadiusFilters(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository(), memory.NewBlockRepository())

	// Lisbon and Porto are ~270 km apart.
	publish(t, props, "Lisbon", 38.7223, -9.1393)
	publish(t, props, "Porto", 41.1496, -8.6109)

	page := shared.NewPage(20, 0)

	// No geo filter -> both listings.
	all, err := svc.Search(ctx, property.SearchCriteria{Page: page}, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("no-geo total = %d, want 2", all.Total)
	}

	// 50 km around Lisbon -> only Lisbon.
	near := property.SearchCriteria{Page: page, Geo: &property.GeoFilter{Lat: 38.7223, Lng: -9.1393, RadiusKm: 50}}
	res, err := svc.Search(ctx, near, nil)
	if err != nil {
		t.Fatalf("geo search: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 || res.Items[0].Address.City != "Lisbon" {
		t.Fatalf("50km near Lisbon = %d items, want 1 (Lisbon)", res.Total)
	}

	// 400 km around Lisbon -> both.
	wide := property.SearchCriteria{Page: page, Geo: &property.GeoFilter{Lat: 38.7223, Lng: -9.1393, RadiusKm: 400}}
	res, err = svc.Search(ctx, wide, nil)
	if err != nil {
		t.Fatalf("wide geo search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("400km near Lisbon total = %d, want 2", res.Total)
	}
}
