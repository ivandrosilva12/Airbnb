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

	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	blockapp "github.com/airhost/backend/internal/application/block"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	messageapp "github.com/airhost/backend/internal/application/message"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	propertyapp "github.com/airhost/backend/internal/application/property"
	reviewapp "github.com/airhost/backend/internal/application/review"
	searchapp "github.com/airhost/backend/internal/application/search"
	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/config"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/auth"
	"github.com/airhost/backend/internal/infrastructure/observability"
	paymentgw "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/postgres"
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
	favoriteRepo := postgres.NewFavoriteRepository(pool)
	notificationRepo := postgres.NewNotificationRepository(pool)
	paymentRepo := postgres.NewPaymentRepository(pool)
	blockRepo := postgres.NewBlockRepository(pool)

	// --- Domain events ----------------------------------------------------
	// A synchronous in-process dispatcher fans domain events out to subscribers.
	dispatcher := event.NewDispatcher()

	// --- Application services ---------------------------------------------
	userSvc := userapp.NewService(userRepo)
	propertySvc := propertyapp.NewService(propertyRepo, objectStore)
	bookingSvc := bookingapp.NewService(bookingRepo, propertyRepo, blockRepo, cfg.Pricing.ServiceFeeRate, dispatcher)
	reviewSvc := reviewapp.NewService(reviewRepo, bookingRepo, propertyRepo)
	messageSvc := messageapp.NewService(messageRepo, propertyRepo, dispatcher)
	searchSvc := searchapp.NewService(propertyRepo, bookingRepo, blockRepo)
	favoriteSvc := favoriteapp.NewService(favoriteRepo, propertyRepo)
	notificationSvc := notificationapp.NewService(notificationRepo)
	paymentSvc := paymentapp.NewService(paymentRepo, paymentgw.NewFakeGateway(), bookingRepo, propertyRepo)
	analyticsSvc := analyticsapp.NewService(propertyRepo, bookingRepo, paymentRepo)
	blockSvc := blockapp.NewService(blockRepo, propertyRepo)

	// Notifications and payments are produced by reacting to domain events.
	dispatcher.Subscribe(notificationSvc.EventHandler())
	dispatcher.Subscribe(paymentSvc.EventHandler())

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
		})
	}
	authMW := middleware.NewAuthMiddleware(verifier, syncFn)

	router := apphttp.NewRouter(apphttp.Deps{
		Config:   cfg,
		Metrics:  metrics,
		Registry: registry,
		Auth:     authMW,
		Handlers: apphttp.Handlers{
			Health:       handler.NewHealthHandler(pool),
			User:         handler.NewUserHandler(userSvc),
			Property:     handler.NewPropertyHandler(propertySvc, searchSvc, metrics),
			Booking:      handler.NewBookingHandler(bookingSvc, metrics),
			Review:       handler.NewReviewHandler(reviewSvc),
			Message:      handler.NewMessageHandler(messageSvc),
			Favorite:     handler.NewFavoriteHandler(favoriteSvc),
			Notification: handler.NewNotificationHandler(notificationSvc),
			Payment:      handler.NewPaymentHandler(paymentSvc),
			Analytics:    handler.NewAnalyticsHandler(analyticsSvc),
			Block:        handler.NewBlockHandler(blockSvc),
		},
	})

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
