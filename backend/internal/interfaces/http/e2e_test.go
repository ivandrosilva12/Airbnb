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

	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	blockapp "github.com/airhost/backend/internal/application/block"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	emailapp "github.com/airhost/backend/internal/application/email"
	"github.com/airhost/backend/internal/application/event"
	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	identityapp "github.com/airhost/backend/internal/application/identity"
	messageapp "github.com/airhost/backend/internal/application/message"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	propertyapp "github.com/airhost/backend/internal/application/property"
	realtimeapp "github.com/airhost/backend/internal/application/realtime"
	reportapp "github.com/airhost/backend/internal/application/report"
	reviewapp "github.com/airhost/backend/internal/application/review"
	searchapp "github.com/airhost/backend/internal/application/search"
	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/config"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/email"
	"github.com/airhost/backend/internal/infrastructure/observability"
	paymentgw "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/airhost/backend/internal/infrastructure/realtime"
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
	t           *testing.T
	router      *gin.Engine
	userRepo    *memory.UserRepository
	paymentRepo *memory.PaymentRepository
	mailer      *email.RecordingMailer
}

// webhookSecret is the GPay Angola webhook secret the harness registers so e2e
// tests can sign payloads.
const webhookSecret = "test-webhook-secret"

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := memory.NewUserRepository()
	propertyRepo := memory.NewPropertyRepository()
	bookingRepo := memory.NewBookingRepository()
	reviewRepo := memory.NewReviewRepository()
	messageRepo := memory.NewMessageRepository()
	favoriteRepo := memory.NewFavoriteRepository()
	notificationRepo := memory.NewNotificationRepository()
	paymentRepo := memory.NewPaymentRepository()
	payoutRepo := memory.NewPayoutRepository()
	blockRepo := memory.NewBlockRepository()
	identityRepo := memory.NewIdentityRepository()
	reportRepo := memory.NewReportRepository()

	dispatcher := event.NewDispatcher()

	userSvc := userapp.NewService(userRepo)
	propertySvc := propertyapp.NewService(propertyRepo, fakeStorage{})
	bookingSvc := bookingapp.NewService(bookingRepo, propertyRepo, blockRepo, 0.10, dispatcher) // 10% service fee
	reviewSvc := reviewapp.NewService(reviewRepo, bookingRepo, propertyRepo)
	messageSvc := messageapp.NewService(messageRepo, propertyRepo, dispatcher)
	searchSvc := searchapp.NewService(propertyRepo, bookingRepo, blockRepo)
	favoriteSvc := favoriteapp.NewService(favoriteRepo, propertyRepo)
	notificationSvc := notificationapp.NewService(notificationRepo)
	paymentSvc := paymentapp.NewService(paymentRepo, paymentgw.NewFakeGateway(), bookingRepo, propertyRepo)
	analyticsSvc := analyticsapp.NewService(propertyRepo, bookingRepo, paymentRepo)
	blockSvc := blockapp.NewService(blockRepo, propertyRepo)
	mailer := email.NewRecordingMailer()
	emailSvc := emailapp.NewService(userRepo, mailer)
	payoutSvc := payoutapp.NewService(payoutRepo, bookingRepo, propertyRepo)
	identitySvc := identityapp.NewService(identityRepo, dispatcher)
	reportSvc := reportapp.NewService(reportRepo, propertyRepo)
	realtimeHub := realtime.NewHub()
	realtimeSvc := realtimeapp.NewService(realtimeHub)
	dispatcher.Subscribe(notificationSvc.EventHandler())
	dispatcher.Subscribe(paymentSvc.EventHandler())
	dispatcher.Subscribe(emailSvc.EventHandler())
	dispatcher.Subscribe(payoutSvc.EventHandler())
	dispatcher.Subscribe(realtimeSvc.EventHandler())

	registry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(registry)

	authMW := func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" {
			raw = c.Query("access_token") // EventSource can't set headers
		}
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
		Config: &config.Config{
			App:      config.AppConfig{Environment: "test"},
			HTTP:     config.HTTPConfig{AllowedOrigins: []string{"*"}},
			Security: config.SecurityConfig{WebhookRateRPS: 1000, WebhookRateBurst: 1000},
		},
		Metrics:  metrics,
		Registry: registry,
		Auth:     authMW,
		Handlers: apphttp.Handlers{
			Health:         handler.NewHealthHandler(nil),
			User:           handler.NewUserHandler(userSvc),
			Property:       handler.NewPropertyHandler(propertySvc, searchSvc, metrics),
			Booking:        handler.NewBookingHandler(bookingSvc, metrics),
			Review:         handler.NewReviewHandler(reviewSvc),
			Message:        handler.NewMessageHandler(messageSvc),
			Favorite:       handler.NewFavoriteHandler(favoriteSvc),
			Notification:   handler.NewNotificationHandler(notificationSvc),
			Payment:        handler.NewPaymentHandler(paymentSvc),
			Analytics:      handler.NewAnalyticsHandler(analyticsSvc),
			Block:          handler.NewBlockHandler(blockSvc),
			Payout:         handler.NewPayoutHandler(payoutSvc),
			Realtime:       handler.NewRealtimeHandler(realtimeHub),
			Identity:       handler.NewIdentityHandler(identitySvc),
			Report:         handler.NewReportHandler(reportSvc),
			PaymentWebhook: handler.NewPaymentWebhookHandler(paymentSvc, paymentgw.NewWebhookVerifiers(config.PaymentConfig{GPayAngola: config.GPayAngolaConfig{WebhookSecret: webhookSecret}}), memory.NewWebhookEventRepository(), metrics),
		},
	})

	return &harness{t: t, router: router, userRepo: userRepo, paymentRepo: paymentRepo, mailer: mailer}
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

