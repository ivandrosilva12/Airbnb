// Package favoriteapp contains wishlist use cases. It coordinates the favorite
// and property contexts: favorites are stored as associations (optionally filed
// under a named collection), then hydrated into full listings for display.
package favoriteapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates favorite use cases.
type Service struct {
	favorites  favorite.Repository
	properties property.Repository
}

// NewService wires the favorite application service.
func NewService(favorites favorite.Repository, properties property.Repository) *Service {
	return &Service{favorites: favorites, properties: properties}
}

// Add saves a property to the user's wishlist, optionally filing it under a
// collection (nil for the default bucket). The property and, when given, the
// collection must exist and belong to the user.
func (s *Service) Add(ctx context.Context, userID, propertyID uuid.UUID, collectionID *uuid.UUID) error {
	if _, err := s.properties.FindByID(ctx, propertyID); err != nil {
		return err
	}
	if err := s.ensureOwnedCollection(ctx, userID, collectionID); err != nil {
		return err
	}
	return s.favorites.Add(ctx, favorite.New(userID, propertyID, collectionID))
}

// Remove drops a property from the user's wishlist.
func (s *Service) Remove(ctx context.Context, userID, propertyID uuid.UUID) error {
	return s.favorites.Remove(ctx, userID, propertyID)
}

// Move files an already-saved property under a different collection (nil to move
// it back to the default bucket).
func (s *Service) Move(ctx context.Context, userID, propertyID uuid.UUID, collectionID *uuid.UUID) error {
	if err := s.ensureOwnedCollection(ctx, userID, collectionID); err != nil {
		return err
	}
	return s.favorites.SetCollection(ctx, userID, propertyID, collectionID)
}

// ensureOwnedCollection verifies a non-nil collection exists and belongs to the
// user, guarding against filing favorites under someone else's collection.
func (s *Service) ensureOwnedCollection(ctx context.Context, userID uuid.UUID, collectionID *uuid.UUID) error {
	if collectionID == nil {
		return nil
	}
	_, err := s.favorites.FindCollection(ctx, userID, *collectionID)
	return err
}

// ListFilter selects which favorites List returns.
type ListFilter struct {
	// All lists favorites across every collection (the default view).
	All bool
	// CollectionID, used when All is false, selects a specific collection; nil
	// then means the default "Saved" bucket.
	CollectionID *uuid.UUID
}

// List returns the user's favorited listings (filtered per the given filter),
// hydrated into full properties. Listings since deleted are skipped.
func (s *Service) List(ctx context.Context, userID uuid.UUID, filter ListFilter, page shared.Page) (shared.PageResult[*property.Property], error) {
	var (
		ids shared.PageResult[uuid.UUID]
		err error
	)
	if filter.All {
		ids, err = s.favorites.ListPropertyIDs(ctx, userID, page)
	} else {
		ids, err = s.favorites.ListPropertyIDsInCollection(ctx, userID, filter.CollectionID, page)
	}
	if err != nil {
		return shared.PageResult[*property.Property]{}, err
	}
	// Batch-load all listings in one query (avoids an N+1), then re-order to the
	// wishlist order; listings since deleted are simply absent.
	loaded, err := s.properties.FindByIDs(ctx, ids.Items)
	if err != nil {
		return shared.PageResult[*property.Property]{}, err
	}
	byID := make(map[uuid.UUID]*property.Property, len(loaded))
	for _, p := range loaded {
		byID[p.ID] = p
	}
	items := make([]*property.Property, 0, len(ids.Items))
	for _, id := range ids.Items {
		if p, ok := byID[id]; ok {
			items = append(items, p)
		}
	}
	return shared.PageResult[*property.Property]{Items: items, Total: ids.Total}, nil
}

// CreateCollection creates a named wishlist collection for the user.
func (s *Service) CreateCollection(ctx context.Context, userID uuid.UUID, name string) (*favorite.Collection, error) {
	c, err := favorite.NewCollection(userID, name)
	if err != nil {
		return nil, err
	}
	if err := s.favorites.CreateCollection(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteCollection removes a collection; its listings fall back to the default
// bucket.
func (s *Service) DeleteCollection(ctx context.Context, userID, collectionID uuid.UUID) error {
	return s.favorites.DeleteCollection(ctx, userID, collectionID)
}

// ListCollections returns the user's collections with their saved-listing counts.
func (s *Service) ListCollections(ctx context.Context, userID uuid.UUID) ([]favorite.CollectionWithCount, error) {
	return s.favorites.ListCollections(ctx, userID)
}

// ShareCollection turns on a public share link for a collection the user owns
// and returns the share token.
func (s *Service) ShareCollection(ctx context.Context, userID, collectionID uuid.UUID) (string, error) {
	c, err := s.favorites.FindCollection(ctx, userID, collectionID)
	if err != nil {
		return "", err
	}
	token := c.Share()
	if err := s.favorites.SetCollectionShareToken(ctx, userID, collectionID, token); err != nil {
		return "", err
	}
	return token, nil
}

// UnshareCollection revokes a collection's public share link.
func (s *Service) UnshareCollection(ctx context.Context, userID, collectionID uuid.UUID) error {
	return s.favorites.SetCollectionShareToken(ctx, userID, collectionID, "")
}

// SharedCollection is the public read-model of a shared wishlist: the
// collection's name and the listings saved in it.
type SharedCollection struct {
	Name     string
	Listings shared.PageResult[*property.Property]
}

// GetSharedCollection resolves a public share token to the collection's name and
// its listings (hydrated; listings since deleted are skipped). No auth required.
func (s *Service) GetSharedCollection(ctx context.Context, token string, page shared.Page) (SharedCollection, error) {
	c, err := s.favorites.FindCollectionByShareToken(ctx, token)
	if err != nil {
		return SharedCollection{}, err
	}
	id := c.ID
	ids, err := s.favorites.ListPropertyIDsInCollection(ctx, c.UserID, &id, page)
	if err != nil {
		return SharedCollection{}, err
	}
	loaded, err := s.properties.FindByIDs(ctx, ids.Items)
	if err != nil {
		return SharedCollection{}, err
	}
	byID := make(map[uuid.UUID]*property.Property, len(loaded))
	for _, p := range loaded {
		byID[p.ID] = p
	}
	items := make([]*property.Property, 0, len(ids.Items))
	for _, pid := range ids.Items {
		if p, ok := byID[pid]; ok {
			items = append(items, p)
		}
	}
	return SharedCollection{
		Name:     c.Name,
		Listings: shared.PageResult[*property.Property]{Items: items, Total: ids.Total},
	}, nil
}
