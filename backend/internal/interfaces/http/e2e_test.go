package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	messageapp "github.com/airhost/backend/internal/application/message"
	propertyapp "github.com/airhost/backend/internal/application/property"
	reviewapp "github.com/airhost/backend/internal/application/review"
	searchapp "github.com/airhost/backend/internal/application/search"
	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/config"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	apphttp "github.com/airhost/backend/internal/interfaces/http"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

// fakeStorage is a no-op port.Storage for the HTTP e2e test.
type fakeStorage struct{}

func (fakeStorage) Upload(_ context.Context, key string, _ io.Reader, _ int64, _ string) (string, error) {
	return "http://storage.test/" + key, nil
}
func (fakeStorage) PresignedPutURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "http://storage.test/presigned/" + key, nil
}
func (fakeStorage) PublicURL(key string) string { return "http://storage.test/" + key }

// harness wires the real router against in-memory repositories and a stub auth
// middleware that resolves "Bearer <userID>" to a seeded local user.
type harness struct {
	t        *testing.T
	router   *gin.Engine
	userRepo *memory.UserRepository
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := memory.NewUserRepository()
	propertyRepo := memory.NewPropertyRepository()
	bookingRepo := memory.NewBookingRepository()
	reviewRepo := memory.NewReviewRepository()
	messageRepo := memory.NewMessageRepository()
	favoriteRepo := memory.NewFavoriteRepository()

	userSvc := userapp.NewService(userRepo)
	propertySvc := propertyapp.NewService(propertyRepo, fakeStorage{})
	bookingSvc := bookingapp.NewService(bookingRepo, propertyRepo, 0.10) // 10% service fee
	reviewSvc := reviewapp.NewService(reviewRepo, bookingRepo)
	messageSvc := messageapp.NewService(messageRepo, propertyRepo)
	searchSvc := searchapp.NewService(propertyRepo, bookingRepo)
	favoriteSvc := favoriteapp.NewService(favoriteRepo, propertyRepo)

	registry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(registry)

	authMW := func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		id, err := uuid.Parse(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bad token"})
			return
		}
		u, err := userRepo.FindByID(c.Request.Context(), id)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unknown user"})
			return
		}
		middleware.SetCurrentUser(c, u)
		c.Next()
	}

	router := apphttp.NewRouter(apphttp.Deps{
		Config:   &config.Config{App: config.AppConfig{Environment: "test"}, HTTP: config.HTTPConfig{AllowedOrigins: []string{"*"}}},
		Metrics:  metrics,
		Registry: registry,
		Auth:     authMW,
		Handlers: apphttp.Handlers{
			Health:   handler.NewHealthHandler(nil),
			User:     handler.NewUserHandler(userSvc),
			Property: handler.NewPropertyHandler(propertySvc, searchSvc, metrics),
			Booking:  handler.NewBookingHandler(bookingSvc, metrics),
			Review:   handler.NewReviewHandler(reviewSvc),
			Message:  handler.NewMessageHandler(messageSvc),
			Favorite: handler.NewFavoriteHandler(favoriteSvc),
		},
	})

	return &harness{t: t, router: router, userRepo: userRepo}
}

func (h *harness) seedUser(role domainuser.Role, email string) *domainuser.User {
	h.t.Helper()
	u, err := domainuser.NewUser("sub-"+email, email, "Test "+string(role), role)
	if err != nil {
		h.t.Fatalf("seed user: %v", err)
	}
	if err := h.userRepo.Create(context.Background(), u); err != nil {
		h.t.Fatalf("store user: %v", err)
	}
	return u
}

// do issues a JSON request as the given user (empty token = anonymous).
func (h *harness) do(method, path, token string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) decode(rec *httptest.ResponseRecorder) map[string]any {
	h.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		h.t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return m
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, label string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d, want %d (body: %s)", label, rec.Code, want, rec.Body.String())
	}
}

