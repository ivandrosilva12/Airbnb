// Package messagetemplate is the bounded context for a host's saved-reply
// playbook. A Template is a short, host-authored message that the messaging
// composer can stamp into the input — extending the built-in canned replies
// with snippets tailored to the host's listings (e.g. "Here's the lockbox
// code: 1234").
//
// Templates are author-scoped: only the owning host can read, edit or
// delete them. The HTTP layer pairs that with a "list mine" endpoint the
// composer hits to render the host's chips.
package messagetemplate

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Field length caps. Label is short (a tab/chip label); Body matches the
// 4000-char ceiling on conversation messages so a template can be sent as-is.
const (
	MaxLabelLength = 60
	MaxBodyLength  = 4000
)

// Template is the aggregate root.
type Template struct {
	ID        uuid.UUID
	HostID    uuid.UUID
	Label     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New builds a template after trimming/validating the label and body.
func New(hostID uuid.UUID, label, body string) (*Template, error) {
	label = strings.TrimSpace(label)
	body = strings.TrimSpace(body)
	if err := validateFields(label, body); err != nil {
		return nil, err
	}
	if hostID == uuid.Nil {
		return nil, shared.NewValidationError("hostID is required")
	}
	now := time.Now().UTC()
	return &Template{
		ID:        uuid.New(),
		HostID:    hostID,
		Label:     label,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Update replaces the label and body in place, re-running the same
// validations as New. Touches UpdatedAt on success.
func (t *Template) Update(label, body string) error {
	label = strings.TrimSpace(label)
	body = strings.TrimSpace(body)
	if err := validateFields(label, body); err != nil {
		return err
	}
	t.Label = label
	t.Body = body
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// IsOwnedBy reports whether the given user is the template's author.
func (t *Template) IsOwnedBy(userID uuid.UUID) bool { return t.HostID == userID }

func validateFields(label, body string) error {
	if label == "" {
		return shared.NewValidationError("label is required")
	}
	if len(label) > MaxLabelLength {
		return shared.NewValidationError("label is too long")
	}
	if body == "" {
		return shared.NewValidationError("body is required")
	}
	if len(body) > MaxBodyLength {
		return shared.NewValidationError("body is too long")
	}
	return nil
}
