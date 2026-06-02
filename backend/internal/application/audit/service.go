// Package auditapp wraps the audit BC for admin-handler use. The
// service is intentionally tiny: build an Event, persist it,
// surface read access for the admin trail endpoint. Higher-level
// semantics (which actions justify an audit row, what metadata to
// capture) live at the call site in the admin handler.
package auditapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/google/uuid"
)

// Service orchestrates the audit BC. One repo dep; no events
// (audit entries are themselves the record, not a trigger).
type Service struct {
	repo audit.Repository
}

// NewService wires the audit service.
func NewService(repo audit.Repository) *Service {
	return &Service{repo: repo}
}

// RecordInput is the data needed to log one admin action. Metadata is
// optional but encouraged — context like "reason: spam" or
// "refundCents: 5000" is what makes the trail useful months later.
type RecordInput struct {
	ActorID    uuid.UUID
	Action     audit.Action
	TargetType audit.TargetType
	TargetID   uuid.UUID
	Metadata   map[string]any
}

// Record appends one event. RequestID is pulled from the context (set
// by the S32 middleware) so the audit row can be cross-referenced
// against the originating HTTP log line. Failures propagate — callers
// MUST treat a record error as a hard failure of the admin operation;
// shipping a state change without an audit row defeats the BC.
func (s *Service) Record(ctx context.Context, in RecordInput) error {
	ev, err := audit.New(in.ActorID, in.Action, in.TargetType, in.TargetID, in.Metadata, logctx.RequestIDFrom(ctx))
	if err != nil {
		return err
	}
	return s.repo.Create(ctx, ev)
}

// List returns audit events matching f, newest first. Used by the
// admin trail endpoint.
func (s *Service) List(ctx context.Context, f audit.Filter, page shared.Page) (shared.PageResult[*audit.Event], error) {
	return s.repo.List(ctx, f, page)
}