func TestEndToEnd_BookingAndMessagingFlow(t *testing.T) {
	h := newHarness(t)
	host := h.seedUser(domainuser.RoleHost, "host@test.dev")
	guest := h.seedUser(domainuser.RoleGuest, "guest@test.dev")
	other := h.seedUser(domainuser.RoleGuest, "other@test.dev")
	admin := h.seedUser(domainuser.RoleAdmin, "admin@test.dev")

	hostTok := host.ID.String()
	guestTok := guest.ID.String()
	otherTok := other.ID.String()
	adminTok := admin.ID.String()

	// 1. Host creates a property.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Sea View Loft", "type": "apartment", "city": "Lisbon", "country": "PT",
		"latitude": 38.7, "longitude": -9.1, "priceCents": 12000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 3,
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	propID := h.decode(rec)["id"].(string)

	// A guest must not be able to create a listing (host-only route).
	if r := h.do(http.MethodPost, "/api/v1/properties", guestTok, map[string]any{
		"title": "x", "type": "house", "city": "c", "country": "PT", "priceCents": 100, "currency": "EUR", "maxGuests": 1,
	}); r.Code != http.StatusForbidden {
		t.Fatalf("guest create property: status = %d, want 403", r.Code)
	}

	// 2. Host uploads a photo (multipart) so the listing can be published.
	uploadPhoto(t, h, hostTok, propID)

	// 3. Host publishes.
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/publish", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "publish")
	if h.decode(rec)["status"] != "published" {
		t.Fatal("expected status published")
	}

	// 4. Guest sees it in search.
	rec = h.do(http.MethodGet, "/api/v1/properties?city=Lisbon", "", nil)
	mustStatus(t, rec, http.StatusOK, "search")
	if total := h.decode(rec)["total"].(float64); total < 1 {
		t.Fatalf("search total = %v, want >= 1", total)
	}

	// 5. Availability empty before booking.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/availability", "", nil)
	mustStatus(t, rec, http.StatusOK, "availability before")
	if booked := h.decode(rec)["booked"].([]any); len(booked) != 0 {
		t.Fatalf("expected no booked ranges, got %d", len(booked))
	}

	// 6. Guest books.
	in := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	out := time.Now().UTC().AddDate(0, 0, 8).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/bookings", guestTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 2,
	})
	mustStatus(t, rec, http.StatusCreated, "create booking")
	booking := h.decode(rec)
	bookingID := booking["id"].(string)
	// 3 nights * 120.00 = 360.00 subtotal + 30.00 cleaning = 390.00; 10% service
	// fee = 39.00; total = 429.00.
	if got := booking["subtotal"].(map[string]any)["amountCents"].(float64); got != 36000 {
		t.Fatalf("subtotal = %v, want 36000", got)
	}
	if got := booking["totalPrice"].(map[string]any)["amountCents"].(float64); got != 42900 {
		t.Fatalf("total price = %v, want 42900", got)
	}

	// 7. Availability now shows one booked range.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/availability", "", nil)
	if booked := h.decode(rec)["booked"].([]any); len(booked) != 1 {
		t.Fatalf("expected 1 booked range, got %d", len(booked))
	}

	// 8. Overlapping booking is rejected.
	if r := h.do(http.MethodPost, "/api/v1/bookings", otherTok, map[string]any{
		"propertyId": propID, "checkIn": in, "checkOut": out, "guests": 1,
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("overlap booking: status = %d, want 422 (body %s)", r.Code, r.Body.String())
	}

	// 9. Host lists the property's bookings and confirms.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/bookings", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host bookings")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("host bookings total = %v, want 1", total)
	}
	rec = h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/confirm", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "confirm")
	if h.decode(rec)["status"] != "confirmed" {
		t.Fatal("expected confirmed")
	}

	// 10. Guest cannot confirm (host-only).
	if r := h.do(http.MethodPost, "/api/v1/bookings/"+bookingID+"/confirm", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest confirm: status = %d, want 403", r.Code)
	}

	// 11. Messaging: guest starts a conversation and sends a message.
	rec = h.do(http.MethodPost, "/api/v1/conversations", guestTok, map[string]any{"propertyId": propID})
	mustStatus(t, rec, http.StatusCreated, "start conversation")
	convID := h.decode(rec)["id"].(string)

	rec = h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", guestTok, map[string]any{"body": "Is early check-in possible?"})
	mustStatus(t, rec, http.StatusCreated, "send message")

	// 12. Host sees the conversation and the message.
	rec = h.do(http.MethodGet, "/api/v1/conversations", hostTok, nil)
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("host conversations total = %v, want 1", total)
	}
	rec = h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", hostTok, nil)
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("messages total = %v, want 1", total)
	}

	// 13. A non-participant cannot read the thread.
	if r := h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", otherTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("non-participant read: status = %d, want 403", r.Code)
	}

	// 14. Date-aware search: the booked window hides the listing, a free window
	// shows it again.
	rec = h.do(http.MethodGet, "/api/v1/properties?checkIn="+in+"&checkOut="+out, "", nil)
	mustStatus(t, rec, http.StatusOK, "search booked window")
	if total := h.decode(rec)["total"].(float64); total != 0 {
		t.Fatalf("search over booked window total = %v, want 0", total)
	}
	freeIn := time.Now().UTC().AddDate(0, 0, 40).Format("2006-01-02")
	freeOut := time.Now().UTC().AddDate(0, 0, 43).Format("2006-01-02")
	rec = h.do(http.MethodGet, "/api/v1/properties?checkIn="+freeIn+"&checkOut="+freeOut, "", nil)
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("search over free window total = %v, want 1", total)
	}

	// 15. Favorites: save, list, unsave.
	rec = h.do(http.MethodPost, "/api/v1/favorites", guestTok, map[string]any{"propertyId": propID})
	mustStatus(t, rec, http.StatusCreated, "add favorite")
	rec = h.do(http.MethodGet, "/api/v1/favorites", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "list favorites")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("favorites total = %v, want 1", total)
	}
	if r := h.do(http.MethodDelete, "/api/v1/favorites/"+propID, guestTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("remove favorite: status = %d, want 204", r.Code)
	}
	rec = h.do(http.MethodGet, "/api/v1/favorites", guestTok, nil)
	if total := h.decode(rec)["total"].(float64); total != 0 {
		t.Fatalf("favorites after remove = %v, want 0", total)
	}

	// 16. Admin moderation: a non-admin is forbidden; an admin can suspend the
	// listing (removing it from search) and then reinstate it.
	if r := h.do(http.MethodPost, "/api/v1/admin/properties/"+propID+"/suspend", hostTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("non-admin suspend: status = %d, want 403", r.Code)
	}
	rec = h.do(http.MethodPost, "/api/v1/admin/properties/"+propID+"/suspend", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin suspend")
	if h.decode(rec)["status"] != "suspended" {
		t.Fatal("expected status suspended")
	}
	rec = h.do(http.MethodGet, "/api/v1/properties?city=Lisbon", "", nil)
	if total := h.decode(rec)["total"].(float64); total != 0 {
		t.Fatalf("search after suspend total = %v, want 0", total)
	}
	rec = h.do(http.MethodPost, "/api/v1/admin/properties/"+propID+"/unsuspend", adminTok, nil)
	mustStatus(t, rec, http.StatusOK, "admin unsuspend")
	if h.decode(rec)["status"] != "published" {
		t.Fatal("expected status published after unsuspend")
	}
}

func uploadPhoto(t *testing.T, h *harness, token, propID string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("photo", "cover.jpg")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	fmt.Fprint(fw, "fake-image-bytes")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/properties/"+propID+"/photos", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	mustStatus(t, rec, http.StatusOK, "upload photo")
}
