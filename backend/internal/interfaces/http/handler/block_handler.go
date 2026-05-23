package handler

import (
	"net/http"
	"time"

	blockapp "github.com/airhost/backend/internal/application/block"
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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
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
