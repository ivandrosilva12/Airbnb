package http

import (
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
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
	r.Use(gin.Recovery())
	r.Use(middleware.Metrics(d.Metrics))
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

	api := r.Group("/api/v1")

	// Public listing & review reads.
	api.GET("/amenities", h.Property.Amenities)
	api.GET("/properties", h.Property.Search)
	api.GET("/properties/:id", h.Property.Get)
	api.GET("/properties/:id/availability", h.Booking.Availability)
	api.GET("/properties/:id/calendar.ics", h.Booking.CalendarICS)
	api.GET("/properties/:id/reviews", h.Review.ListForProperty)
	api.GET("/properties/:id/reviews/summary", h.Review.Summary)

	// Payment gateway webhooks (authenticated by per-provider signature, not by a
	// user token, so they live outside the auth group).
	api.POST("/webhooks/payments/:provider", h.PaymentWebhook.Handle)

	// Authenticated routes.
	auth := api.Group("")
	auth.Use(d.Auth)
	{
		// Profile.
		auth.GET("/me", h.User.Me)
		auth.PATCH("/me", h.User.UpdateMe)
		auth.PATCH("/me/preferences", h.User.UpdatePreferences)
		auth.POST("/me/become-host", h.User.BecomeHost)

		// KYC identity verification (user-facing submit/status).
		auth.GET("/me/verification", h.Identity.GetMine)
		auth.POST("/me/verification", h.Identity.Submit)

		// Bookings.
		auth.POST("/bookings", h.Booking.Create)
		auth.GET("/bookings/me", h.Booking.ListMine)
		auth.GET("/bookings/:id", h.Booking.Get)
		auth.POST("/bookings/:id/cancel", h.Booking.Cancel)

		// Reviews (guest -> property, and host -> guest).
		auth.POST("/reviews", h.Review.Create)
		auth.POST("/reviews/guest", h.Review.CreateGuest)
		auth.GET("/me/guest-reviews", h.Review.MyGuestReviews)

		// Messaging (host↔guest). Both roles participate, so these live under
		// the authenticated group rather than the host-only group.
		auth.GET("/conversations", h.Message.ListMine)
		auth.POST("/conversations", h.Message.Start)
		auth.GET("/conversations/unread-count", h.Message.UnreadCount)
		auth.GET("/conversations/:id/messages", h.Message.ListMessages)
		auth.POST("/conversations/:id/messages", h.Message.Send)
		auth.POST("/conversations/:id/read", h.Message.MarkRead)

		// Favorites (wishlist).
		auth.GET("/favorites", h.Favorite.List)
		auth.POST("/favorites", h.Favorite.Add)
		auth.DELETE("/favorites/:propertyId", h.Favorite.Remove)

		// Report a listing for moderation (any authenticated user).
		auth.POST("/properties/:id/reports", h.Report.Create)

		// Live updates (SSE) — token passed via ?access_token= for EventSource.
		auth.GET("/realtime", h.Realtime.Stream)

		// In-app notifications.
		auth.GET("/notifications", h.Notification.List)
		auth.POST("/notifications/read-all", h.Notification.MarkAllRead)
		auth.POST("/notifications/:id/read", h.Notification.MarkRead)

		// Payments (guest-facing reads).
		auth.GET("/payments/me", h.Payment.ListMine)
		auth.GET("/bookings/:id/payment", h.Payment.GetForBooking)
		auth.GET("/bookings/:id/receipt", h.Payment.Receipt)

		// Host-only listing management.
		host := auth.Group("")
		host.Use(middleware.RequireHost())
		{
			host.GET("/host/properties", h.Property.ListMine)
			host.GET("/host/metrics", h.Analytics.HostMetrics)
			host.GET("/host/earnings", h.Payout.Summary)
			host.GET("/host/earnings/entries", h.Payout.ListEntries)
			host.GET("/host/earnings/export.csv", h.Payout.ExportCSV)
			host.POST("/properties", h.Property.Create)
			host.PATCH("/properties/:id", h.Property.Update)
			host.DELETE("/properties/:id", h.Property.Delete)
			host.POST("/properties/:id/publish", h.Property.Publish)
			host.POST("/properties/:id/photos", h.Property.UploadPhoto)
			host.POST("/properties/:id/photos/presign", h.Property.PresignPhotoUpload)
			host.PATCH("/properties/:id/photos/order", h.Property.ReorderPhotos)
			host.DELETE("/properties/:id/photos/:photoId", h.Property.DeletePhoto)
			host.GET("/properties/:id/bookings", h.Booking.ListForProperty)
			host.POST("/bookings/:id/confirm", h.Booking.Confirm)
			host.POST("/bookings/:id/complete", h.Booking.Complete)

			// Calendar blocks.
			host.GET("/properties/:id/blocks", h.Block.ListForProperty)
			host.POST("/properties/:id/blocks", h.Block.Create)
			host.POST("/properties/:id/calendar/import", h.Block.ImportCalendar)
			host.DELETE("/blocks/:id", h.Block.Delete)
		}

		// Admin-only moderation.
		admin := auth.Group("/admin")
		admin.Use(middleware.RequireAdmin())
		{
			admin.POST("/properties/:id/suspend", h.Property.AdminSuspend)
			admin.POST("/properties/:id/unsuspend", h.Property.AdminUnsuspend)

			// KYC review queue.
			admin.GET("/verifications", h.Identity.ListPending)
			admin.POST("/verifications/:id/approve", h.Identity.Approve)
			admin.POST("/verifications/:id/reject", h.Identity.Reject)

			// Listing-report moderation queue.
			admin.GET("/reports", h.Report.ListOpen)
			admin.POST("/reports/:id/resolve", h.Report.Resolve)
			admin.POST("/reports/:id/dismiss", h.Report.Dismiss)
		}
	}

	return r
}
