// Package experienceapp orchestrates the Experience BC use cases:
// hosts create + edit + publish + suspend their own listings; guests
// browse the public catalogue via Search; admins (future slice) can
// override status. The service enforces ownership — only the host who
// owns a listing can mutate it.
package experienceapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service is the application service for the Experience BC.
type Service struct {
	repo experience.Repository
}

// NewService wires the application service.
func NewService(repo experience.Repository) *Service {
	return &Service{repo: repo}
}

// CreateInput carries the data required to draft a new experience.
type CreateInput struct {
	HostID          uuid.UUID
	Title           string
	Description     string
	Category        experience.Category
	Address         experience.Address
	DurationMinutes int
	MaxGuests       int
	PricePerGuest   shared.Money
	Language        string
}

// Create persists a new draft experience.
func (s *Service) Create(ctx context.Context, in CreateInput) (*experience.Experience, error) {
	e, err := experience.NewExperience(in.HostID, in.Title, in.Description, in.Category,
		in.Address, in.DurationMinutes, in.MaxGuests, in.PricePerGuest, in.Language)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateInput is CreateInput minus HostID (host can't transfer
// ownership) plus the target ID.
type UpdateInput struct {
	ID              uuid.UUID
	ActorID         uuid.UUID
	Title           string
	Description     string
	Category        experience.Category
	Address         experience.Address
	DurationMinutes int
	MaxGuests       int
	PricePerGuest   shared.Money
	Language        string
}

// Update mutates an existing experience after asserting the actor owns
// it. Returns shared.ErrForbidden when the actor isn't the host.
func (s *Service) Update(ctx context.Context, in UpdateInput) (*experience.Experience, error) {
	e, err := s.repo.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if e.HostID != in.ActorID {
		return nil, shared.ErrForbidden
	}
	if err := e.UpdateBasics(in.Title, in.Description, in.Category, in.Address,
		in.DurationMinutes, in.MaxGuests, in.PricePerGuest, in.Language); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// AddPhoto attaches a photo reference to a host-owned listing.
func (s *Service) AddPhoto(ctx context.Context, actorID, id uuid.UUID, objectKey, url string) (*experience.Experience, error) {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	e.AddPhoto(objectKey, url)
	if err := s.repo.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Publish transitions the listing to published, after the domain
// gatekeeper (description + at least one photo).
func (s *Service) Publish(ctx context.Context, actorID, id uuid.UUID) (*experience.Experience, error) {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	if err := e.Publish(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Suspend takes the listing off the catalogue.
func (s *Service) Suspend(ctx context.Context, actorID, id uuid.UUID) (*experience.Experience, error) {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.HostID != actorID {
		return nil, shared.ErrForbidden
	}
	e.Suspend()
	if err := s.repo.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Delete drops a listing. Only the owning host may call.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	e, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if e.HostID != actorID {
		return shared.ErrForbidden
	}
	return s.repo.Delete(ctx, id)
}

// Get reads a single experience by id. Public — drafts ARE readable
// here (the handler may filter); search is what hides drafts.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*experience.Experience, error) {
	return s.repo.FindByID(ctx, id)
}

// ListByHost returns every listing owned by the host.
func (s *Service) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*experience.Experience], error) {
	return s.repo.ListByHost(ctx, hostID, page)
}

// Search returns published listings matching the filter.
func (s *Service) Search(ctx context.Context, f experience.SearchFilter, page shared.Page) (shared.PageResult[*experience.Experience], error) {
	return s.repo.Search(ctx, f, page)
}
