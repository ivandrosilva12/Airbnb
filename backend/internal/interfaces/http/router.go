package http

import (
	"log/slog"

	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/openapi"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handlers bundles the HTTP handlers wired by the composition root.
type Handlers struct {
	Health         *handler.HealthHandler
	User           *handler.UserHandler
	Property       *handler.PropertyHandler
	Booking        *handler.BookingHandler
	Review         *handler.ReviewHandler
	Message        *handler.MessageHandler
	Favorite       *handler.FavoriteHandler
	Notification   *handler.NotificationHandler
	Payment        *handler.PaymentHandler
	Analytics      *handler.AnalyticsHandler
	Block          *handler.BlockHandler
	Payout         *handler.PayoutHandler
	Realtime       *handler.RealtimeHandler
	Identity       *handler.IdentityHandler
	Report         *handler.ReportHandler
	PaymentWebhook *handler.PaymentWebhookHandler
	ConnectWebhook *handler.ConnectWebhookHandler
	Alert          *handler.AlertHandler
	Privacy        *handler.PrivacyHandler
	Coupon         *handler.CouponHandler
	UserBlock      *handler.UserBlockHandler
	Offer          *handler.OfferHandler
	SavedSearch    *handler.SavedSearchHandler
	PriceRule      *handler.PriceRuleHandler
	PushToken      *handler.PushTokenHandler
	Audit          *handler.AuditHandler
	Dispute        *handler.DisputeHandler
	Cohost          *handler.CohostHandler
	SplitPayment    *handler.SplitPaymentHandler
	MessageTemplate *handler.MessageTemplateHandler
	HouseRules      *handler.HouseRulesHandler
	Tax             *handler.TaxHandler
	TaxRemittance   *handler.TaxRemittanceHandler
	Fraud           *handler.FraudHandler
	Experience      *handler.ExperienceHandler
}

// Deps are the dependencies required to build the router.
type Deps struct {
	Config   *config.Config
	Metrics  *observability.Metrics
	Registry *prometheus.Registry
	Auth     gin.HandlerFunc
	Handlers Handlers
}

// NewRouter assembles the Gin engine, middleware stack and route table.
func NewRouter(d Deps) *gin.Engine {
	if d.Config.App.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// Trust only the configured proxies for X-Forwarded-For (empty = trust none),
	// so a client cannot spoof its source IP to evade the per-IP rate limiter.
	_ = r.SetTrustedProxies(d.Config.HTTP.TrustedProxies)
	r.Use(gin.Recovery())
	// RequestID runs early so every downstream middleware and handler sees
	// the same per-request UUID — both on the gin context (RequestIDGin) and
	// on the stdlib context (RequestIDFrom / LoggerFrom). The response also
	// carries X-Request-ID so a client or upstream proxy can correlate.
	r.Use(middleware.RequestID())
	// Tracing must run AFTER RequestID (so the request-id logger exists when
	// TracingLoggerEnrich enriches it with trace/span IDs) and BEFORE the
	// business middleware so every handler executes inside the root span.
	// When TRACING_EXPORTER=noop (the default), otelgin still creates a
	// span on a noop tracer — cheap and keeps the propagation path warm
	// for downstream services that DO export.
	r.Use(middleware.Tracing(d.Config.App.Name))
	r.Use(middleware.TracingLoggerEnrich())
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.Metrics(d.Metrics))
	r.Use(middleware.MaxBody(d.Config.Security.MaxRequestBodyBytes))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     d.Config.HTTP.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	h := d.Handlers

	// Operational endpoints.
	r.GET("/healthz", h.Health.Live)
	r.GET("/readyz", h.Health.Ready)
	r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(d.Registry, promhttp.HandlerOpts{})))

	// OpenAPI 3.0 spec generated from the live route table (S46). The
	// two endpoints below are registered now (so they show up in the
	// generated spec) and their payloads are filled by Refresh at the
	// bottom of NewRouter — by that point every other route exists.
	specHandler := openapi.NewHandler()
	r.GET("/openapi.yaml", specHandler.ServeYAML)
	r.GET("/openapi.json", specHandler.ServeJSON)

	api := r.Group("/api/v1")

	// Coarse per-IP rate limit over the whole API surface (blunts floods/brute
	// force). Disabled when APIRateRPS <= 0 (the default in tests).
	if d.Config.Security.APIRateRPS > 0 {
		api.Use(middleware.RateLimit(
			d.Config.Security.APIRateRPS, d.Config.Security.APIRateBurst,
			func() { d.Metrics.RateLimitedTotal.WithLabelValues("api").Inc() },
		))
	}

	// writeRateLimit builds a per-IP token-bucket limiter for an individual
	// write-endpoint family (S30). The global limit above keeps the API surface
	// safe in aggregate; these stricter, family-scoped limits stop a single
	// caller from spamming cohort invites / dispute evidence / share authorize
	// inside that global quota. The defaults (3 rps, burst 10) are generous
	// for any human flow but cap scripted abuse before it can dominate the
	// global allowance. Disabled in tests via APIRateRPS<=0 so the e2e suite
	// can fire its bursts unimpeded.
	writeRateLimit := func(route string) gin.HandlerFunc {
		if d.Config.Security.APIRateRPS <= 0 {
			return func(c *gin.Context) { c.Next() }
		}
		return middleware.RateLimit(3, 10, func() {
			d.Metrics.RateLimitedTotal.WithLabelValues(route).Inc()
		})
	}

	// Public listing & review reads.
	api.GET("/amenities", h.Property.Amenities)
	api.GET("/properties", h.Property.Search)
	api.GET("/properties/:id", h.Property.Get)
	api.GET("/properties/:id/availability", h.Booking.Availability)
	api.GET("/properties/:id/calendar.ics", h.Booking.CalendarICS)
	api.GET("/properties/:id/reviews", h.Review.ListForProperty)
	api.GET("/properties/:id/reviews/summary", h.Review.Summary)
	// House rules (S47) — public read so a prospective guest can
	// preview the rules before logging in to book.
	api.GET("/properties/:id/house-rules", h.HouseRules.Get)
	// Tax quote (S48) — public preview of the tax breakdown for a
	// stay. Anonymous-friendly so a UI can render the line items
	// before the guest signs in.
	api.GET("/properties/:id/tax-quote", h.Tax.Quote)
	// Pricing quote (S59) — public dry-run of the full price breakdown
	// (subtotal, fees, jurisdiction tax, total) for a stay. Mirrors what
	// booking.Create would assemble without persisting anything, so the
	// booking flow can preview before the guest commits.
	api.GET("/properties/:id/pricing-quote", h.Booking.Quote)

		// Experiences catalogue (S71) — public search + detail. Filters:
		// category, city, country, language. Drafts and suspended
		// listings are hidden by the search; Get returns whatever id
		// matches (the host UI uses this for the edit pre-load).
		api.GET("/experiences", h.Experience.Search)
		api.GET("/experiences/:id", h.Experience.Get)

		// Publicly shared wishlist collection (anyone with the link).
		api.GET("/shared/collections/:token", h.Favorite.GetShared)

	// Payment gateway webhooks (authenticated by per-provider signature, not by a
	// user token, so they live outside the auth group). Rate-limited per IP to
	// blunt floods/replay storms against the unauthenticated route; rejections
	// are counted in the rate-limited metric.
	webhookLimiter := middleware.RateLimit(
		d.Config.Security.WebhookRateRPS, d.Config.Security.WebhookRateBurst,
		func() { d.Metrics.RateLimitedTotal.WithLabelValues("webhook").Inc() },
	)
	api.POST("/webhooks/payments/:provider", webhookLimiter, h.PaymentWebhook.Handle)

	// Stripe Connect account webhooks (account.updated → host payouts_enabled),
	// signature-authenticated and rate-limited like the payment webhook route.
	api.POST("/webhooks/connect/:provider", webhookLimiter, h.ConnectWebhook.Handle)

	// Alertmanager pushes firing/resolved notifications here so the internal UI
	// can reflect alert state. Authenticated by network placement (internal),
	// not a user token; rate-limited like the other webhook route.
	api.POST("/webhooks/alerts", webhookLimiter, h.Alert.IngestNotification)

	// Authenticated routes.
	auth := api.Group("")
	auth.Use(d.Auth)
	{
		// Profile.
		auth.GET("/me", h.User.Me)
		auth.PATCH("/me", h.User.UpdateMe)
		auth.PATCH("/me/preferences", h.User.UpdatePreferences)
		auth.POST("/me/become-host", h.User.BecomeHost)

		// GDPR self-service: export everything we hold, or erase the account.
		auth.GET("/me/export", h.Privacy.Export)
		auth.DELETE("/me", h.Privacy.Erase)

		// KYC identity verification (user-facing submit/status).
		auth.GET("/me/verification", h.Identity.GetMine)
		auth.POST("/me/verification", h.Identity.Submit)

		// Bookings.
		auth.POST("/bookings", h.Booking.Create)
		auth.POST("/bookings/preview-coupon", h.Booking.PreviewCoupon)
		auth.GET("/bookings/me", h.Booking.ListMine)
		auth.GET("/bookings/:id", h.Booking.Get)
		auth.POST("/bookings/:id/modify", h.Booking.Modify)
		auth.POST("/bookings/:id/cancel", h.Booking.Cancel)

		// Saved searches & new-listing alerts.
		auth.GET("/saved-searches", h.SavedSearch.List)
		auth.POST("/saved-searches", h.SavedSearch.Create)
		auth.DELETE("/saved-searches/:id", h.SavedSearch.Delete)
		auth.PATCH("/saved-searches/:id", h.SavedSearch.SetAlerts)

		// Offers addressed to the guest (pre-approvals / special offers).
		auth.GET("/offers", h.Offer.ListMine)
		auth.POST("/offers/:id/accept", h.Offer.Accept)
		auth.POST("/offers/:id/decline", h.Offer.Decline)

		// Reviews (guest -> property, and host -> guest).
		auth.POST("/reviews", h.Review.Create)
		auth.PATCH("/reviews/:id", h.Review.Edit)
		auth.DELETE("/reviews/:id", h.Review.Delete)
		auth.POST("/reviews/:id/response", h.Review.Respond)
		auth.POST("/reviews/:id/reports", h.Report.CreateForReview)
		auth.POST("/reviews/guest", h.Review.CreateGuest)
		auth.GET("/me/guest-reviews", h.Review.MyGuestReviews)
		auth.GET("/me/reviews/pending", h.Review.MyPendingReviews)

		// Messaging (host↔guest). Both roles participate, so these live under
		// the authenticated group rather than the host-only group.
		auth.GET("/conversations", h.Message.ListMine)
		// Team mailbox: threads on listings I help manage as a co-host with
		// reply_messages. Distinct from /conversations so a co-host can tell
		// "my own threads" from "threads I help handle".
		auth.GET("/me/cohost-mailbox", h.Message.ListCohostMailbox)
		auth.POST("/conversations", h.Message.Start)
		auth.GET("/conversations/unread-count", h.Message.UnreadCount)
		auth.GET("/conversations/:id/messages", h.Message.ListMessages)
		auth.POST("/conversations/:id/messages", h.Message.Send)
		auth.POST("/conversations/:id/attachments", h.Message.SendAttachment)
		auth.POST("/conversations/:id/read", h.Message.MarkRead)

		// Blocking users (gates messaging both ways).
		auth.GET("/me/blocks", h.UserBlock.ListBlocked)
		auth.POST("/users/:id/block", h.UserBlock.Block)
		auth.DELETE("/users/:id/block", h.UserBlock.Unblock)

		// Favorites (wishlist).
		auth.GET("/favorites", h.Favorite.List)
		auth.POST("/favorites", h.Favorite.Add)
		auth.PATCH("/favorites/:propertyId", h.Favorite.Move)
		auth.DELETE("/favorites/:propertyId", h.Favorite.Remove)
		// Named wishlist collections. Kept under their own path (not
		// /favorites/...) so the static segment does not collide with the
		// /favorites/:propertyId param route in Gin's radix tree.
		auth.GET("/wishlist/collections", h.Favorite.ListCollections)
		auth.POST("/wishlist/collections", h.Favorite.CreateCollection)
		auth.DELETE("/wishlist/collections/:id", h.Favorite.DeleteCollection)
		auth.POST("/wishlist/collections/:id/share", h.Favorite.Share)
		auth.DELETE("/wishlist/collections/:id/share", h.Favorite.Unshare)

		// Report a listing for moderation (any authenticated user).
		auth.POST("/properties/:id/reports", h.Report.Create)

		// Live updates (SSE) — token passed via ?access_token= for EventSource.
		auth.GET("/realtime", h.Realtime.Stream)

		// In-app notifications.
		auth.GET("/notifications", h.Notification.List)
		auth.POST("/notifications/read-all", h.Notification.MarkAllRead)
		auth.POST("/notifications/:id/read", h.Notification.MarkRead)
		auth.POST("/notifications/:id/unread", h.Notification.MarkUnread)

		// Push-token (FCM/APNs/Web Push) registration. Clients call register on
		// app launch and after the OS rotates the token; unregister on logout.
		auth.GET("/me/push-tokens", h.PushToken.List)
		auth.POST("/me/push-tokens", h.PushToken.Register)
		auth.POST("/me/push-tokens/unregister", h.PushToken.Unregister)

		// "Listings I help manage" — surfaces co-host grants regardless of
		// whether the caller has the host role (a co-host is a regular user
		// the primary host invited; they don't need to be a host themselves).
		auth.GET("/me/cohost-listings", h.Cohost.ListMine)

		// Saved-reply playbook (composer chips, host-authored). Owner-only
		// CRUD; the composer reads via List, mutations via the other three.
		// Writes are rate-limited per-IP (S30) so a script can't burst-create
		// thousands of empty templates inside the global allowance.
		auth.GET("/me/message-templates", h.MessageTemplate.List)
		auth.POST("/me/message-templates", writeRateLimit("template_create"), h.MessageTemplate.Create)
		auth.PATCH("/me/message-templates/:id", writeRateLimit("template_update"), h.MessageTemplate.Update)
		auth.DELETE("/me/message-templates/:id", writeRateLimit("template_delete"), h.MessageTemplate.Delete)

		// Split payment: per-payer authorize + organizer cancel + reads.
		// Authorize is a sensitive state mutation (drives booking confirmation)
		// — capped per-IP to keep retry-storms and idempotency probes bounded.
		auth.GET("/me/splits", h.SplitPayment.ListMine)
		auth.GET("/splits/:id", h.SplitPayment.Get)
		auth.POST("/splits/:id/shares/:shareId/authorize", writeRateLimit("split_authorize"), h.SplitPayment.AuthorizeShare)
		auth.POST("/splits/:id/cancel", writeRateLimit("split_cancel"), h.SplitPayment.Cancel)
		auth.GET("/bookings/:id/split", h.SplitPayment.GetForBooking)

		// Calendar blocks and seasonal price rules: the per-listing gate
		// (primary host OR co-host with the matching permission) lives in
		// the application service, so these sit on the authenticated group
		// rather than the host-only group. A user who's neither owner nor
		// a co-host on the listing is rejected at the application layer
		// with 403.
		auth.GET("/properties/:id/blocks", h.Block.ListForProperty)
		auth.POST("/properties/:id/blocks", h.Block.Create)
		auth.POST("/properties/:id/calendar/import", h.Block.ImportCalendar)
		auth.DELETE("/blocks/:id", h.Block.Delete)
		auth.GET("/properties/:id/price-rules", h.PriceRule.ListForProperty)
		auth.POST("/properties/:id/price-rules", h.PriceRule.Create)
		auth.DELETE("/properties/:id/price-rules/:ruleId", h.PriceRule.Delete)

		// Resolution Center — guests/hosts open and participate in post-stay
		// cases; admins decide them (separate route group below). All four
		// write paths are per-IP rate-limited (S30) to stop a malicious
		// reporter from spamming evidence or host-response within the global
		// allowance — an attack that would otherwise let one IP dominate a
		// case thread.
		auth.POST("/bookings/:id/disputes", writeRateLimit("dispute_open"), h.Dispute.Open)
		auth.GET("/me/disputes", h.Dispute.ListMine)
		auth.GET("/disputes/:id", h.Dispute.Get)
		auth.POST("/disputes/:id/evidence", writeRateLimit("dispute_evidence"), h.Dispute.AddEvidence)
		auth.POST("/disputes/:id/host-response", writeRateLimit("dispute_host_response"), h.Dispute.HostRespond)

		// House-rules acceptance proof (S47) — the per-booking row plus
		// the versioned rules text the guest acknowledged. Callable by
		// the booking's guest, the listing's host, or an admin.
		auth.GET("/bookings/:id/house-rules-acceptance", h.HouseRules.GetAcceptance)

		// Payments (guest-facing reads).
		auth.GET("/payments/me", h.Payment.ListMine)
		auth.GET("/bookings/:id/payment", h.Payment.GetForBooking)
		auth.GET("/bookings/:id/deposit", h.Payment.GetDeposit)
		auth.GET("/bookings/:id/arrival", h.Booking.Arrival)
		auth.GET("/bookings/:id/receipt", h.Payment.Receipt)

		// Host-only listing management.
		host := auth.Group("")
		host.Use(middleware.RequireHost())
		{
			host.GET("/host/properties", h.Property.ListMine)
			host.GET("/host/properties/:id", h.Property.GetForHost)
			host.GET("/host/metrics", h.Analytics.HostMetrics)
			host.GET("/host/earnings", h.Payout.Summary)
			host.GET("/host/earnings/entries", h.Payout.ListEntries)
			host.GET("/host/earnings/export.csv", h.Payout.ExportCSV)
			host.GET("/host/payouts/available", h.Payout.Available)
			host.GET("/host/payouts", h.Payout.ListDisbursements)
			host.POST("/host/payouts", h.Payout.RequestDisbursement)
			host.POST("/host/payouts/onboard", h.Payout.Onboard)
			host.POST("/host/payouts/account/refresh", h.Payout.RefreshAccount)
			host.POST("/properties", h.Property.Create)
			host.PATCH("/properties/:id", h.Property.Update)
			host.DELETE("/properties/:id", h.Property.Delete)
			host.POST("/properties/:id/publish", h.Property.Publish)
			// Duplicate (S60) — clone a listing into a fresh draft. Ownership
			// is checked by the service; co-hosts cannot fork another host's
			// inventory.
			host.POST("/properties/:id/duplicate", h.Property.Duplicate)
			// House rules write — primary host only; the service enforces
			// HostID match before persisting so the route gate is enough
			// here (no extra permission check). Co-hosts intentionally
			// cannot edit rules.
			host.PATCH("/properties/:id/house-rules", h.HouseRules.Set)
			host.POST("/properties/:id/photos", h.Property.UploadPhoto)
			host.PATCH("/properties/:id/photos/order", h.Property.ReorderPhotos)
			host.DELETE("/properties/:id/photos/:photoId", h.Property.DeletePhoto)
			host.GET("/properties/:id/bookings", h.Booking.ListForProperty)
			host.POST("/bookings/:id/confirm", h.Booking.Confirm)
			host.POST("/bookings/:id/complete", h.Booking.Complete)

			// Experiences host CRUD (S71). Ownership is checked inside
			// the service so the route gate is enough here. Public Get
			// + Search are registered above on the public group.
			host.GET("/host/experiences", h.Experience.HostList)
			host.POST("/experiences", h.Experience.Create)
			host.PATCH("/experiences/:id", h.Experience.Update)
			host.DELETE("/experiences/:id", h.Experience.Delete)
			host.POST("/experiences/:id/publish", h.Experience.Publish)
			host.POST("/experiences/:id/suspend", h.Experience.Suspend)
			host.POST("/experiences/:id/photos", h.Experience.AddPhoto)

			// Host offers (pre-approval / special offer) to guests.
			host.POST("/offers", h.Offer.Create)
			host.GET("/offers/sent", h.Offer.ListSent)
			host.POST("/offers/:id/withdraw", h.Offer.Withdraw)

			// Calendar blocks and seasonal pricing rules used to live here, but
			// were moved to the authenticated group above so co-hosts (who
			// may not have the global host role) can act on the listings
			// they're granted on — the per-listing permission check sits in
			// the application service.

			// Co-host grants on a listing (primary host only — the handler
			// enforces ownership; the route group sits on the host gate to
			// keep accidental access out).
			host.GET("/host/properties/:id/cohosts", h.Cohost.List)
			host.POST("/host/properties/:id/cohosts", writeRateLimit("cohost_invite"), h.Cohost.Invite)
			host.PATCH("/host/properties/:id/cohosts/:cohostId", writeRateLimit("cohost_update"), h.Cohost.UpdatePermissions)
			host.DELETE("/host/properties/:id/cohosts/:cohostId", writeRateLimit("cohost_revoke"), h.Cohost.Revoke)
		}

		// Admin-only moderation.
		admin := auth.Group("/admin")
		admin.Use(middleware.RequireAdmin())
		{
			admin.POST("/properties/:id/suspend", h.Property.AdminSuspend)
			admin.POST("/properties/:id/unsuspend", h.Property.AdminUnsuspend)
			// User moderation (S61/S65). List powers the admin
			// user-management table (S65); suspend/unsuspend locks a
			// user out of the platform (auth middleware refuses inactive
			// bearers) and records an audit row on success.
			admin.GET("/users", h.User.AdminList)
			admin.POST("/users/:id/suspend", h.User.AdminSuspend)
			admin.POST("/users/:id/unsuspend", h.User.AdminUnsuspend)

			// KYC review queue.
			admin.GET("/verifications", h.Identity.ListPending)
			admin.POST("/verifications/:id/approve", h.Identity.Approve)
			admin.POST("/verifications/:id/reject", h.Identity.Reject)

			// Listing-report moderation queue.
			admin.GET("/reports", h.Report.ListOpen)
			admin.POST("/reports/:id/resolve", h.Report.Resolve)
			admin.POST("/reports/:id/dismiss", h.Report.Dismiss)

			// Resolution Center moderation queue.
			admin.GET("/disputes", h.Dispute.AdminListOpen)
			admin.POST("/disputes/:id/resolve", h.Dispute.AdminResolve)
			admin.POST("/disputes/:id/reject", h.Dispute.AdminReject)

			// Promo codes
			admin.GET("/coupons", h.Coupon.List)
			admin.POST("/coupons", h.Coupon.Create)
			admin.POST("/coupons/:id/deactivate", h.Coupon.Deactivate)

			// Webhook dedupe-table retention/cleanup.
			admin.POST("/webhooks/events/cleanup", h.PaymentWebhook.Cleanup)

			// Live alert states (firing + recently resolved) for the console.
			admin.GET("/alerts", h.Alert.ListAlerts)

			// Alertmanager silences (maintenance windows that mute alerts).
			admin.GET("/alerts/silences", h.Alert.ListSilences)
			admin.POST("/alerts/silences", h.Alert.CreateSilence)
			admin.DELETE("/alerts/silences/:id", h.Alert.DeleteSilence)

			// Audit trail (S45): read-only feed of admin-initiated state
			// changes. Writes happen implicitly inside other admin handlers
			// on success (property suspend/unsuspend, KYC approve/reject,
			// dispute resolve/reject).
			admin.GET("/audit", h.Audit.List)

			// Tax rules CRUD (S48). Stub admin surface — no audit
			// hookup yet (the audit BC's Action enum is closed and
			// adding tax.* would warrant its own slice).
			admin.GET("/tax-rules", h.Tax.AdminList)
			admin.POST("/tax-rules", h.Tax.AdminCreate)
			admin.DELETE("/tax-rules/:id", h.Tax.AdminDelete)

			// Tax remittance (S62) — per-jurisdiction monthly report of
			// tax captured on confirmed/completed bookings. Read-only,
			// no state machine; the operator hands the output to the
			// tax authority. ?year=YYYY&month=1..12.
			admin.GET("/tax/remittance", h.TaxRemittance.AdminPeriod)

			// Fraud detection (S68) — read-only review queue over the
			// assessments emitted as a side-effect of booking creation.
			// ?level=low|medium|high filters; default returns all.
			admin.GET("/fraud/assessments", h.Fraud.AdminList)
			admin.GET("/fraud/assessments/by-booking/:bookingId", h.Fraud.AdminGetByBooking)
		}
	}

	// Fill the OpenAPI cache now that every route is registered. A
	// generator error here is non-fatal — we log and continue so a bad
	// spec doesn't bring down the entire API surface; clients can still
	// hit every endpoint, they just won't get a 200 from /openapi.*.
	if err := specHandler.Refresh(routesToOpenAPI(r.Routes()), openapi.Info{
		Title:       "AirHost API",
		Version:     d.Config.App.Environment,
		Description: "AirHost public + authenticated API surface. Generated from the live router — see /openapi.yaml.",
	}); err != nil {
		slog.Default().Error("openapi spec refresh failed", "err", err)
	}

	return r
}

// routesToOpenAPI converts gin's RouteInfo slice into the openapi
// package's slim Route type. Kept here (rather than imported into the
// openapi package) so openapi/ stays free of gin and can be tested in
// isolation.
func routesToOpenAPI(in gin.RoutesInfo) []openapi.Route {
	out := make([]openapi.Route, 0, len(in))
	for _, r := range in {
		out = append(out, openapi.Route{
			Method:  r.Method,
			Path:    r.Path,
			Handler: r.Handler,
		})
	}
	return out
}
