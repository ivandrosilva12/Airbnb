// Package favoriteapp contains wishlist use cases. It coordinates the favorite
// and property contexts: favorites are stored as associations, then hydrated
// into full listings for display.
package favoriteapp

import (
	"context"
	"errors"

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

// Add saves a property to the user's wishlist, verifying the property exists.
func (s *Service) Add(ctx context.Context, userID, propertyID uuid.UUID) error {
	if _, err := s.properties.FindByID(ctx, propertyID); err != nil {
		return err
	}
	return s.favorites.Add(ctx, favorite.New(userID, propertyID))
}

// Remove drops a property from the user's wishlist.
func (s *Service) Remove(ctx context.Context, userID, propertyID uuid.UUID) error {
	return s.favorites.Remove(ctx, userID, propertyID)
}

// List returns the user's favorited listings, hydrated into full properties.
// Listings that have since been deleted are skipped.
func (s *Service) List(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	ids, err := s.favorites.ListPropertyIDs(ctx, userID, page)
	if err != nil {
		return shared.PageResult[*property.Property]{}, err
	}
	items := make([]*property.Property, 0, len(ids.Items))
	for _, id := range ids.Items {
		p, err := s.properties.FindByID(ctx, id)
		if err != nil {
			if errors.Is(err, shared.ErrNotFound) {
				continue
			}
			return shared.PageResult[*property.Property]{}, err
		}
		items = append(items, p)
	}
	return shared.PageResult[*property.Property]{Items: items, Total: ids.Total}, nil
}
