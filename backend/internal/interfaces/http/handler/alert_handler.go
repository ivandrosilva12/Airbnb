package handler

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	alertingapp "github.com/airhost/backend/internal/application/alerting"
	alertstateapp "github.com/airhost/backend/internal/application/alertstate"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/gin-gonic/gin"
)

// AlertHandler exposes endpoints around alerting: admin management of
// Alertmanager silences (maintenance mute windows), an Alertmanager webhook
// receiver that records the latest alert states, and an admin read of those
// states so the internal UI can reflect firing/resolved (not just e-mail/Slack).
type AlertHandler struct {
	svc          *alertingapp.Service
	state        *alertstateapp.Service
	webhookToken string
}

// NewAlertHandler builds an AlertHandler. state may be nil to disable the
// alert-state view (the silence endpoints still work). webhookToken, when
// non-empty, is required as a bearer on the Alertmanager webhook receiver.
func NewAlertHandler(svc *alertingapp.Service, state *alertstateapp.Service, webhookToken string) *AlertHandler {
	return &AlertHandler{svc: svc, state: state, webhookToken: webhookToken}
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
	if !bindJSON(c, &req) {
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

// --- Alertmanager webhook receiver + alert-state view -----------------------

type alertmanagerAlert struct {
	Status      string            `json:"status"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
}

type alertmanagerNotification struct {
	Status string              `json:"status"`
	Alerts []alertmanagerAlert `json:"alerts"`
}

// IngestNotification receives an Alertmanager webhook (firing or resolved) and
// records the latest state of each alert. It always answers 200 so Alertmanager
// does not retry. This route is authenticated by network placement (internal),
// not a user token, so it lives outside the auth group.
func (h *AlertHandler) IngestNotification(c *gin.Context) {
	if !h.authorizeWebhook(c) {
		response.FailMessage(c, http.StatusUnauthorized, "invalid alert webhook token")
		return
	}
	if h.state == nil {
		response.OK(c, gin.H{"status": "ignored"})
		return
	}
	var req alertmanagerNotification
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, "invalid alert payload")
		return
	}
	n := alertstateapp.Notification{Status: req.Status, Alerts: make([]alertstateapp.Alert, 0, len(req.Alerts))}
	for _, a := range req.Alerts {
		n.Alerts = append(n.Alerts, alertstateapp.Alert{
			Status:      a.Status,
			Fingerprint: a.Fingerprint,
			Labels:      a.Labels,
			Annotations: a.Annotations,
			StartsAt:    a.StartsAt,
			EndsAt:      a.EndsAt,
		})
	}
	h.state.Ingest(n)
	response.OK(c, gin.H{"status": "ok", "received": len(n.Alerts)})
}

// ListAlerts returns the latest alert states (firing + recently resolved) so the
// admin console can reflect them. Admin only.
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	if h.state == nil {
		response.OK(c, gin.H{"items": []dto.AlertStateView{}})
		return
	}
	states := h.state.List()
	items := make([]dto.AlertStateView, 0, len(states))
	for _, s := range states {
		items = append(items, dto.FromAlertState(s))
	}
	response.OK(c, gin.H{"items": items})
}

// authorizeWebhook reports whether the alert-webhook request carries the
// configured bearer token. When no token is configured the route is open
// (trusted by network placement).
func (h *AlertHandler) authorizeWebhook(c *gin.Context) bool {
	if h.webhookToken == "" {
		return true
	}
	got := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.webhookToken)) == 1
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
		// Most likely the upstream Alertmanager is unreachable. Log the detail
		// server-side; return a generic 502 without leaking internals.
		logctx.LoggerFrom(c.Request.Context()).Warn("alerting: upstream request failed", "error", err)
		response.FailMessage(c, http.StatusBadGateway, "alert manager is unavailable")
	}
}
