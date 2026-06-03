package handler

import (
	"net/http"
	"strconv"

	experienceapp "github.com/airhost/backend/internal/application/experience"
	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// ExperienceHandler exposes the Experience BC endpoints (S71). Public:
// GET /experiences (search), GET /experiences/:id. Host: POST /experiences,
// PATCH /experiences/:id, POST /experiences/:id/publish, POST /experiences/
// :id/suspend, DELETE /experiences/:id, POST /experiences/:id/photos,
// GET /host/experiences.
type ExperienceHandler struct {
	svc *experienceapp.Service
}

// NewExperienceHandler builds an ExperienceHandler.
func NewExperienceHandler(svc *experienceapp.Service) *ExperienceHandler {
	return &ExperienceHandler{svc: svc}
}

type experienceAddressRequest struct {
	City      string  `json:"city" binding:"required"`
	Country   string  `json:"country" binding:"required"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (a experienceAddressRequest) toDomain() experience.Address {
	return experience.Address{City: a.City, Country: a.Country, Latitude: a.Latitude, Longitude: a.Longitude}
}

type createExperienceRequest struct {
	Title              string                   `json:"title" binding:"required"`
	Description        string                   `json:"description"`
	Category           string                   `json:"category" binding:"required"`
	Address            experienceAddressRequest `json:"address" binding:"required"`
	DurationMinutes    int                      `json:"durationMinutes" binding:"required"`
	MaxGuests          int                      `json:"maxGuests" binding:"required"`
	PricePerGuestCents int64                    `json:"pricePerGuestCents" binding:"required"`
	Currency           string                   `json:"currency" binding:"required"`
	Language           string                   `json:"language" binding:"required"`
}

// Create drafts a new experience for the authenticated host.
//
//	POST /api/v1/experiences
func (h *ExperienceHandler) Create(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createExperienceRequest
	if !bindJSON(c, &req) {
		return
	}
	price, err := shared.NewMoney(req.PricePerGuestCents, req.Currency)
	if err != nil {
		response.Fail(c, err)
		return
	}
	e, err := h.svc.Create(c.Request.Context(), experienceapp.CreateInput{
		HostID: hostID, Title: req.Title, Description: req.Description,
		Category: experience.Category(req.Category), Address: req.Address.toDomain(),
		DurationMinutes: req.DurationMinutes, MaxGuests: req.MaxGuests,
		PricePerGuest: price, Language: req.Language,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromExperience(e))
}

type updateExperienceRequest struct {
	Title              string                   `json:"title" binding:"required"`
	Description        string                   `json:"description"`
	Category           string                   `json:"category" binding:"required"`
	Address            experienceAddressRequest `json:"address" binding:"required"`
	DurationMinutes    int                      `json:"durationMinutes" binding:"required"`
	MaxGuests          int                      `json:"maxGuests" binding:"required"`
	PricePerGuestCents int64                    `json:"pricePerGuestCents" binding:"required"`
	Currency           string                   `json:"currency" binding:"required"`
	Language           string                   `json:"language" binding:"required"`
}

// Update mutates an experience the actor owns.
//
//	PATCH /api/v1/experiences/:id
func (h *ExperienceHandler) Update(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	var req updateExperienceRequest
	if !bindJSON(c, &req) {
		return
	}
	price, err := shared.NewMoney(req.PricePerGuestCents, req.Currency)
	if err != nil {
		response.Fail(c, err)
		return
	}
	e, err := h.svc.Update(c.Request.Context(), experienceapp.UpdateInput{
		ID: id, ActorID: actor, Title: req.Title, Description: req.Description,
		Category: experience.Category(req.Category), Address: req.Address.toDomain(),
		DurationMinutes: req.DurationMinutes, MaxGuests: req.MaxGuests,
		PricePerGuest: price, Language: req.Language,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperience(e))
}

type addPhotoRequest struct {
	ObjectKey string `json:"objectKey" binding:"required"`
	URL       string `json:"url" binding:"required"`
}

// AddPhoto attaches a photo reference. First-class upload (multipart
// to object store) lands in a future slice; for the BC stub the host
// passes a pre-uploaded object key + URL.
//
//	POST /api/v1/experiences/:id/photos
func (h *ExperienceHandler) AddPhoto(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	var req addPhotoRequest
	if !bindJSON(c, &req) {
		return
	}
	e, err := h.svc.AddPhoto(c.Request.Context(), actor, id, req.ObjectKey, req.URL)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperience(e))
}

// Publish transitions the listing to published.
//
//	POST /api/v1/experiences/:id/publish
func (h *ExperienceHandler) Publish(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	e, err := h.svc.Publish(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperience(e))
}

// Suspend takes the listing off the catalogue.
//
//	POST /api/v1/experiences/:id/suspend
func (h *ExperienceHandler) Suspend(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	e, err := h.svc.Suspend(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperience(e))
}

// Delete drops the listing.
//
//	DELETE /api/v1/experiences/:id
func (h *ExperienceHandler) Delete(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actor, id); err != nil {
		response.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Get reads a single experience. Public.
//
//	GET /api/v1/experiences/:id
func (h *ExperienceHandler) Get(c *gin.Context) {
	id, ok := parseUUID(c, c.Param("id"), "id")
	if !ok {
		return
	}
	e, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperience(e))
}

// Search runs the public catalogue query. Empty filter returns every
// published listing, newest first.
//
//	GET /api/v1/experiences?category=&city=&country=&language=&limit=&offset=
func (h *ExperienceHandler) Search(c *gin.Context) {
	filter := experience.SearchFilter{
		City:     c.Query("city"),
		Country:  c.Query("country"),
		Language: c.Query("language"),
	}
	if cat := c.Query("category"); cat != "" {
		ct := experience.Category(cat)
		if !ct.Valid() {
			response.FailMessage(c, http.StatusBadRequest, "unknown category")
			return
		}
		filter.Category = ct
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	page := shared.NewPage(limit, offset)
	res, err := h.svc.Search(c.Request.Context(), filter, page)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ExperienceView, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, dto.FromExperience(e))
	}
	response.OK(c, gin.H{"items": items, "total": res.Total})
}

// HostList returns every experience owned by the authenticated host
// (any status).
//
//	GET /api/v1/host/experiences
func (h *ExperienceHandler) HostList(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	page := shared.NewPage(limit, offset)
	res, err := h.svc.ListByHost(c.Request.Context(), hostID, page)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ExperienceView, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, dto.FromExperience(e))
	}
	response.OK(c, gin.H{"items": items, "total": res.Total})
}
