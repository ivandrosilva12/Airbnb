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
	"strings"
	"syscall"
	"time"

	alertingapp "github.com/airhost/backend/internal/application/alerting"
	auditapp "github.com/airhost/backend/internal/application/audit"
	alertstateapp "github.com/airhost/backend/internal/application/alertstate"
	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	blockapp "github.com/airhost/backend/internal/application/block"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	couponapp "github.com/airhost/backend/internal/application/coupon"
	disputeapp "github.com/airhost/backend/internal/application/dispute"
	emailapp "github.com/airhost/backend/internal/application/email"
	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	favoriteapp "github.com/airhost/backend/internal/application/favorite"
	houserulesapp "github.com/airhost/backend/internal/application/houserules"
	identityapp "github.com/airhost/backend/internal/application/identity"
	messageapp "github.com/airhost/backend/internal/application/message"
	messagetemplateapp "github.com/airhost/backend/internal/application/messagetemplate"
	notificationapp "github.com/airhost/backend/internal/application/notification"
	offerapp "github.com/airhost/backend/internal/application/offer"
	paymentapp "github.com/airhost/backend/internal/application/payment"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	priceruleapp "github.com/airhost/backend/internal/application/pricerule"
	privacyapp "github.com/airhost/backend/internal/application/privacy"
	propertyapp "github.com/airhost/backend/internal/application/property"
	pushtokenapp "github.com/airhost/backend/internal/application/pushtoken"
	realtimeapp "github.com/airhost/backend/internal/application/realtime"
	reportapp "github.com/airhost/backend/internal/application/report"
	experienceapp "github.com/airhost/backend/internal/application/experience"
	experiencebookingapp "github.com/airhost/backend/internal/application/experiencebooking"
	fraudapp "github.com/airhost/backend/internal/application/fraud"
	reviewapp "github.com/airhost/backend/internal/application/review"
	savedsearchapp "github.com/airhost/backend/internal/application/savedsearch"
	searchapp "github.com/airhost/backend/internal/application/search"
	splitpaymentapp "github.com/airhost/backend/internal/application/splitpayment"
	taxapp "github.com/airhost/backend/internal/application/tax"
	taxremittanceapp "github.com/airhost/backend/internal/application/taxremittance"
	userapp "github.com/airhost/backend/internal/application/user"
	userblockapp "github.com/airhost/backend/internal/application/userblock"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/splitpayment"
	domainuser "github.com/airhost/backend/internal/domain/user"
	infraalerting "github.com/airhost/backend/internal/infrastructure/alerting"
	"github.com/airhost/backend/internal/infrastructure/auth"
	"github.com/airhost/backend/internal/infrastructure/cache"
	"github.com/airhost/backend/internal/infrastructure/email"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/airhost/backend/internal/observability/tracing"
	paymentgw "github.com/airhost/backend/internal/infrastructure/payment"
	"github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	infrapush "github.com/airhost/backend/internal/infrastructure/push"
	"github.com/airhost/backend/internal/infrastructure/realtime"
	"github.com/airhost/backend/internal/infrastructure/scheduler"
	"github.com/airhost/backend/internal/infrastructure/storage"
	apphttp "github.com/airhost/backend/internal/interfaces/http"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	// Wrap the JSON handler with PII redaction (S44) so emails, IPv4,
	// card-shaped digits and JWT tokens get masked in every log line
	// regardless of which call site emitted them. logctx.LoggerFrom
	// derives request-scoped loggers from slog.Default, so the
	// redaction propagates through the whole structured-log chain.
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(logctx.NewRedactingHandler(base))
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

	// Validate secrets / placeholders at startup (S50). In production
	// any blocker fails the boot — refusing to start with a known-weak
	// credential is the safest default. In non-production we log
	// warnings and keep going so dev workflows aren't blocked by
	// placeholder values they intentionally chose.
	if vres, verr := cfg.Validate(); verr != nil {
		return verr
	} else if len(vres.Warnings) > 0 {
		for _, w := range vres.Warnings {
			slog.Warn("config validation warning", "issue", w)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Tracing (S52) -----------------------------------------------------
	// Init returns a shutdown function that flushes the last in-flight
	// span batch on process exit — required for OTLP correctness; a
	// no-op for the noop exporter.
	traceShutdown, err := tracing.Init(ctx, tracing.Config{
		Exporter:       cfg.Tracing.Exporter,
		ServiceName:    cfg.App.Name,
		Environment:    cfg.App.Environment,
		OTLPEndpoint:   cfg.Tracing.OTLPEndpoint,
		SampleRatio:    cfg.Tracing.SampleRatio,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Bounded timeout so a hung OTLP endpoint can't block shutdown
		// forever.
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := traceShutdown(shutdownCtx); err != nil {
			slog.Warn("tracing shutdown error", "err", err)
		}
	}()
	slog.Info("tracing initialised", "exporter", cfg.Tracing.Exporter)

	// --- Cache (S53) -------------------------------------------------------
	// Pick the port.Cache adapter from config. Unknown backend defaults to
	// noop with a warning rather than failing the boot — a typo on a
	// nonessential infra knob shouldn't down the API.
	var cacheBackend port.Cache
	switch strings.ToLower(cfg.Cache.Backend) {
	case "redis":
		redisCache, err := cache.NewRedis(cfg.Cache.RedisURL)
		if err != nil {
			return err
		}
		defer redisCache.Close()
		cacheBackend = redisCache
	case "memory":
		cacheBackend = cache.NewMemory()
	case "", "noop":
		cacheBackend = cache.NewNoop()
	default:
		slog.Warn("unknown CACHE_BACKEND, falling back to noop", "value", cfg.Cache.Backend)
		cacheBackend = cache.NewNoop()
	}
	slog.Info("cache initialised", "backend", cfg.Cache.Backend)

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
	depositRepo := postgres.NewDepositRepository(pool)
	payoutRepo := postgres.NewPayoutRepository(pool)
	blockRepo := postgres.NewBlockRepository(pool)
	priceRuleRepo := postgres.NewPriceRuleRepository(pool)
	identityRepo := postgres.NewIdentityRepository(pool)
	reportRepo := postgres.NewReportRepository(pool)
	couponRepo := postgres.NewCouponRepository(pool)
	webhookEventRepo := postgres.NewWebhookEventRepository(pool)
	pushTokenRepo := postgres.NewPushTokenRepository(pool)
	disputeRepo := postgres.NewDisputeRepository(pool)
	cohostRepo := postgres.NewCohostRepository(pool)
	splitPaymentRepo := postgres.NewSplitPaymentRepository(pool)
	messageTemplateRepo := postgres.NewMessageTemplateRepository(pool)
	auditRepo := postgres.NewAuditRepository(pool)
	houseRulesRepo := postgres.NewHouseRulesRepository(pool)
	taxRepo := postgres.NewTaxRepository(pool)

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
	bookingSvc := bookingapp.NewService(bookingRepo, propertyRepo, blockRepo, couponRepo, priceRuleRepo, cfg.Pricing.ServiceFeeRate, identitySvc, cfg.Identity.RequireKYCToBook, uow).
		WithHighValueThresholds(cfg.Identity.HighValueThresholdsCents)
	reviewSvc := reviewapp.NewService(reviewRepo, bookingRepo, propertyRepo)
	messageSvc := messageapp.NewService(messageRepo, propertyRepo, userBlockRepo, objectStore, uow)
	// cohostSvc is wired below (after blockSvc) and message svc gets it via WithCohosts later.
	searchSvc := searchapp.NewService(propertyRepo, bookingRepo, blockRepo)
	favoriteSvc := favoriteapp.NewService(favoriteRepo, propertyRepo)
	// Native push: build a sender from configured providers (FCM/APNs) — the
	// factory falls back to a log-only sender when nothing is configured, so the
	// platform always has *some* sender behind the port. The notification
	// service mirrors every newly-created notification through the push channel,
	// honouring per-category opt-outs on the user aggregate.
	pushSender := infrapush.NewSender(cfg.Push)
	pushTokenSvc := pushtokenapp.NewService(pushTokenRepo, userRepo, pushSender)
	notificationSvc := notificationapp.NewService(notificationRepo).
		WithPush(pushTokenSvc.AsNotifier()).
		// S93 / WF-GAP-011 — split-payment completion subscriber needs
		// these to resolve the organizer + payer list.
		WithBookings(bookingRepo).
		WithSplitPayments(splitPaymentRepo)
	// S88 — share one PaymentGateway instance across the payment subscriber
	// AND the split-payment service so the per-share authorize/refund flow
	// hits the same provider as the regular booking flow.
	paymentGateway := paymentgw.NewGateway(cfg.Payment)
	paymentSvc := paymentapp.NewService(paymentRepo, paymentGateway, bookingRepo, propertyRepo).
		WithDeposits(depositRepo).
		// S94 / WF-GAP-010 — publisher lets the async-auth webhook emit
		// PaymentAuthorized so the booking subscriber can promote
		// pending→confirmed without the webhook handler touching the
		// booking aggregate directly.
		WithPublisher(dispatcher)
	analyticsSvc := analyticsapp.NewService(propertyRepo, bookingRepo, paymentRepo)
	cohostSvc := propertyapp.NewCohostService(cohostRepo, propertyRepo, userRepo).
		// S99 / WF-GAP-016 — emit CohostInvited so the invitee gets a
		// notification + SSE hint when a host grants them access.
		WithPublisher(dispatcher)
	blockSvc := blockapp.NewService(blockRepo, propertyRepo).WithCohosts(cohostSvc)
	priceRuleSvc := priceruleapp.NewService(priceRuleRepo, propertyRepo).WithCohosts(cohostSvc)
	messageSvc.WithCohosts(cohostSvc)
	// Split payment: wire the SplitPayment service, then plug a tiny adapter
	// into bookingapp so it can create a split alongside a booking. S88 —
	// hook the same PaymentGateway so AuthorizeShare places a real per-share
	// hold and Cancel / the BookingCancelled subscription release them.
	splitPaymentSvc := splitpaymentapp.NewService(splitPaymentRepo, userRepo, uow).
		WithGateway(paymentGateway)
	bookingSvc.WithSplitter(splitterAdapter{svc: splitPaymentSvc}, userEmailResolver{users: userRepo})
	dispatcher.Subscribe(bookingSvc.EventHandler())
	messageTemplateSvc := messagetemplateapp.NewService(messageTemplateRepo)
	auditSvc := auditapp.NewService(auditRepo)
	houseRulesSvc := houserulesapp.NewService(houseRulesRepo, propertyRepo)
	taxSvc := taxapp.NewService(taxRepo, propertyRepo)
	// S49 — plug the tax BC into the booking pricing flow so every
	// reservation lands with itemised jurisdiction lines + total.
	bookingSvc.WithTaxQuoter(taxQuoterAdapter{svc: taxSvc})
	// S62 — read-only remittance aggregator over the same booking data.
	taxRemittanceSvc := taxremittanceapp.NewService(bookingRepo, propertyRepo)
	// S68 + S72 — fraud detection BC. Persisted to Postgres via
	// migration 0050; the memory adapter remains available for tests
	// and local experimentation. Reuses the same per-currency high-
	// value table as the KYC step-up gate so the two surfaces don't
	// drift over time.
	fraudRepo := postgres.NewFraudRepository(pool)
	fraudSvc := fraudapp.NewService(fraudRepo, bookingRepo, userRepo, identitySvc, cfg.Identity.HighValueThresholdsCents)
	// S71 + S75 — Experiences BC. Persisted to Postgres via migration
	// 0051; the memory adapter remains available for the e2e harness
	// and local experimentation.
	experienceRepo := postgres.NewExperienceRepository(pool)
	experienceSvc := experienceapp.NewService(experienceRepo)
	// S80 — ExperienceBooking BC. Postgres-backed since S81 (migration
	// 0052). Lifecycle events fan out through the in-process dispatcher
	// (S85); the Prometheus sink is attached after metrics is constructed
	// further below, mirroring the fraud service's two-step wiring. The
	// platform service-fee rate is the same one applied to property
	// bookings.
	experienceBookingRepo := postgres.NewExperienceBookingRepository(pool)
	experienceBookingSvc := experiencebookingapp.NewService(experienceBookingRepo, experienceRepo, cfg.Pricing.ServiceFeeRate).
		WithDispatcher(dispatcher).
		// S87 — route the lifecycle writes through the same UoW the
		// property-booking service uses so a payment-subscriber crash
		// mid-publish can never lose an ExperienceBookingCreated event
		// (WF-GAP-020). The outbox table is shared; the UoW already
		// dispatches recorded events post-commit through `dispatcher`.
		WithUnitOfWork(uow)
	// Plug the verifier into the booking service so Create enforces the
	// guest acknowledged the listing's current rules version (S47). The
	// BookingHandler below records the per-booking acceptance row right
	// after a successful Create.
	bookingSvc.WithHouseRules(houseRulesSvc)
	emailSvc := emailapp.NewService(userRepo, email.NewMailer(cfg.Email)).
		// S93 / WF-GAP-011 — same split-completion fan-out as notifications.
		WithBookings(bookingRepo).
		WithSplitPayments(splitPaymentRepo)
	payoutSvc := payoutapp.NewService(payoutRepo, bookingRepo, propertyRepo, userRepo, paymentgw.NewDisburser(cfg.Payment), paymentgw.NewConnectGateway(cfg.Payment)).
		WithSplitPayments(splitPaymentRepo) // S88 — credit per-share on split bookings
	// privacySvc orchestrates GDPR self-service (export + erase). The erase
	// pipeline (S90 / WF-GAP-009) sweeps every PII-bearing table — adding a
	// new table to the platform means extending NewService AND this call
	// site so the schema and the erase pipeline cannot drift apart.
	privacySvc := privacyapp.NewService(
		userRepo, bookingRepo, paymentRepo, favoriteRepo, notificationRepo, payoutRepo, reviewRepo,
		pushTokenRepo, savedSearchRepo, identityRepo, disputeRepo, splitPaymentRepo, cohostRepo,
		messageTemplateRepo, houseRulesRepo, fraudRepo, auditRepo,
	)
	reportSvc := reportapp.NewService(reportRepo, propertyRepo, reviewRepo)
	couponSvc := couponapp.NewService(couponRepo)
	userBlockSvc := userblockapp.NewService(userBlockRepo)
	offerSvc := offerapp.NewService(offerRepo, propertyRepo, bookingSvc).
		// S99 / WF-GAP-008 — emit OfferCreated/Declined/Withdrawn so the
		// notification, email and realtime subscribers fan the right
		// hint out to host or guest.
		WithPublisher(dispatcher)
	savedSearchSvc := savedsearchapp.NewService(savedSearchRepo, searchSvc, notificationSvc)
	// Dispute resolution runs inside a UnitOfWork (S89 — WF-GAP-003 +
	// WF-GAP-013): the dispute row and its DisputeOpened / DisputeResolved
	// event commit atomically through the outbox, so a crash between the
	// two can no longer leave a decided case with no notification or a
	// notified party with no decision on file. The PaymentAdjuster
	// refund/damage call still runs BEFORE the transaction (the payment
	// gateway is not transactional with our DB) and remains idempotent on
	// the (RefKind="dispute", RefID=disputeID) key.
	disputeSvc := disputeapp.NewService(disputeRepo, bookingRepo, propertyRepo, dispatcher).
		WithPaymentAdjuster(paymentSvc).
		WithUnitOfWork(uow).
		WithOutbox(outboxRepo)
	alertingSvc := alertingapp.NewService(infraalerting.NewSilencer(cfg.Alerting))
	alertStateSvc := alertstateapp.NewService()
	realtimeHub := realtime.NewHub()
	realtimeSvc := realtimeapp.NewService(realtimeHub).
		WithBookings(bookingRepo).
		WithSplitPayments(splitPaymentRepo)

	// Notifications, payments, emails, host payouts and live updates are produced
	// by reacting to domain events.
	dispatcher.Subscribe(notificationSvc.EventHandler())
	dispatcher.Subscribe(paymentSvc.EventHandler())
	dispatcher.Subscribe(emailSvc.EventHandler())
	dispatcher.Subscribe(payoutSvc.EventHandler())
	// S88 — release per-share holds on cancellation (WF-GAP-005). Lives
	// next to the other money-flow subscribers; the split-payment service
	// reads the share table and calls gateway.Refund per paid share.
	dispatcher.Subscribe(splitPaymentSvc.EventHandler())
	dispatcher.Subscribe(realtimeSvc.EventHandler())

	// --- Observability -----------------------------------------------------
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(registry)
	// S74 — plug the metrics sink into the fraud assessor so every
	// Assess Save increments the FraudAssessmentsTotal counter
	// labeled by the resulting risk level.
	fraudSvc.WithMetrics(metrics)
	experienceBookingSvc.WithMetrics(metrics)
	// S85 — same fluent-wire pattern for experience-booking lifecycle
	// events: every dispatched created / confirmed / cancelled bumps
	// the ExperienceBookingEventsTotal counter labeled by EventName.
	experienceBookingSvc.WithMetrics(metrics)
	// S97 — outbox DLQ + depth metric (WF-GAP-018). After 5 failed
	// delivery attempts the relay promotes the record to dead-letter
	// state, keeping the recovery scan cheap and surfacing stuck events
	// to operators via the airhost_outbox_dlq_total counter. The depth
	// gauge is sampled at the end of each recovery cycle.
	eventPublisher.
		WithMaxAttempts(5).
		WithDepthObserver(func(n int) { metrics.OutboxPending.Set(float64(n)) }).
		WithDLQObserver(func(name, reason string) { metrics.OutboxDLQTotal.WithLabelValues(name, reason).Inc() })

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
			User:           handler.NewUserHandler(userSvc).WithAudit(auditSvc),
			Property:       handler.NewPropertyHandler(propertySvc, searchSvc, metrics).WithAudit(auditSvc).WithCache(cacheBackend, cfg.Cache.PropertyTTL),
			Booking:        handler.NewBookingHandler(bookingSvc, metrics).WithHouseRules(houseRulesSvc).WithFraud(fraudSvc),
			Review:         handler.NewReviewHandler(reviewSvc),
			Message:        handler.NewMessageHandler(messageSvc),
			Favorite:       handler.NewFavoriteHandler(favoriteSvc),
			Notification:   handler.NewNotificationHandler(notificationSvc),
			Payment:        handler.NewPaymentHandler(paymentSvc),
			Analytics:      handler.NewAnalyticsHandler(analyticsSvc),
			Block:          handler.NewBlockHandler(blockSvc),
			Payout:         handler.NewPayoutHandler(payoutSvc),
			Realtime:       handler.NewRealtimeHandler(realtimeHub),
			Identity:       handler.NewIdentityHandler(identitySvc).WithAudit(auditSvc),
			Report:         handler.NewReportHandler(reportSvc).WithAudit(auditSvc),
			PaymentWebhook: handler.NewPaymentWebhookHandler(paymentSvc, paymentgw.NewWebhookVerifiers(cfg.Payment), webhookEventRepo, metrics),
			ConnectWebhook: handler.NewConnectWebhookHandler(payoutSvc, paymentgw.NewConnectWebhookVerifiers(cfg.Payment), webhookEventRepo, metrics),
			Alert:          handler.NewAlertHandler(alertingSvc, alertStateSvc, cfg.Alerting.WebhookToken),
			Privacy:        handler.NewPrivacyHandler(privacySvc),
			Coupon:         handler.NewCouponHandler(couponSvc).WithAudit(auditSvc),
			UserBlock:      handler.NewUserBlockHandler(userBlockSvc),
			Offer:          handler.NewOfferHandler(offerSvc),
			SavedSearch:    handler.NewSavedSearchHandler(savedSearchSvc),
			PriceRule:      handler.NewPriceRuleHandler(priceRuleSvc),
			PushToken:      handler.NewPushTokenHandler(pushTokenSvc).WithVAPIDPublicKey(cfg.Push.WebPushPublicKey),
			Audit:          handler.NewAuditHandler(auditSvc),
			Dispute:        handler.NewDisputeHandler(disputeSvc, metrics).WithAudit(auditSvc),
			Cohost:          handler.NewCohostHandler(cohostSvc, userRepo, metrics),
			SplitPayment:    handler.NewSplitPaymentHandler(splitPaymentSvc, metrics),
			MessageTemplate: handler.NewMessageTemplateHandler(messageTemplateSvc),
			HouseRules:      handler.NewHouseRulesHandler(houseRulesSvc),
			Tax:             handler.NewTaxHandler(taxSvc).WithAudit(auditSvc),
			TaxRemittance:   handler.NewTaxRemittanceHandler(taxRemittanceSvc),
			Fraud:           handler.NewFraudHandler(fraudSvc),
			Experience:      handler.NewExperienceHandler(experienceSvc),
			ExperienceBooking: handler.NewExperienceBookingHandler(experienceBookingSvc),
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
	// Flip confirmed experience bookings to completed once their session
	// window has elapsed. Five minutes is a sensible compromise: tight
	// enough for the review-request workflow to fire promptly, loose
	// enough that the per-tick DB load stays trivial.
	sched.Add(scheduler.Job{
		Name:     "experience-bookings-auto-complete",
		Interval: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			_, err := experienceBookingSvc.AutoCompleteOverdue(ctx)
			return err
		},
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

// splitterAdapter bridges the bookingapp.Splitter port onto the concrete
// splitpaymentapp.Service. The booking package stays free of an upward
// dependency on splitpaymentapp this way — composition lives at the
// composition root.
type splitterAdapter struct {
	svc *splitpaymentapp.Service
}

func (a splitterAdapter) CreateInTx(ctx context.Context, tx port.Tx, in bookingapp.SplitterCreateInput) error {
	shares := make([]splitpayment.ShareInput, 0, len(in.Shares))
	for _, s := range in.Shares {
		shares = append(shares, splitpayment.ShareInput{Email: s.Email, AmountCents: s.AmountCents})
	}
	_, err := a.svc.CreateInTx(ctx, tx, splitpaymentapp.CreateInput{
		BookingID:      in.BookingID,
		OrganizerID:    in.OrganizerID,
		OrganizerEmail: in.OrganizerEmail,
		Currency:       in.Currency,
		TotalCents:     in.TotalCents,
		Shares:         shares,
	})
	return err
}

// userEmailResolver satisfies bookingapp.OrganizerResolver by looking up
// the organizer's email via the user repository.
type userEmailResolver struct {
	users domainuser.Repository
}

func (r userEmailResolver) EmailByID(ctx context.Context, id uuid.UUID) (string, error) {
	u, err := r.users.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	return u.Email, nil
}

// taxQuoterAdapter bridges bookingapp.TaxQuoter onto taxapp.Service so
// the booking package stays free of any tax import (S49). Mirrors the
// testTaxQuoter wired in the e2e harness.
type taxQuoterAdapter struct {
	svc *taxapp.Service
}

func (a taxQuoterAdapter) QuoteForBooking(ctx context.Context, in bookingapp.TaxQuoteInput) (bookingapp.TaxQuoteResult, error) {
	q, err := a.svc.QuoteForBooking(ctx, taxapp.BookingQuoteInput{
		PropertyID:    in.PropertyID,
		CheckIn:       in.CheckIn,
		Nights:        in.Nights,
		Guests:        in.Guests,
		SubtotalCents: in.SubtotalCents,
	})
	if err != nil {
		return bookingapp.TaxQuoteResult{}, err
	}
	lines := make([]booking.TaxLine, 0, len(q.Lines))
	for _, l := range q.Lines {
		lines = append(lines, booking.TaxLine{Name: l.Name, AmountCents: l.AmountCents})
	}
	return bookingapp.TaxQuoteResult{Lines: lines, TotalCents: q.TotalCents, Currency: q.Currency}, nil
}
