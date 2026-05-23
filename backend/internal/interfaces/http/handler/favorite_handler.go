package handler

import (
	"net/http"

	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// FavoriteHandler exposes wishlist endpoints.
type FavoriteHandler struct {
	svc *favoriteapp.Service
}

// NewFavoriteHandler builds a FavoriteHandler.
func NewFavoriteHandler(svc *favoriteapp.Service) *FavoriteHandler { return &FavoriteHandler{svc: svc} }

type addFavoriteRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
}

// Add saves a property to the authenticated user's wishlist.
func (h *FavoriteHandler) Add(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	var req addFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	propertyID, ok := parseUUID(c, req.PropertyID, "propertyId")
	if !ok {
		return
	}
	if err := h.svc.Add(c.Request.Context(), userID, propertyID); err != nil {
		response.Fail(c, err)
		return
	}
	c.Status(http.StatusCreated)
}

// Remove drops a property from the wishlist.
func (h *FavoriteHandler) Remove(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "propertyId")
	if !ok {
		return
	}
	if err := h.svc.Remove(c.Request.Context(), userID, propertyID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// List returns the authenticated user's favorited listings.
func (h *FavoriteHandler) List(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.List(c.Request.Context(), userID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PropertyView, 0, len(res.Items))
	for _, p := range res.Items {
		items = append(items, dto.FromProperty(p))
	}
	response.OK(c, dto.PageView[dto.PropertyView]{Items: items, Total: res.Total})
}
