package middleware

import (
	"strconv"
	"time"

	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/gin-gonic/gin"
)

// Metrics records Prometheus metrics for each request. It uses the matched
// route template (not the raw path) as a label to bound cardinality.
func Metrics(m *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		m.HTTPInFlight.Inc()

		c.Next()

		m.HTTPInFlight.Dec()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		m.HTTPRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		m.HTTPRequestDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}
