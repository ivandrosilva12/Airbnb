package handler

import (
	auditapp "github.com/airhost/backend/internal/application/audit"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditHandler exposes the admin-only read surface over the audit trail
// (S45). Writes happen implicitly when other admin handlers complete
// their actions — never via a public endpoint, which is why this file
// has no Create.
type AuditHandler struct {
	svc *auditapp.Service
}

// NewAuditHandler wires an AuditHandler.
func NewAuditHandler(svc *auditapp.Service) *AuditHandler {
	return &AuditHandler{svc: svc}
}

// List returns audit events filtered by the query params:
//   actorId=<uuid>      who performed the action
//   action=<string>     exact action (e.g. property.suspend)
//   targetType=<string> property|identity|report|dispute|coupon
//   targetId=<uuid>     the specific affected aggregate
//
// All filters AND together. Paginated via the standard pageFromQuery
// helper (limit/offset on the querystring), newest first.
func (h *AuditHandler) List(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	var f audit.Filter
	if raw := c.Query("actorId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.FailMessage(c, 400, "actorId must be a UUID")
			return
		}
		f.ActorID = id
	}
	if raw := c.Query("action"); raw != "" {
		f.Action = audit.Action(raw)
	}
	if raw := c.Query("targetType"); raw != "" {
		f.TargetType = audit.TargetType(raw)
	}
	if raw := c.Query("targetId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.FailMessage(c, 400, "targetId must be a UUID")
			return
		}
		f.TargetID = id
	}
	res, err := h.svc.List(c.Request.Context(), f, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	// Lightweight shaping: expose the typed enums as strings (the wire
	// shape the admin UI will work with) and surface metadata as the
	// raw map.
	items := make([]gin.H, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, gin.H{
			"id":         e.ID,
			"actorId":    e.ActorID,
			"action":     string(e.Action),
			"targetType": string(e.TargetType),
			"targetId":   e.TargetID,
			"metadata":   e.Metadata,
			"requestId":  e.RequestID,
			"createdAt":  e.CreatedAt,
		})
	}
	response.OK(c, gin.H{"items": items, "total": res.Total})
}
