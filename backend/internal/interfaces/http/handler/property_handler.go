package handler

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	propertyapp "github.com/airhost/backend/internal/application/property"
	searchapp "github.com/airhost/backend/internal/application/search"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Photo-upload limits. The content type is detected from the bytes (not trusted
// from the client header) so the stored object cannot be served as active
// content (e.g. text/html, SVG) from the public URL.
const maxPhotoUploadBytes = 10 << 20 // 10 MiB

var allowedPhotoContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// PropertyHandler exposes listing endpoints.
type PropertyHandler struct {
	svc     *propertyapp.Service
	search  *searchapp.Service
	metrics *observability.Metrics
}

// NewPropertyHandler builds a PropertyHandler.
func NewPropertyHandler(svc *propertyapp.Service, search *searchapp.Service, m *observability.Metrics) *PropertyHandler {
	return &PropertyHandler{svc: svc, search: search, metrics: m}
}

type createPropertyRequest struct {
	Title              string   `json:"title" binding:"required"`
	Description        string   `json:"description"`
	Type               string   `json:"type" binding:"required"`
	AddressLine1       string   `json:"addressLine1"`
	City               string   `json:"city" binding:"required"`
	Country            string   `json:"country" binding:"required"`
	PostalCode         string   `json:"postalCode"`
	Latitude           float64  `json:"latitude"`
	Longitude          float64  `json:"longitude"`
	PriceCents         int64    `json:"priceCents" binding:"required"`
	CleaningFeeCents   int64    `json:"cleaningFeeCents"`
	Currency           string   `json:"currency" binding:"required"`
	MaxGuests          int      `json:"maxGuests" binding:"required"`
	Bedrooms           int      `json:"bedrooms"`
	Beds               int      `json:"beds"`
	Bathrooms          int      `json:"bathrooms"`
	Amenities          []string `json:"amenities"`
	CancellationPolicy string   `json:"cancellationPolicy"`
	WeeklyDiscountPct  float64  `json:"weeklyDiscountPct"`
	MonthlyDiscountPct float64  `json:"monthlyDiscountPct"`
	TaxRatePct         float64  `json:"taxRatePct"`
}

// Create publishes a new draft listing for the authenticated host.
func (h *PropertyHandler) Create(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createPropertyRequest
	if !bindJSON(c, &req) {
		return
	}
	p, err := h.svc.Create(c.Request.Context(), propertyapp.CreateInput{
		HostID:             hostID,
		Title:              req.Title,
		Description:        req.Description,
		Type:               req.Type,
		AddressLine1:       req.AddressLine1,
		City:               req.City,
		Country:            req.Country,
		PostalCode:         req.PostalCode,
		Latitude:           req.Latitude,
		Longitude:          req.Longitude,
		PriceCents:         req.PriceCents,
		CleaningFeeCents:   req.CleaningFeeCents,
		Currency:           req.Currency,
		MaxGuests:          req.MaxGuests,
		Bedrooms:           req.Bedrooms,
		Beds:               req.Beds,
		Bathrooms:          req.Bathrooms,
		Amenities:          req.Amenities,
		CancellationPolicy: req.CancellationPolicy,
		WeeklyDiscountPct:  req.WeeklyDiscountPct,
		MonthlyDiscountPct: req.MonthlyDiscountPct,
		TaxRatePct:         req.TaxRatePct,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.metrics.PropertiesCreated.Inc()
	response.Created(c, dto.FromProperty(p))
}

// Search runs a public, filtered listing search. When checkIn/checkOut query
// params are supplied, listings already booked for that window are excluded;
// when lat/lng/radiusKm are supplied, results are limited to that radius.
func (h *PropertyHandler) Search(c *gin.Context) {
	maxPrice, _ := strconv.ParseInt(c.Query("maxPrice"), 10, 64)
	minGuests, _ := strconv.Atoi(c.Query("minGuests"))
	criteria := property.SearchCriteria{
		Query:     strings.TrimSpace(c.Query("q")),
		City:      c.Query("city"),
		Country:   c.Query("country"),
		Type:      property.PropertyType(c.Query("type")),
		MinGuests: minGuests,
		MaxPrice:  maxPrice,
		Amenities: property.NormalizeAmenities(c.QueryArray("amenity")),
		Sort:      property.Sort(c.Query("sort")),
		Page:      pageFromQuery(c),
	}

	lat, errLat := strconv.ParseFloat(c.Query("lat"), 64)
	lng, errLng := strconv.ParseFloat(c.Query("lng"), 64)
	radiusKm, _ := strconv.ParseFloat(c.Query("radiusKm"), 64)
	if errLat == nil && errLng == nil && radiusKm > 0 {
		criteria.Geo = &property.GeoFilter{Lat: lat, Lng: lng, RadiusKm: radiusKm}
	}

	var window *searchapp.DateWindow
	checkIn, errIn := time.Parse("2006-01-02", c.Query("checkIn"))
	checkOut, errOut := time.Parse("2006-01-02", c.Query("checkOut"))
	if errIn == nil && errOut == nil {
		window = &searchapp.DateWindow{CheckIn: checkIn.UTC(), CheckOut: checkOut.UTC()}
	}

	res, err := h.search.Search(c.Request.Context(), criteria, window)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PropertyView, 0, len(res.Items))
	for _, p := range res.Items {
		items = append(items, dto.FromProperty(p))
	}
	response.OK(c, dto.PageView[dto.PropertyView]{
		Items: items, Total: res.Total, Limit: criteria.Page.Limit, Offset: criteria.Page.Offset,
	})
}

// Amenities returns the canonical amenity codes (public), so clients render the
// same set in the create form and the search filter.
func (h *PropertyHandler) Amenities(c *gin.Context) {
	response.OK(c, gin.H{"amenities": property.CanonicalAmenities})
}

// Get returns a single listing.
func (h *PropertyHandler) Get(c *gin.Context) {
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

type updatePropertyRequest struct {
	Title              string  `json:"title" binding:"required"`
	Description        string  `json:"description"`
	PriceCents         int64   `json:"priceCents" binding:"required"`
	CleaningFeeCents   int64   `json:"cleaningFeeCents"`
	Currency           string  `json:"currency" binding:"required"`
	MaxGuests          int     `json:"maxGuests" binding:"required"`
	CancellationPolicy string  `json:"cancellationPolicy"`
	WeeklyDiscountPct  float64 `json:"weeklyDiscountPct"`
	MonthlyDiscountPct float64 `json:"monthlyDiscountPct"`
	TaxRatePct         float64 `json:"taxRatePct"`
}

// Update edits an owned listing.
func (h *PropertyHandler) Update(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req updatePropertyRequest
	if !bindJSON(c, &req) {
		return
	}
	p, err := h.svc.Update(c.Request.Context(), actorID, id, propertyapp.UpdateInput{
		Title:              req.Title,
		Description:        req.Description,
		PriceCents:         req.PriceCents,
		CleaningFeeCents:   req.CleaningFeeCents,
		Currency:           req.Currency,
		MaxGuests:          req.MaxGuests,
		CancellationPolicy: req.CancellationPolicy,
		WeeklyDiscountPct:  req.WeeklyDiscountPct,
		MonthlyDiscountPct: req.MonthlyDiscountPct,
		TaxRatePct:         req.TaxRatePct,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

// Publish publishes an owned listing.
func (h *PropertyHandler) Publish(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.Publish(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

// Delete removes an owned listing.
func (h *PropertyHandler) Delete(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), actorID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// AdminSuspend hides a listing from search (admin only).
func (h *PropertyHandler) AdminSuspend(c *gin.Context) {
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.Suspend(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

// AdminUnsuspend restores a suspended listing to published (admin only).
func (h *PropertyHandler) AdminUnsuspend(c *gin.Context) {
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.Unsuspend(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

// ListMine returns the authenticated host's listings.
func (h *PropertyHandler) ListMine(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListByHost(c.Request.Context(), hostID, pageFromQuery(c))
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

// UploadPhoto attaches an uploaded image to an owned listing.
func (h *PropertyHandler) UploadPhoto(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	// Cap the total request body so an oversized multipart upload is rejected
	// while parsing, not buffered whole.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoUploadBytes+1<<10)
	fileHeader, err := c.FormFile("photo")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.FailMessage(c, http.StatusRequestEntityTooLarge, "photo exceeds the 10 MiB limit")
			return
		}
		response.FailMessage(c, http.StatusBadRequest, "photo file is required")
		return
	}
	if fileHeader.Size > maxPhotoUploadBytes {
		response.FailMessage(c, http.StatusRequestEntityTooLarge, "photo exceeds the 10 MiB limit")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not read uploaded file")
		return
	}
	defer file.Close()

	// Detect the content type from the leading bytes rather than trusting the
	// client-supplied header, then rewind so the full file is stored.
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	contentType := http.DetectContentType(head[:n])
	if !allowedPhotoContentTypes[contentType] {
		response.FailMessage(c, http.StatusUnsupportedMediaType, "only JPEG, PNG, WebP or GIF images are allowed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not read uploaded file")
		return
	}
	p, err := h.svc.UploadPhoto(c.Request.Context(), actorID, id, file, fileHeader.Size, contentType, fileHeader.Filename)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

// DeletePhoto detaches a photo from an owned listing.
func (h *PropertyHandler) DeletePhoto(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	photoID, ok := pathUUID(c, "photoId")
	if !ok {
		return
	}
	p, err := h.svc.RemovePhoto(c.Request.Context(), actorID, id, photoID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

type reorderPhotosRequest struct {
	PhotoIDs []string `json:"photoIds" binding:"required"`
}

// ReorderPhotos sets the photo order (first is the cover) for an owned listing.
func (h *PropertyHandler) ReorderPhotos(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req reorderPhotosRequest
	if !bindJSON(c, &req) {
		return
	}
	orderedIDs := make([]uuid.UUID, 0, len(req.PhotoIDs))
	for _, raw := range req.PhotoIDs {
		pid, err := uuid.Parse(raw)
		if err != nil {
			response.FailMessage(c, http.StatusBadRequest, "invalid photo id")
			return
		}
		orderedIDs = append(orderedIDs, pid)
	}
	p, err := h.svc.ReorderPhotos(c.Request.Context(), actorID, id, orderedIDs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromProperty(p))
}

type presignRequest struct {
	Filename string `json:"filename" binding:"required"`
}

// PresignPhotoUpload returns a direct-upload URL for the listing.
func (h *PropertyHandler) PresignPhotoUpload(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req presignRequest
	if !bindJSON(c, &req) {
		return
	}
	url, key, err := h.svc.PresignPhotoUpload(c.Request.Context(), actorID, id, path.Ext(req.Filename))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"uploadUrl": url, "objectKey": key})
}
