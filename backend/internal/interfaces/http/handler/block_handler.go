package handler

import (
	"net/http"
	"strings"
	"time"

	blockapp "github.com/airhost/backend/internal/application/block"
	"github.com/airhost/backend/internal/infrastructure/ical"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// BlockHandler exposes host calendar-block endpoints.
type BlockHandler struct {
	svc *blockapp.Service
}

// NewBlockHandler builds a BlockHandler.
func NewBlockHandler(svc *blockapp.Service) *BlockHandler { return &BlockHandler{svc: svc} }

type createBlockRequest struct {
	From   string `json:"from" binding:"required"`
	To     string `json:"to" binding:"required"`
	Reason string `json:"reason"`
}

// Create blocks a date range on a listing the host owns.
func (h *BlockHandler) Create(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req createBlockRequest
	if !bindJSON(c, &req) {
		return
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "from must be YYYY-MM-DD")
		return
	}
	to, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "to must be YYYY-MM-DD")
		return
	}
	b, err := h.svc.Create(c.Request.Context(), hostID, propertyID, from, to, req.Reason)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromBlock(b))
}

// ListForProperty returns the host's blocks on a listing they own.
func (h *BlockHandler) ListForProperty(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	res, err := h.svc.ListForHost(c.Request.Context(), hostID, propertyID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.BlockView, 0, len(res.Items))
	for _, b := range res.Items {
		items = append(items, dto.FromBlock(b))
	}
	response.OK(c, dto.PageView[dto.BlockView]{Items: items, Total: res.Total})
}

type importCalendarRequest struct {
	ICal string `json:"ical" binding:"required"`
}

// ImportCalendar parses an external iCalendar feed (pasted or fetched by the
// client) and blocks its busy ranges on a listing the host owns. Idempotent:
// ranges already blocked are skipped.
func (h *BlockHandler) ImportCalendar(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req importCalendarRequest
	if !bindJSON(c, &req) {
		return
	}
	events, err := ical.Parse([]byte(req.ICal))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not parse iCalendar data")
		return
	}
	ranges := make([]blockapp.ImportRange, 0, len(events))
	for _, e := range events {
		reason := "Imported"
		if s := strings.TrimSpace(e.Summary); s != "" {
			reason = "Imported: " + s
		}
		ranges = append(ranges, blockapp.ImportRange{From: e.Start, To: e.End, Reason: reason})
	}
	result, err := h.svc.Import(c.Request.Context(), hostID, propertyID, ranges)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// S168 — surface confirmed-booking conflicts so the host's iCal-import UI
	// can show "8 imported, 2 conflicts: 2026-08-10..15 already booked" instead
	// of silently dropping the dates. SkippedBookingConflict is rendered as an
	// empty array (never null) for stable JSON.
	conflicts := make([]gin.H, 0, len(result.SkippedBookingConflict))
	for _, r := range result.SkippedBookingConflict {
		conflicts = append(conflicts, gin.H{
			"from": r.From.Format("2006-01-02"),
			"to":   r.To.Format("2006-01-02"),
		})
	}
	response.OK(c, gin.H{
		"created":                result.Created,
		"skippedBlockOverlap":    result.SkippedBlockOverlap,
		"skippedBookingConflict": conflicts,
		"found":                  len(events),
		// Back-compat: existing clients keyed off "imported".
		"imported": result.Created,
	})
}

// Delete removes a block on a listing the host owns.
func (h *BlockHandler) Delete(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), hostID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
