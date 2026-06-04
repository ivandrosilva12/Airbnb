package handler

import (
	"net/http"
	"strconv"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OutboxAdminHandler exposes the dead-letter queue plus a requeue affordance
// to admins. The interface for both lives on event.DeadLetterStore (S97); this
// handler is the thin HTTP layer over ListDeadLettered + Requeue + PendingCount.
//
// Routes are mounted on /admin so the existing RequireAdmin middleware gates
// them — operators only.
type OutboxAdminHandler struct {
	dlq event.DeadLetterStore
}

// NewOutboxAdminHandler wires the handler. dlq is the production
// OutboxRepository (postgres.OutboxRepository implements DeadLetterStore).
func NewOutboxAdminHandler(dlq event.DeadLetterStore) *OutboxAdminHandler {
	return &OutboxAdminHandler{dlq: dlq}
}

// outboxDLQItem mirrors event.DeadLetteredRecord in the wire shape — the
// payload is base64-friendly JSON so the admin UI can inspect what was queued
// (subscriber bug investigations are 90% reading the payload).
type outboxDLQItem struct {
	ID             string `json:"id"`
	EventName      string `json:"eventName"`
	Payload        string `json:"payload"`
	CreatedAt      string `json:"createdAt"`
	Attempts       int    `json:"attempts"`
	DeadLetteredAt string `json:"deadLetteredAt"`
	LastError      string `json:"lastError"`
}

// outboxDLQPage is the list response — also carries the current pending count
// so the admin can spot a stuck relay (large pending) alongside DLQ growth.
type outboxDLQPage struct {
	Items   []outboxDLQItem `json:"items"`
	Pending int             `json:"pending"`
}

// List returns dead-lettered records newest-first, paginated by ?limit&offset.
// Defaults: limit=50, offset=0; limit is capped at 200 to keep the response
// bounded.
func (h *OutboxAdminHandler) List(c *gin.Context) {
	limit := atoiOr(c.Query("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	offset := atoiOr(c.Query("offset"), 0)
	ctx := c.Request.Context()
	recs, err := h.dlq.ListDeadLettered(ctx, limit, offset)
	if err != nil {
		response.FailMessage(c, http.StatusInternalServerError, "list dlq failed: "+err.Error())
		return
	}
	pending, _ := h.dlq.PendingCount(ctx) // best-effort — a count failure shouldn't break the listing
	items := make([]outboxDLQItem, 0, len(recs))
	for _, r := range recs {
		items = append(items, outboxDLQItem{
			ID:             r.Record.ID.String(),
			EventName:      r.Record.Name,
			Payload:        string(r.Record.Payload),
			CreatedAt:      r.Record.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Attempts:       r.Record.Attempts,
			DeadLetteredAt: r.DeadLetteredAt.UTC().Format("2006-01-02T15:04:05Z"),
			LastError:      r.LastError,
		})
	}
	c.JSON(http.StatusOK, outboxDLQPage{Items: items, Pending: pending})
}

// Requeue clears the dead-letter state for a record so the next recovery cycle
// picks it up again. Idempotent — requeuing an already-live record is a no-op.
func (h *OutboxAdminHandler) Requeue(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.dlq.Requeue(c.Request.Context(), id); err != nil {
		response.FailMessage(c, http.StatusInternalServerError, "requeue failed: "+err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// atoiOr returns the integer value of s, or fallback if s is empty or invalid.
func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
