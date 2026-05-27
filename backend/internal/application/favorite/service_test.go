package favoriteapp_test

import (
	"context"
	"testing"

	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func makeProperty(t *testing.T, props *memory.PropertyRepository) uuid.UUID {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := property.NewProperty(uuid.New(), "Flat", "", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	if err := props.Create(context.Background(), p); err != nil {
		t.Fatalf("store property: %v", err)
	}
	return p.ID
}

func TestShareCollection(t *testing.T) {
	ctx := context.Background()
	favs := memory.NewFavoriteRepository()
	props := memory.NewPropertyRepository()
	svc := favoriteapp.NewService(favs, props)

	userID := uuid.New()
	propA := makeProperty(t, props)
	col, err := svc.CreateCollection(ctx, userID, "Lisbon faves")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if err := svc.Add(ctx, userID, propA, &col.ID); err != nil {
		t.Fatalf("add: %v", err)
	}

	// An unknown token is not found.
	if _, err := svc.GetSharedCollection(ctx, "nope", shared.NewPage(0, 0)); err != shared.ErrNotFound {
		t.Fatalf("unknown token err = %v, want ErrNotFound", err)
	}

	token, err := svc.ShareCollection(ctx, userID, col.ID)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty share token")
	}

	shared1, err := svc.GetSharedCollection(ctx, token, shared.NewPage(0, 0))
	if err != nil {
		t.Fatalf("get shared: %v", err)
	}
	if shared1.Name != "Lisbon faves" || len(shared1.Listings.Items) != 1 || shared1.Listings.Items[0].ID != propA {
		t.Fatalf("shared view = %q with %d listings, want the collection with propA", shared1.Name, len(shared1.Listings.Items))
	}

	// Unsharing revokes access by the old token.
	if err := svc.UnshareCollection(ctx, userID, col.ID); err != nil {
		t.Fatalf("unshare: %v", err)
	}
	if _, err := svc.GetSharedCollection(ctx, token, shared.NewPage(0, 0)); err != shared.ErrNotFound {
		t.Fatalf("after unshare err = %v, want ErrNotFound", err)
	}
}

func TestWishlistCollections(t *testing.T) {
	ctx := context.Background()
	favs := memory.NewFavoriteRepository()
	props := memory.NewPropertyRepository()
	svc := favoriteapp.NewService(favs, props)

	userID := uuid.New()
	propA := makeProperty(t, props)
	propB := makeProperty(t, props)

	col, err := svc.CreateCollection(ctx, userID, "Beach trips")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	// A duplicate name (case-insensitive) is rejected.
	if _, err := svc.CreateCollection(ctx, userID, "beach TRIPS"); err != shared.ErrConflict {
		t.Fatalf("duplicate collection err = %v, want ErrConflict", err)
	}

	// propA saved to the default bucket, propB into the collection.
	if err := svc.Add(ctx, userID, propA, nil); err != nil {
		t.Fatalf("add propA: %v", err)
	}
	if err := svc.Add(ctx, userID, propB, &col.ID); err != nil {
		t.Fatalf("add propB: %v", err)
	}

	assertCount := func(name string, filter favoriteapp.ListFilter, want int) {
		res, err := svc.List(ctx, userID, filter, shared.NewPage(50, 0))
		if err != nil {
			t.Fatalf("list %s: %v", name, err)
		}
		if len(res.Items) != want {
			t.Errorf("list %s = %d items, want %d", name, len(res.Items), want)
		}
	}
	assertCount("all", favoriteapp.ListFilter{All: true}, 2)
	assertCount("default", favoriteapp.ListFilter{}, 1)
	assertCount("collection", favoriteapp.ListFilter{CollectionID: &col.ID}, 1)

	cols, err := svc.ListCollections(ctx, userID)
	if err != nil {
		t.Fatalf("list collections: %v", err)
	}
	if len(cols) != 1 || cols[0].Count != 1 {
		t.Fatalf("collections = %+v, want 1 with count 1", cols)
	}

	// Saving under a collection the user does not own is rejected.
	if err := svc.Add(ctx, userID, propA, ptr(uuid.New())); err != shared.ErrNotFound {
		t.Fatalf("add to foreign collection err = %v, want ErrNotFound", err)
	}

	// Move propA into the collection.
	if err := svc.Move(ctx, userID, propA, &col.ID); err != nil {
		t.Fatalf("move propA: %v", err)
	}
	assertCount("collection after move", favoriteapp.ListFilter{CollectionID: &col.ID}, 2)
	assertCount("default after move", favoriteapp.ListFilter{}, 0)

	// Deleting the collection drops its listings back to the default bucket.
	if err := svc.DeleteCollection(ctx, userID, col.ID); err != nil {
		t.Fatalf("delete collection: %v", err)
	}
	assertCount("all after delete", favoriteapp.ListFilter{All: true}, 2)
	assertCount("default after delete", favoriteapp.ListFilter{}, 2)
	cols, _ = svc.ListCollections(ctx, userID)
	if len(cols) != 0 {
		t.Errorf("collections after delete = %d, want 0", len(cols))
	}
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }
