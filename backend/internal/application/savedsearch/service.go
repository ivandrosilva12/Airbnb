// Package savedsearchapp contains saved-search use cases plus the periodic alert
// job: for each saved search with alerts on, it replays the search and notifies
// the owner when listings created since the last alert match.
package savedsearchapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	searchapp "github.com/airhost/backend/internal/application/search"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Notifier delivers an in-app notification to a user.
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, title, body string, relatedID uuid.UUID) error
}

// Service orchestrates saved-search use cases.
type Service struct {
	repo     savedsearch.Repository
	search   *searchapp.Service
	notifier Notifier
}

// NewService wires the saved-search application service.
func NewService(repo savedsearch.Repository, search *searchapp.Service, notifier Notifier) *Service {
	return &Service{repo: repo, search: search, notifier: notifier}
}

// Save stores a new saved search for the user.
func (s *Service) Save(ctx context.Context, userID uuid.UUID, name, query string, alertsEnabled bool) (*savedsearch.SavedSearch, error) {
	ss, err := savedsearch.New(userID, name, query, alertsEnabled)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, ss); err != nil {
		return nil, err
	}
	return ss, nil
}

// List returns the user's saved searches.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*savedsearch.SavedSearch, error) {
	return s.repo.ListByUser(ctx, userID)
}

// Delete removes a saved search the user owns.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.Delete(ctx, userID, id)
}

// SetAlerts toggles alerts for a saved search the user owns.
func (s *Service) SetAlerts(ctx context.Context, userID, id uuid.UUID, enabled bool) error {
	return s.repo.SetAlerts(ctx, userID, id, enabled)
}

// RunAlerts replays every alert-enabled saved search and notifies its owner of
// listings created since the last alert. Intended to be called periodically.
func (s *Service) RunAlerts(ctx context.Context) error {
	searches, err := s.repo.ListAlertable(ctx)
	if err != nil {
		return err
	}
	for _, ss := range searches {
		crit := parseCriteria(ss.Query)
		crit.Page = shared.NewPage(50, 0)
		res, err := s.search.Search(ctx, crit, nil)
		if err != nil {
			slog.Warn("saved-search alert: search failed", "search", ss.ID, "error", err)
			continue
		}
		newCount := 0
		for _, p := range res.Items {
			if p.CreatedAt.After(ss.LastNotifiedAt) {
				newCount++
			}
		}
		if newCount == 0 {
			continue
		}
		body := fmt.Sprintf("%d new place(s) match your saved search “%s”.", newCount, ss.Name)
		if err := s.notifier.Notify(ctx, ss.UserID, "New stays match your search", body, ss.ID); err != nil {
			slog.Warn("saved-search alert: notify failed", "search", ss.ID, "error", err)
			continue
		}
		if err := s.repo.MarkNotified(ctx, ss.ID, time.Now().UTC()); err != nil {
			slog.Warn("saved-search alert: mark-notified failed", "search", ss.ID, "error", err)
		}
	}
	return nil
}

// parseCriteria reconstructs the listing search criteria from a saved raw URL
// query, mirroring the property search handler (geo/date filters are ignored —
// alerts match on the listing facets, not transient availability).
func parseCriteria(query string) property.SearchCriteria {
	q, _ := url.ParseQuery(query)
	atoi := func(key string) int { n, _ := strconv.Atoi(q.Get(key)); return n }
	atoi64 := func(key string) int64 { n, _ := strconv.ParseInt(q.Get(key), 10, 64); return n }
	return property.SearchCriteria{
		Query:           strings.TrimSpace(q.Get("q")),
		City:            q.Get("city"),
		Country:         q.Get("country"),
		Type:            property.PropertyType(q.Get("type")),
		MinGuests:       atoi("minGuests"),
		MinPrice:        atoi64("minPrice"),
		MaxPrice:        atoi64("maxPrice"),
		MinBedrooms:     atoi("bedrooms"),
		MinBeds:         atoi("beds"),
		MinBathrooms:    atoi("bathrooms"),
		InstantBookOnly: q.Get("instantBook") == "true",
		Amenities:       property.NormalizeAmenities(q["amenity"]),
	}
}
