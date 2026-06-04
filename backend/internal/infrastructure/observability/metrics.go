// Package observability wires Prometheus metrics for the API.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the application's Prometheus collectors.
type Metrics struct {
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPInFlight        prometheus.Gauge
	BookingsCreated     prometheus.Counter
	PropertiesCreated   prometheus.Counter
	WebhookEventsTotal  *prometheus.CounterVec
	RateLimitedTotal    *prometheus.CounterVec
	// S29 — new-feature counters. Each increments at the point of the
	// successful business event, NOT on a transport-layer status code, so
	// retries / partial failures upstream don't double-count.
	KYCStepUpRequired      prometheus.Counter
	SplitPaymentsCompleted prometheus.Counter
	DisputesOpened         prometheus.Counter
	CohostActions          *prometheus.CounterVec
	// S74 — fraud assessor outcomes, labeled by Assessment.Level
	// (low/medium/high) so ops can graph the risk distribution and
	// alert on level=high spikes. Incremented after every successful
	// Save in fraudapp.Service.Assess.
	FraudAssessmentsTotal *prometheus.CounterVec
	// S85 — experience-booking lifecycle events, labeled by EventName
	// (experiencebooking.created / .confirmed / .cancelled). Incremented
	// after the application service hands the event to the dispatcher,
	// so subscriber failures don't suppress the metric.
	ExperienceBookingEventsTotal *prometheus.CounterVec
	// S113 — offer lifecycle events (WF-GAP-008 follow-on to S99/S106),
	// labeled by EventName (offer.created / .declined / .withdrawn).
	// Incremented after the offer service hands the event to the
	// publisher, so subscriber failures don't suppress the metric and
	// ops can graph the offer flow rate.
	OffersTotal *prometheus.CounterVec
	// S97 — outbox observability (WF-GAP-018). OutboxPending tracks the
	// number of records the recovery scan would still pick up; spikes mean
	// the dispatcher is falling behind or a subscriber is stuck. DLQ
	// counts records promoted to dead-letter, labeled by event name.
	OutboxPending  prometheus.Gauge
	OutboxDLQTotal *prometheus.CounterVec
}

// NewMetrics registers and returns the metric collectors. It uses a dedicated
// registry so the caller controls exposition.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)
	return &Metrics{
		HTTPRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_http_requests_total",
				Help: "Total number of HTTP requests processed, labeled by method, route and status.",
			},
			[]string{"method", "route", "status"},
		),
		HTTPRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "airhost_http_request_duration_seconds",
				Help:    "HTTP request latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		HTTPInFlight: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "airhost_http_in_flight_requests",
				Help: "Number of HTTP requests currently being served.",
			},
		),
		BookingsCreated: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "airhost_bookings_created_total",
				Help: "Total number of bookings created.",
			},
		),
		PropertiesCreated: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "airhost_properties_created_total",
				Help: "Total number of properties created.",
			},
		),
		WebhookEventsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_webhook_events_total",
				Help: "Total payment-gateway webhook events, labeled by provider and outcome.",
			},
			[]string{"provider", "outcome"},
		),
		RateLimitedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_rate_limited_total",
				Help: "Total requests rejected by a rate limiter, labeled by route.",
			},
			[]string{"route"},
		),
		KYCStepUpRequired: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "airhost_kyc_step_up_required_total",
				Help: "Total bookings rejected because the guest needs a higher-tier identity check for a high-value reservation (S19).",
			},
		),
		SplitPaymentsCompleted: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "airhost_split_payments_completed_total",
				Help: "Total split-payment plans where every share has been authorised and the booking auto-confirmed (S20a).",
			},
		),
		DisputesOpened: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "airhost_disputes_opened_total",
				Help: "Total Resolution Center cases opened by guests or hosts (S13).",
			},
		),
		CohostActions: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_cohost_actions_total",
				Help: "Total co-host grant mutations, labeled by action (invited / permissions_changed / revoked).",
			},
			[]string{"action"},
		),
		FraudAssessmentsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_fraud_assessments_total",
				Help: "Total fraud assessments persisted, labeled by resulting risk level (low / medium / high) so ops can graph the risk distribution and alert on level=high spikes (S74).",
			},
			[]string{"level"},
		),
		ExperienceBookingEventsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_experience_booking_events_total",
				Help: "Total experience-booking domain events dispatched, labeled by EventName (experiencebooking.created / .confirmed / .cancelled) (S85).",
			},
			[]string{"event"},
		),
		OffersTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_offers_total",
				Help: "Total offer lifecycle events published, labeled by EventName (offer.created / .declined / .withdrawn) — follow-on to S99/WF-GAP-008 (S113).",
			},
			[]string{"event"},
		),
		OutboxPending: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "airhost_outbox_pending_count",
				Help: "Number of outbox records still awaiting delivery — refreshed each recovery cycle (S97).",
			},
		),
		OutboxDLQTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "airhost_outbox_dlq_total",
				Help: "Outbox records promoted to dead-letter, labeled by event name and reason (S97).",
			},
			[]string{"event", "reason"},
		),
	}
}
