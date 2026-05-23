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
	price, _ := shared.NewMoney(10000, "EUR")
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
}

func TestSearch_GeoRadiusFilters(t *testing.T) {
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	svc := searchapp.NewService(props, memory.NewBookingRepository())

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
