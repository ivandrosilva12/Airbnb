// Package alerting is the application service for operating the alerting stack:
// today it manages Alertmanager silences (maintenance windows that mute alerts)
// through the AlertSilencer port.
package alerting

import (
	"context"
	"strings"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
)

// maxSilenceDuration caps how far in the future a silence may end, so a typo
// cannot mute alerts for months.
const maxSilenceDuration = 30 * 24 * time.Hour

// Service validates silence requests and delegates to the AlertSilencer port.
type Service struct {
	silencer port.AlertSilencer
	now      func() time.Time
}

// NewService builds a Service.
func NewService(silencer port.AlertSilencer) *Service {
	return &Service{silencer: silencer, now: time.Now}
}

// CreateSilenceInput is the validated input for creating a silence. Exactly one
// of Duration or EndsAt should be supplied; if both are zero the request is
// rejected. StartsAt defaults to now when zero.
type CreateSilenceInput struct {
	Matchers  []port.SilenceMatcher
	StartsAt  time.Time
	Duration  time.Duration
	EndsAt    time.Time
	CreatedBy string
	Comment   string
}

// CreateSilence validates the request, defaults the time window and creates the
// silence, returning it with its assigned id and computed status.
func (s *Service) CreateSilence(ctx context.Context, in CreateSilenceInput) (port.Silence, error) {
	now := s.now().UTC()

	starts := in.StartsAt.UTC()
	if in.StartsAt.IsZero() {
		starts = now
	}
	ends := in.EndsAt.UTC()
	if in.EndsAt.IsZero() {
		if in.Duration <= 0 {
			return port.Silence{}, shared.NewValidationError("a silence needs an endsAt or a positive durationMinutes")
		}
		ends = starts.Add(in.Duration)
	}

	matchers, err := normalizeMatchers(in.Matchers)
	if err != nil {
		return port.Silence{}, err
	}
	comment := strings.TrimSpace(in.Comment)
	if comment == "" {
		return port.Silence{}, shared.NewValidationError("a silence needs a comment describing why")
	}
	createdBy := strings.TrimSpace(in.CreatedBy)
	if createdBy == "" {
		createdBy = "airhost-admin"
	}
	if !ends.After(starts) {
		return port.Silence{}, shared.NewValidationError("silence endsAt must be after startsAt")
	}
	if ends.After(now.Add(maxSilenceDuration)) {
		return port.Silence{}, shared.NewValidationError("a silence may not last longer than 30 days")
	}

	ns := port.NewSilence{
		Matchers:  matchers,
		StartsAt:  starts,
		EndsAt:    ends,
		CreatedBy: createdBy,
		Comment:   comment,
	}
	id, err := s.silencer.Create(ctx, ns)
	if err != nil {
		return port.Silence{}, err
	}
	return port.Silence{
		ID:        id,
		Matchers:  matchers,
		StartsAt:  starts,
		EndsAt:    ends,
		CreatedBy: createdBy,
		Comment:   comment,
		Status:    statusFor(now, starts, ends),
	}, nil
}

// ListSilences returns the current silences from the upstream manager.
func (s *Service) ListSilences(ctx context.Context) ([]port.Silence, error) {
	return s.silencer.List(ctx)
}

// DeleteSilence expires the silence with the given id.
func (s *Service) DeleteSilence(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return shared.NewValidationError("silence id is required")
	}
	return s.silencer.Delete(ctx, id)
}

// normalizeMatchers trims and validates matchers, defaulting IsEqual to true.
func normalizeMatchers(in []port.SilenceMatcher) ([]port.SilenceMatcher, error) {
	if len(in) == 0 {
		return nil, shared.NewValidationError("a silence needs at least one matcher")
	}
	out := make([]port.SilenceMatcher, 0, len(in))
	for _, m := range in {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			return nil, shared.NewValidationError("every matcher needs a name")
		}
		// An empty value is only meaningful as a regex (e.g. ".+"); otherwise a
		// blank value would match nothing useful and is almost always a mistake.
		if strings.TrimSpace(m.Value) == "" && !m.IsRegex {
			return nil, shared.NewValidationError("matcher " + name + " needs a value")
		}
		out = append(out, port.SilenceMatcher{
			Name:    name,
			Value:   m.Value,
			IsRegex: m.IsRegex,
			IsEqual: m.IsEqual,
		})
	}
	return out, nil
}

// statusFor derives the human status of a silence from its window.
func statusFor(now, starts, ends time.Time) string {
	switch {
	case now.Before(starts):
		return "pending"
	case now.After(ends):
		return "expired"
	default:
		return "active"
	}
}
