// Package propertyapp contains listing-related use cases.
package propertyapp

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates property use cases.
type Service struct {
	repo    property.Repository
	storage port.Storage
}

// NewService wires the property application service.
func NewService(repo property.Repository, storage port.Storage) *Service {
	return &Service{repo: repo, storage: storage}
}

// CreateInput carries the data required to create a listing.
type CreateInput struct {
	HostID        uuid.UUID
	Title         string
	Description   string
	Type          string
	AddressLine1  string
	City          string
	Country       string
	PostalCode    string
	Latitude      float64
	Longitude     float64
	PriceCents    int64
	Currency      string
	MaxGuests     int
	Bedrooms      int
	Beds          int
	Bathrooms     int
	Amenities     []string
}

// Create builds and persists a draft listing.
func (s *Service) Create(ctx context.Context, in CreateInput) (*property.Property, error) {
	price, err := shared.NewMoney(in.PriceCents, in.Currency)
	if err != nil {
		return nil, err
	}
	addr := property.Address{
		Line1:      in.AddressLine1,
		City:       in.City,
		Country:    in.Country,
		PostalCode: in.PostalCode,
		Latitude:   in.Latitude,
		Longitude:  in.Longitude,
	}
	p, err := property.NewProperty(
		in.HostID, in.Title, in.Description, property.PropertyType(in.Type),
		addr, price, in.MaxGuests, in.Bedrooms, in.Beds, in.Bathrooms, in.Amenities,
	)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// GetByID fetches a single listing.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	return s.repo.FindByID(ctx, id)
}

// Search runs a filtered listing search.
func (s *Service) Search(ctx context.Context, c property.SearchCriteria) (shared.PageResult[*property.Property], error) {
	return s.repo.Search(ctx, c)
}

// ListByHost returns the listings owned by a host.
func (s *Service) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	return s.repo.ListByHost(ctx, hostID, page)
}

// UpdateInput carries editable listing fields.
type UpdateInput struct {
	Title       string
	Description string
	PriceCents  int64
	Currency    string
	MaxGuests   int
}

// Update mutates a listing after verifying ownership.
func (s *Service) Update(ctx context.Context, actorID, propertyID uuid.UUID, in UpdateInput) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	price, err := shared.NewMoney(in.PriceCents, in.Currency)
	if err != nil {
		return nil, err
	}
	if err := p.UpdateDetails(in.Title, in.Description, price, in.MaxGuests); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Publish publishes a listing after verifying ownership.
func (s *Service) Publish(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	if err := p.Publish(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UploadPhoto stores an uploaded image and attaches it to the listing.
func (s *Service) UploadPhoto(
	ctx context.Context,
	actorID, propertyID uuid.UUID,
	r io.Reader, size int64, contentType, filename string,
) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("properties/%s/%s%s", propertyID, uuid.NewString(), path.Ext(filename))
	url, err := s.storage.Upload(ctx, objectKey, r, size, contentType)
	if err != nil {
		return nil, err
	}
	p.AddPhoto(objectKey, url)
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// PresignPhotoUpload returns a direct-upload URL for client-side uploads.
func (s *Service) PresignPhotoUpload(ctx context.Context, actorID, propertyID uuid.UUID, ext string) (string, string, error) {
	if _, err := s.ownedProperty(ctx, actorID, propertyID); err != nil {
		return "", "", err
	}
	objectKey := fmt.Sprintf("properties/%s/%s%s", propertyID, uuid.NewString(), ext)
	url, err := s.storage.PresignedPutURL(ctx, objectKey, 15*time.Minute)
	if err != nil {
		return "", "", err
	}
	return url, objectKey, nil
}

// Delete removes a listing after verifying ownership.
func (s *Service) Delete(ctx context.Context, actorID, propertyID uuid.UUID) error {
	if _, err := s.ownedProperty(ctx, actorID, propertyID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, propertyID)
}

func (s *Service) ownedProperty(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.repo.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !p.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return p, nil
}
