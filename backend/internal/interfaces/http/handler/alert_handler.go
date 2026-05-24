package handler

import (
	"errors"
	"net/http"
	"time"

	alertingapp "github.com/airhost/backend/internal/application/alerting"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// AlertHandler exposes admin endpoints to manage Alertmanager silences —
// maintenance windows that mute matching alerts.
type AlertHandler struct {
	svc *alertingapp.Service
}

// NewAlertHandler builds an AlertHandler.
func NewAlertHandler(svc *alertingapp.Service) *AlertHandler {
	return &AlertHandler{svc: svc}
}

type silenceMatcherRequest struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual"` // pointer so an omitted field defaults to true
}

type createSilenceRequest struct {
	Matchers        []silenceMatcherRequest `json:"matchers"`
	DurationMinutes int                     `json:"durationMinutes"`
	StartsAt        *time.Time              `json:"startsAt"`
	EndsAt          *time.Time              `json:"endsAt"`
	Comment         string                  `json:"comment"`
}

// CreateSilence registers a new silence (admin action).
func (h *AlertHandler) CreateSilence(c *gin.Context) {
	var req createSilenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
		return
	}

	in := alertingapp.CreateSilenceInput{
		Matchers:  make([]port.SilenceMatcher, 0, len(req.Matchers)),
		Comment:   req.Comment,
		CreatedBy: currentActor(c),
	}
	for _, m := range req.Matchers {
		isEqual := true
		if m.IsEqual != nil {
			isEqual = *m.IsEqual
		}
		in.Matchers = append(in.Matchers, port.SilenceMatcher{
			Name:    m.Name,
			Value:   m.Value,
			IsRegex: m.IsRegex,
			IsEqual: isEqual,
		})
	}
	if req.StartsAt != nil {
		in.StartsAt = *req.StartsAt
	}
	if req.EndsAt != nil {
		in.EndsAt = *req.EndsAt
	}
	if req.DurationMinutes > 0 {
		in.Duration = time.Duration(req.DurationMinutes) * time.Minute
	}

	s, err := h.svc.CreateSilence(c.Request.Context(), in)
	if err != nil {
		failAlerting(c, err)
		return
	}
	response.Created(c, dto.FromSilence(s))
}

// ListSilences returns the current silences (admin action).
func (h *AlertHandler) ListSilences(c *gin.Context) {
	silences, err := h.svc.ListSilences(c.Request.Context())
	if err != nil {
		failAlerting(c, err)
		return
	}
	items := make([]dto.SilenceView, 0, len(silences))
	for _, s := range silences {
		items = append(items, dto.FromSilence(s))
	}
	response.OK(c, gin.H{"items": items})
}

// DeleteSilence expires a silence by id (admin action).
func (h *AlertHandler) DeleteSilence(c *gin.Context) {
	if err := h.svc.DeleteSilence(c.Request.Context(), c.Param("id")); err != nil {
		failAlerting(c, err)
		return
	}
	response.NoContent(c)
}

// currentActor returns a human label for who performed the action, preferring
// the e-mail and falling back to the user id.
func currentActor(c *gin.Context) string {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		return ""
	}
	if u.Email != "" {
		return u.Email
	}
	return u.ID.String()
}

// failAlerting maps alerting errors: validation → 422, not-found → 404, and any
// other failure (most likely the upstream Alertmanager being unreachable) → 502.
func failAlerting(c *gin.Context, err error) {
	switch {
	case errors.Is(err, shared.ErrValidation):
		response.Fail(c, err)
	case errors.Is(err, shared.ErrNotFound):
		response.Fail(c, err)
	default:
		response.FailMessage(c, http.StatusBadGateway, err.Error())
	}
}