// doRaw issues a request with a raw body and explicit headers (e.g. a signed
// webhook payload), bypassing JSON marshalling and bearer auth.
func (h *harness) doRaw(method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
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

	// 1. Host creates a property. Amenities are normalised to canonical codes.
	rec := h.do(http.MethodPost, "/api/v1/properties", hostTok, map[string]any{
		"title": "Sea View Loft", "type": "apartment", "city": "Lisbon", "country": "PT",
		"latitude": 38.7, "longitude": -9.1, "priceCents": 12000, "cleaningFeeCents": 3000,
		"currency": "EUR", "maxGuests": 3,
		"amenities": []string{"WiFi", "kitchen", "Air Conditioning", "bogus"},
	})
	mustStatus(t, rec, http.StatusCreated, "create property")
	created := h.decode(rec)
	propID := created["id"].(string)
	amenities := created["amenities"].([]any)
	if len(amenities) != 3 || amenities[0] != "wifi" || amenities[2] != "air-conditioning" {
		t.Fatalf("amenities = %v, want [wifi kitchen air-conditioning]", amenities)
	}

	// The canonical amenity list is exposed publicly.
	rec = h.do(http.MethodGet, "/api/v1/amenities", "", nil)
	mustStatus(t, rec, http.StatusOK, "amenities list")
	if list := h.decode(rec)["amenities"].([]any); len(list) < 8 {
		t.Fatalf("canonical amenities = %d, want several", len(list))
	}

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

	// 4a. Free-text search matches the listing title; an unrelated term does not.
	rec = h.do(http.MethodGet, "/api/v1/properties?q=sea+view", "", nil)
	mustStatus(t, rec, http.StatusOK, "keyword search")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("keyword search total = %v, want 1", total)
	}
	rec = h.do(http.MethodGet, "/api/v1/properties?q=treehouse", "", nil)
	if total := h.decode(rec)["total"].(float64); total != 0 {
		t.Fatalf("keyword search (no match) total = %v, want 0", total)
	}

	// 4b. Geo radius search: near Lisbon finds it; a tight radius around Porto
	// (~270 km away) does not.
	rec = h.do(http.MethodGet, "/api/v1/properties?lat=38.72&lng=-9.14&radiusKm=50", "", nil)
	mustStatus(t, rec, http.StatusOK, "geo search near")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("geo search near Lisbon total = %v, want 1", total)
	}
	rec = h.do(http.MethodGet, "/api/v1/properties?lat=41.15&lng=-8.61&radiusKm=50", "", nil)
	if total := h.decode(rec)["total"].(float64); total != 0 {
		t.Fatalf("geo search near Porto total = %v, want 0", total)
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

	// 7. The payment was authorized for the full total when the booking was made.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "get payment")
	payment := h.decode(rec)
	if payment["status"] != "authorized" {
		t.Fatalf("payment status = %v, want authorized", payment["status"])
	}
	if got := payment["amount"].(map[string]any)["amountCents"].(float64); got != 42900 {
		t.Fatalf("payment amount = %v, want 42900", got)
	}
	// A different guest cannot view this payment.
	if r := h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", otherTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("cross-user payment read: status = %d, want 403", r.Code)
	}

	// 8. Availability now shows one booked range.
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/availability", "", nil)
	if booked := h.decode(rec)["booked"].([]any); len(booked) != 1 {
		t.Fatalf("expected 1 booked range, got %d", len(booked))
	}

	// 8a. The host blocks a future range; a guest booking over it is rejected and
	// it shows up in availability.
	blockIn := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")
	blockOut := time.Now().UTC().AddDate(0, 0, 23).Format("2006-01-02")
	rec = h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", hostTok, map[string]any{
		"from": blockIn, "to": blockOut, "reason": "Maintenance",
	})
	mustStatus(t, rec, http.StatusCreated, "create block")
	blockID := h.decode(rec)["id"].(string)

	if r := h.do(http.MethodPost, "/api/v1/bookings", otherTok, map[string]any{
		"propertyId": propID, "checkIn": blockIn, "checkOut": blockOut, "guests": 1,
	}); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("booking over a block: status = %d, want 422 (body %s)", r.Code, r.Body.String())
	}
	rec = h.do(http.MethodGet, "/api/v1/properties/"+propID+"/availability?from="+blockIn+"&to="+blockOut, "", nil)
	if booked := h.decode(rec)["booked"].([]any); len(booked) != 1 || booked[0].(map[string]any)["status"] != "blocked" {
		t.Fatalf("expected one blocked range, got %v", h.decode(rec)["booked"])
	}
	// A non-host cannot create a block.
	if r := h.do(http.MethodPost, "/api/v1/properties/"+propID+"/blocks", guestTok, map[string]any{"from": blockIn, "to": blockOut}); r.Code != http.StatusForbidden {
		t.Fatalf("guest create block: status = %d, want 403", r.Code)
	}
	// The host can unblock; bookings are then allowed again on those dates.
	if r := h.do(http.MethodDelete, "/api/v1/blocks/"+blockID, hostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete block: status = %d, want 204", r.Code)
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

	// Confirming the booking captured the payment.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/payment", guestTok, nil)
	if h.decode(rec)["status"] != "captured" {
		t.Fatalf("payment status after confirm = %v, want captured", h.decode(rec)["status"])
	}
	// The guest sees the payment in their list.
	rec = h.do(http.MethodGet, "/api/v1/payments/me", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "payments me")
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("payments/me total = %v, want 1", total)
	}

	// The guest can download a PDF receipt; a non-owner cannot.
	rec = h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/receipt", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "receipt")
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("receipt content-type = %q, want application/pdf", ct)
	}
	if body := rec.Body.Bytes(); len(body) < 4 || string(body[:4]) != "%PDF" {
		t.Fatalf("receipt body is not a PDF (len %d)", len(body))
	}
	if r := h.do(http.MethodGet, "/api/v1/bookings/"+bookingID+"/receipt", otherTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("cross-user receipt: status = %d, want 403", r.Code)
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

	// 12. Host sees the conversation, with one unread message.
	rec = h.do(http.MethodGet, "/api/v1/conversations", hostTok, nil)
	hostConvs := h.decode(rec)
	if total := hostConvs["total"].(float64); total != 1 {
		t.Fatalf("host conversations total = %v, want 1", total)
	}
	if unread := hostConvs["items"].([]any)[0].(map[string]any)["unreadCount"].(float64); unread != 1 {
		t.Fatalf("conversation unreadCount = %v, want 1", unread)
	}
	rec = h.do(http.MethodGet, "/api/v1/conversations/"+convID+"/messages", hostTok, nil)
	if total := h.decode(rec)["total"].(float64); total != 1 {
		t.Fatalf("messages total = %v, want 1", total)
	}

	// 12b. Total unread for the host is 1; the sender (guest) has 0.
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	if u := h.decode(rec)["unread"].(float64); u != 1 {
		t.Fatalf("host total unread = %v, want 1", u)
	}
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", guestTok, nil)
	if u := h.decode(rec)["unread"].(float64); u != 0 {
		t.Fatalf("guest total unread = %v, want 0 (own message)", u)
	}

	// 12c. After the host opens the thread, unread drops to 0.
	if r := h.do(http.MethodPost, "/api/v1/conversations/"+convID+"/read", hostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("mark conversation read: status = %d, want 204", r.Code)
	}
	rec = h.do(http.MethodGet, "/api/v1/conversations/unread-count", hostTok, nil)
	if u := h.decode(rec)["unread"].(float64); u != 0 {
		t.Fatalf("host total unread after read = %v, want 0", u)
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

	// 17. Notifications: the host was notified of the booking request and the
	// message; the guest was notified of the confirmation.
	rec = h.do(http.MethodGet, "/api/v1/notifications", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host notifications")
	hostNotifs := h.decode(rec)
	if unread := hostNotifs["unread"].(float64); unread < 2 {
		t.Fatalf("host unread notifications = %v, want >= 2", unread)
	}
	items := hostNotifs["items"].([]any)
	var sawBookingRequested bool
	for _, it := range items {
		if it.(map[string]any)["type"] == "booking_requested" {
			sawBookingRequested = true
		}
	}
	if !sawBookingRequested {
		t.Fatal("expected a booking_requested notification for the host")
	}

	rec = h.do(http.MethodGet, "/api/v1/notifications", guestTok, nil)
	guestNotifs := h.decode(rec)
	if unread := guestNotifs["unread"].(float64); unread < 1 {
		t.Fatalf("guest unread notifications = %v, want >= 1", unread)
	}

	// Marking one read decreases the unread count.
	firstID := items[0].(map[string]any)["id"].(string)
	before := hostNotifs["unread"].(float64)
	if r := h.do(http.MethodPost, "/api/v1/notifications/"+firstID+"/read", hostTok, nil); r.Code != http.StatusNoContent {
		t.Fatalf("mark read: status = %d, want 204", r.Code)
	}
	rec = h.do(http.MethodGet, "/api/v1/notifications", hostTok, nil)
	if after := h.decode(rec)["unread"].(float64); after != before-1 {
		t.Fatalf("unread after mark-read = %v, want %v", after, before-1)
	}

	// A guest cannot mark the host's notification read (not found for them).
	if r := h.do(http.MethodPost, "/api/v1/notifications/"+firstID+"/read", guestTok, nil); r.Code != http.StatusNotFound {
		t.Fatalf("cross-user mark read: status = %d, want 404", r.Code)
	}

	// 17b. Transactional emails were delivered for the same events: the host got
	// a booking-request email, the guest a confirmation, and the host the new
	// message — each to the recipient's seeded address.
	sent := h.mailer.Sent()
	gotEmail := func(to, subject string) bool {
		for _, m := range sent {
			if m.To == to && m.Subject == subject {
				return true
			}
		}
		return false
	}
	if !gotEmail(host.Email, "New booking request") {
		t.Fatalf("expected a booking-request email to the host, got %+v", sent)
	}
	if !gotEmail(guest.Email, "Booking confirmed") {
		t.Fatalf("expected a confirmation email to the guest, got %+v", sent)
	}
	if !gotEmail(host.Email, "New message") {
		t.Fatalf("expected a new-message email to the host, got %+v", sent)
	}

	// 17c. Email preferences round-trip: opted-in by default; a PATCH can opt out
	// of one category without affecting the other.
	rec = h.do(http.MethodGet, "/api/v1/me", guestTok, nil)
	mustStatus(t, rec, http.StatusOK, "get me")
	if p := h.decode(rec)["emailPreferences"].(map[string]any); p["bookings"] != true || p["messages"] != true {
		t.Fatalf("default email prefs = %v, want both true", p)
	}
	rec = h.do(http.MethodPatch, "/api/v1/me/preferences", guestTok, map[string]any{"messages": false})
	mustStatus(t, rec, http.StatusOK, "update prefs")
	if p := h.decode(rec)["emailPreferences"].(map[string]any); p["bookings"] != true || p["messages"] != false {
		t.Fatalf("updated email prefs = %v, want bookings=true messages=false", p)
	}

	// 18. Host metrics: one published listing, one confirmed booking worth the
	// captured total, with an upcoming check-in.
	rec = h.do(http.MethodGet, "/api/v1/host/metrics", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host metrics")
	metrics := h.decode(rec)
	if metrics["listings"].(float64) != 1 || metrics["published"].(float64) != 1 {
		t.Fatalf("metrics listings/published = %v/%v, want 1/1", metrics["listings"], metrics["published"])
	}
	if metrics["confirmed"].(float64) != 1 {
		t.Fatalf("metrics confirmed = %v, want 1", metrics["confirmed"])
	}
	if metrics["upcomingCheckins"].(float64) != 1 {
		t.Fatalf("metrics upcomingCheckins = %v, want 1", metrics["upcomingCheckins"])
	}
	if cap := metrics["capturedRevenue"].(map[string]any)["amountCents"].(float64); cap != 42900 {
		t.Fatalf("metrics captured revenue = %v, want 42900", cap)
	}

	// A non-host (plain guest) cannot read host metrics.
	if r := h.do(http.MethodGet, "/api/v1/host/metrics", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest host metrics: status = %d, want 403", r.Code)
	}

	// 19. Host earnings: confirming the booking credited the host with the net
	// payout (total 429.00 minus the 39.00 service fee = 390.00), recorded as one
	// earning ledger entry.
	rec = h.do(http.MethodGet, "/api/v1/host/earnings", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host earnings")
	balances := h.decode(rec)["balances"].([]any)
	if len(balances) != 1 {
		t.Fatalf("earnings balances = %d, want 1", len(balances))
	}
	bal := balances[0].(map[string]any)
	if net := bal["net"].(map[string]any)["amountCents"].(float64); net != 39000 {
		t.Fatalf("host net earnings = %v, want 39000", net)
	}
	if cur := bal["net"].(map[string]any)["currency"].(string); cur != "EUR" {
		t.Fatalf("host earnings currency = %q, want EUR", cur)
	}

	rec = h.do(http.MethodGet, "/api/v1/host/earnings/entries", hostTok, nil)
	mustStatus(t, rec, http.StatusOK, "host earnings entries")
	entries := h.decode(rec)
	if total := entries["total"].(float64); total != 1 {
		t.Fatalf("earnings entries total = %v, want 1", total)
	}
	if kind := entries["items"].([]any)[0].(map[string]any)["kind"].(string); kind != "earning" {
		t.Fatalf("earnings entry kind = %q, want earning", kind)
	}

	// A plain guest cannot read host earnings.
	if r := h.do(http.MethodGet, "/api/v1/host/earnings", guestTok, nil); r.Code != http.StatusForbidden {
		t.Fatalf("guest host earnings: status = %d, want 403", r.Code)
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
