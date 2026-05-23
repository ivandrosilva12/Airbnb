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
	}
}
