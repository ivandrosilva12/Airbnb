// Package reportapp contains listing-report use cases: a user files a report on
// a listing and an administrator resolves or dismisses it. The open-report
// queue is enriched with the reported listing's title for the moderation UI.
package reportapp

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/report"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates listing-report use cases.
type Service struct {
	reports    report.Repository
	properties property.Repository
}

// NewService wires the report application service.
func NewService(reports report.Repository, properties property.Repository) *Service {
	return &Service{reports: reports, properties: properties}
}

// EnrichedReport pairs a report with the reported listing's title for display.
type EnrichedReport struct {
	Report        *report.Report
	PropertyTitle string
}

// File records a new report against a listing. A user may not file more than one
// open report on the same listing.
func (s *Service) File(ctx context.Context, reporterID, propertyID uuid.UUID, reason string, note string) (*report.Report, error) {
	if _, err := s.properties.FindByID(ctx, propertyID); err != nil {
		return nil, err
	}
	dup, err := s.reports.HasOpenFromReporter(ctx, propertyID, reporterID)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, shared.NewValidationError("you already have an open report on this listing")
	}
	r, err := report.NewReport(propertyID, reporterID, report.Reason(reason), note)
	if err != nil {
		return nil, err
	}
	if err := s.reports.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// ListOpen returns the administrator moderation queue, enriched with listing
// titles (resolved best-effort; a deleted listing yields an empty title).
func (s *Service) ListOpen(ctx context.Context, page shared.Page) (shared.PageResult[EnrichedReport], error) {
	res, err := s.reports.ListByStatus(ctx, report.StatusOpen, page)
	if err != nil {
		return shared.PageResult[EnrichedReport]{}, err
	}
	items := make([]EnrichedReport, 0, len(res.Items))
	titles := map[uuid.UUID]string{}
	for _, r := range res.Items {
		title, ok := titles[r.PropertyID]
		if !ok {
			if p, err := s.properties.FindByID(ctx, r.PropertyID); err == nil {
				title = p.Title
			} else if !errors.Is(err, shared.ErrNotFound) {
				return shared.PageResult[EnrichedReport]{}, err
			}
			titles[r.PropertyID] = title
		}
		items = append(items, EnrichedReport{Report: r, PropertyTitle: title})
	}
	return shared.PageResult[EnrichedReport]{Items: items, Total: res.Total}, nil
}

// Resolve marks an open report acted-upon (administrator action).
func (s *Service) Resolve(ctx context.Context, adminID, reportID uuid.UUID, resolution string) (*report.Report, error) {
	return s.decide(ctx, adminID, reportID, resolution, (*report.Report).Resolve)
}

// Dismiss marks an open report as requiring no action (administrator action).
func (s *Service) Dismiss(ctx context.Context, adminID, reportID uuid.UUID, resolution string) (*report.Report, error) {
	return s.decide(ctx, adminID, reportID, resolution, (*report.Report).Dismiss)
}

func (s *Service) decide(ctx context.Context, adminID, reportID uuid.UUID, resolution string, apply func(*report.Report, uuid.UUID, string) error) (*report.Report, error) {
	r, err := s.reports.FindByID(ctx, reportID)
	if err != nil {
		return nil, err
	}
	if err := apply(r, adminID, resolution); err != nil {
		return nil, err
	}
	if err := s.reports.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}
