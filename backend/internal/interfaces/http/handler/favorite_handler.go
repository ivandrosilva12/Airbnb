package handler

import (
	"net/http"
	"strings"

	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FavoriteHandler exposes wishlist endpoints.
type FavoriteHandler struct {
	svc *favoriteapp.Service
}

// NewFavoriteHandler builds a FavoriteHandler.
func NewFavoriteHandler(svc *favoriteapp.Service) *FavoriteHandler { return &FavoriteHandler{svc: svc} }

type addFavoriteRequest struct {
	PropertyID   string `json:"propertyId" binding:"required"`
	CollectionID string `json:"collectionId"`
}

// Add saves a property to the authenticated user's wishlist, optionally under a
// collection.
func (h *FavoriteHandler) Add(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	var req addFavoriteRequest
	if !bindJSON(c, &req) {
		return
	}
	propertyID, ok := parseUUID(c, req.PropertyID, "propertyId")
	if !ok {
		return
	}
	collectionID, ok := optionalCollectionID(c, req.CollectionID)
	if !ok {
		return
	}
	if err := h.svc.Add(c.Request.Context(), userID, propertyID, collectionID); err != nil {
		response.Fail(c, err)
		return
	}
	c.Status(http.StatusCreated)
}

type moveFavoriteRequest struct {
	CollectionID string `json:"collectionId"`
}

// Move files an already-saved listing under a different collection (empty
// collectionId moves it back to the default bucket).
func (h *FavoriteHandler) Move(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "propertyId")
	if !ok {
		return
	}
	var req moveFavoriteRequest
	if !bindJSON(c, &req) {
		return
	}
	collectionID, ok := optionalCollectionID(c, req.CollectionID)
	if !ok {
		return
	}
	if err := h.svc.Move(c.Request.Context(), userID, propertyID, collectionID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
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

// List returns the authenticated user's favorited listings. The optional
// collectionId query filters by collection: "none" selects the default bucket,
// a UUID selects a named collection, and omitting it lists across all.
func (h *FavoriteHandler) List(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	filter, ok := h.listFilter(c)
	if !ok {
		return
	}
	res, err := h.svc.List(c.Request.Context(), userID, filter, pageFromQuery(c))
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

// listFilter parses the collectionId query into a favorite list filter.
func (h *FavoriteHandler) listFilter(c *gin.Context) (favoriteapp.ListFilter, bool) {
	raw := strings.TrimSpace(c.Query("collectionId"))
	switch raw {
	case "":
		return favoriteapp.ListFilter{All: true}, true
	case "none", "unsorted":
		return favoriteapp.ListFilter{}, true // default bucket (nil collection)
	default:
		id, err := uuid.Parse(raw)
		if err != nil {
			response.FailMessage(c, http.StatusBadRequest, "invalid collectionId")
			return favoriteapp.ListFilter{}, false
		}
		return favoriteapp.ListFilter{CollectionID: &id}, true
	}
}

// ListCollections returns the user's wishlist collections with counts.
func (h *FavoriteHandler) ListCollections(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	cols, err := h.svc.ListCollections(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.CollectionView, 0, len(cols))
	for _, col := range cols {
		items = append(items, dto.FromCollection(col))
	}
	response.OK(c, gin.H{"items": items})
}

type createCollectionRequest struct {
	Name string `json:"name" binding:"required"`
}

// CreateCollection creates a named wishlist collection.
func (h *FavoriteHandler) CreateCollection(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createCollectionRequest
	if !bindJSON(c, &req) {
		return
	}
	col, err := h.svc.CreateCollection(c.Request.Context(), userID, req.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromNewCollection(col))
}

// DeleteCollection removes a collection; its listings fall back to the default
// bucket.
func (h *FavoriteHandler) DeleteCollection(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteCollection(c.Request.Context(), userID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// optionalCollectionID parses an optional collection id from a request body
// field: empty means the default bucket (nil); a malformed value is a 400.
func optionalCollectionID(c *gin.Context, raw string) (*uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "invalid collectionId")
		return nil, false
	}
	return &id, true
}
