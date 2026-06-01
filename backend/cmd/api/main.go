// Command api is the entry point for the AirHost accommodation management API.
//
// It is the composition root: it loads configuration, wires the infrastructure
// adapters (PostgreSQL, Keycloak, MinIO, Prometheus) into the application
// services and HTTP interface, and runs the server with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	alertingapp "github.com/airhost/backend/internal/application/alerting"
	alertstateapp "github.com/airhost/backend/internal/application/alertstate"
	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	blockapp "github.com/airhost/backend/internal/application/block"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	couponapp "github.com/airhost/backend/internal/application/coupon"
	emailapp "github.com/airhost/backend/internal/application/email"
	"github.com/airhost/backend/internal/application/event"
	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	identityapp "github.com/airhost/backend/internal/application/identity"
	messageapp "github.com/airhost/backend/internal/application/message"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	offerapp "github.com/airhost/backend/internal/application/offer"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	priceruleapp "github.com/airhost/backend/internal/application/pricerule"
	privacyapp "github.com/airhost/backend/internal/application/privacy"
	propertyapp "github.com/airhost/backend/internal/application/property"
	realtimeapp "github.com/airhost/backend/internal/application/realtime"
	reportapp "github.com/airhost/backend/internal/application/report"
	reviewapp "github.com/airhost/backend/internal/application/review"
	savedsearchapp "github.com/airhost/backend/internal/application/savedsearch"
	searchapp "github.com/airhost/backend/internal/application/search"
	userapp "github.com/airhost/backend/internal/application/user"
	userblockapp "github.com/airhost/backend/internal/application/userblock"
	"github.com/airhost/backend/internal/config"
	domainuser "github.com/airhost/backend/internal/domain/user"
	infraalerting "github.com/airhost/backend/internal/infrastructure/alerting"
	"github.com/airhost/backend/internal/infrastructure/auth"
	"github.com/airhost/backend/internal/infrastructure/email"
	"github.com/airhost/backend/internal/infrastructure/observability"
	paymentgw "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/airhost/backend/internal/infrastructure/realtime"
	"github.com/airhost/backend/internal/infrastructure/scheduler"
	"github.com/airhost/backend/internal/infrastructure/storage"
	apphttp "github.com/airhost/backend/internal/interfaces/http"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Infrastructure adapters -------------------------------------------
	pool, err := postgres.NewPool(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("connected to postgres", "host", cfg.Database.Host)

	if err := postgres.Migrate(ctx, pool, cfg.Database.MigrationsPath); err != nil {
		return err
	}
	slog.Info("database migrations applied")

	objectStore, err := storage.NewMinioStorage(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	slog.Info("connected to object storage", "endpoint", cfg.Storage.Endpoint, "bucket", cfg.Storage.Bucket)

	verifier, err := auth.NewVerifier(ctx, cfg.Keycloak)
	if err != nil {
		return err
	}
	slog.Info("oidc provider discovered", "issuer", cfg.Keycloak.Issuer)

	// --- Repositories ------------------------------------------------------
	userRepo := postgres.NewUserRepository(pool)
	propertyRepo := postgres.NewPropertyRepository(pool)
	bookingRepo := postgres.NewBookingRepository(pool)
	reviewRepo := postgres.NewReviewRepository(pool)
	messageRepo := postgres.NewMessageRepository(pool)
	userBlockRepo := postgres.NewUserBlockRepository(pool)
	offerRepo := postgres.NewOfferRepository(pool)
	savedSearchRepo := postgres.NewSavedSearchRepository(pool)
	favoriteRepo := postgres.NewFavoriteRepository(pool)
	notificationRepo := postgres.NewNotificationRepository(pool)
	paymentRepo := postgres.NewPaymentRepository(pool)
	payoutRepo := postgres.NewPayoutRepository(pool)
	blockRepo := postgres.NewBlockRepository(pool)
	priceRuleRepo := postgres.NewPriceRuleRepository(pool)
	identityRepo := postgres.NewIdentityRepository(pool)
	reportRepo := postgres.NewReportRepository(pool)
	couponRepo := postgres.NewCouponRepository(pool)
	webhookEventRepo := postgres.NewWebhookEventRepository(pool)

	// --- Domain events ----------------------------------------------------
	// A synchronous in-process dispatcher fans domain events out to subscribers,
	// wrapped in a durable publisher that records each event in the outbox table
	// (so an event in flight survives a crash and is re-delivered on recovery).
	dispatcher := event.NewDispatcher()
	outboxRepo := postgres.NewOutboxRepository(pool)
	eventPublisher := event.NewDurablePublisher(outboxRepo, dispatcher)
	// The unit of work commits a domain write and its events in one transaction,
	// then drains the relay so subscribers run once the write has committed.
	uow := postgres.NewUnitOfWork(pool, eventPublisher)

	// --- Application services ---------------------------------------------
	userSvc := userapp.NewService(userRepo)
	// Identity (KYC) is wired first so the booking and property services can gate
	// on it when the REQUIRE_KYC_* policy flags are enabled.
	identitySvc := identityapp.NewService(identityRepo, uow)
	propertySvc := propertyapp.NewService(propertyRepo, objectStore, identitySvc, cfg.Identity.RequireKYCToHost)
	bookingSvc := bookingapp.NewService(bookingRepo, propertyRepo, blockRepo, couponRepo, priceRuleRepo, cfg.Pricing.ServiceFeeRate, identitySvc, cfg.Identity.RequireKYCToBook, uow)
	reviewSvc := reviewapp.NewService(reviewRepo, bookingRepo, propertyRepo)
	messageSvc := messageapp.NewService(messageRepo, propertyRepo, userBlockRepo, objectStore, uow)
	searchSvc := searchapp.NewService(propertyRepo, bookingRepo, blockRepo)
	favoriteSvc := favoriteapp.NewService(favoriteRepo, propertyRepo)
	notificationSvc := notificationapp.NewService(notificationRepo)
	paymentSvc := paymentapp.NewService(paymentRepo, paymentgw.NewGateway(cfg.Payment), bookingRepo, propertyRepo)
	analyticsSvc := analyticsapp.NewService(propertyRepo, bookingRepo, paymentRepo)
	blockSvc := blockapp.NewService(blockRepo, propertyRepo)
	priceRuleSvc := priceruleapp.NewService(priceRuleRepo, propertyRepo)
	emailSvc := emailapp.NewService(userRepo, email.NewMailer(cfg.Email))
	payoutSvc := payoutapp.NewService(payoutRepo, bookingRepo, propertyRepo, userRepo, paymentgw.NewDisburser(cfg.Payment), paymentgw.NewConnectGateway(cfg.Payment))
	privacySvc := privacyapp.NewService(userRepo, bookingRepo, paymentRepo, favoriteRepo, notificationRepo, payoutRepo, reviewRepo)
	reportSvc := reportapp.NewService(reportRepo, propertyRepo, reviewRepo)
	couponSvc := couponapp.NewService(couponRepo)
	userBlockSvc := userblockapp.NewService(userBlockRepo)
	offerSvc := offerapp.NewService(offerRepo, propertyRepo, bookingSvc)
	savedSearchSvc := savedsearchapp.NewService(savedSearchRepo, searchSvc, notificationSvc)
	alertingSvc := alertingapp.NewService(infraalerting.NewSilencer(cfg.Alerting))
	alertStateSvc := alertstateapp.NewService()
	realtimeHub := realtime.NewHub()
	realtimeSvc := realtimeapp.NewService(realtimeHub)

	// Notifications, payments, emails, host payouts and live updates are produced
	// by reacting to domain events.
	dispatcher.Subscribe(notificationSvc.EventHandler())
	dispatcher.Subscribe(paymentSvc.EventHandler())
	dispatcher.Subscribe(emailSvc.EventHandler())
	dispatcher.Subscribe(payoutSvc.EventHandler())
	dispatcher.Subscribe(realtimeSvc.EventHandler())

	// --- Observability -----------------------------------------------------
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(registry)

	// --- HTTP interface ----------------------------------------------------
	syncFn := func(c *gin.Context, claims auth.Claims) (*domainuser.User, error) {
		return userSvc.SyncFromIdentity(c.Request.Context(), userapp.Identity{
			Subject:  claims.Subject,
			Email:    claims.Email,
			FullName: claims.FullName(),
			Roles:    claims.RealmAccess.Roles,
		})
	}
	authMW := middleware.NewAuthMiddleware(verifier, syncFn)

	router := apphttp.NewRouter(apphttp.Deps{
		Config:   cfg,
		Metrics:  metrics,
		Registry: registry,
		Auth:     authMW,
		Handlers: apphttp.Handlers{
			Health:         handler.NewHealthHandler(pool),
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
			PaymentWebhook: handler.NewPaymentWebhookHandler(paymentSvc, paymentgw.NewWebhookVerifiers(cfg.Payment), webhookEventRepo, metrics),
			ConnectWebhook: handler.NewConnectWebhookHandler(payoutSvc, paymentgw.NewConnectWebhookVerifiers(cfg.Payment), webhookEventRepo, metrics),
			Alert:          handler.NewAlertHandler(alertingSvc, alertStateSvc, cfg.Alerting.WebhookToken),
			Privacy:        handler.NewPrivacyHandler(privacySvc),
			Coupon:         handler.NewCouponHandler(couponSvc),
			UserBlock:      handler.NewUserBlockHandler(userBlockSvc),
			Offer:          handler.NewOfferHandler(offerSvc),
			SavedSearch:    handler.NewSavedSearchHandler(savedSearchSvc),
			PriceRule:      handler.NewPriceRuleHandler(priceRuleSvc),
		},
	})

	// --- Background jobs ---------------------------------------------------
	// Periodically prune the webhook dedupe table so it does not grow forever.
	sched := scheduler.New()
	retentionDays := cfg.Security.WebhookRetentionDays
	sched.Add(scheduler.Job{
		Name:       "webhook-events-cleanup",
		Interval:   cfg.Security.WebhookCleanupInterval,
		RunAtStart: true,
		Run: func(ctx context.Context) error {
			cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
			deleted, err := webhookEventRepo.DeleteOlderThan(ctx, cutoff)
			if err == nil && deleted > 0 {
				slog.Info("webhook-events cleanup", "deleted", deleted, "cutoff", cutoff)
			}
			return err
		},
	})
	// Re-deliver any outbox events left unprocessed by a previous crash, then
	// keep a periodic relay running as a safety net for transient failures.
	if n, err := eventPublisher.Recover(ctx, 500); err != nil {
		slog.Warn("outbox recovery failed at startup", "error", err)
	} else if n > 0 {
		slog.Info("outbox recovery re-delivered events", "count", n)
	}
	sched.Add(scheduler.Job{
		Name:     "outbox-relay",
		Interval: time.Minute,
		Run: func(ctx context.Context) error {
			_, err := eventPublisher.Recover(ctx, 500)
			return err
		},
	})
	// Alert users when new listings match their saved searches.
	sched.Add(scheduler.Job{
		Name:     "saved-search-alerts",
		Interval: time.Hour,
		Run:      savedSearchSvc.RunAlerts,
	})
	sched.Start(ctx)

	srv := &http.Server{
		Addr:         ":" + cfg.HTTP.Port,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	// --- Run with graceful shutdown ---------------------------------------
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "port", cfg.HTTP.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case sig := <-stop:
		slog.Info("shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("server stopped cleanly")
	return nil
}
