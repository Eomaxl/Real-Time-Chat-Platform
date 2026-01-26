package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPMetricsMiddleware creates a Gin middleware for collecting HTTP metrics
func HTTPMetricsMiddleware(metrics *ServiceMetrics, serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Increment in-flight requests
		metrics.HTTPRequestsInFlight.WithLabelValues(serviceName).Inc()
		defer metrics.HTTPRequestsInFlight.WithLabelValues(serviceName).Dec()

		// Process request
		c.Next()

		// Record metrics
		duration := time.Since(start)
		status := strconv.Itoa(c.Writer.Status())
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = "unknown"
		}

		metrics.RecordHTTPRequest(serviceName, c.Request.Method, endpoint, status, duration)
		metrics.HTTPResponseSize.WithLabelValues(serviceName, endpoint).Observe(float64(c.Writer.Size()))

		// Check for SLA violations (p95 latency targets)
		if duration.Milliseconds() > 200 && c.Request.Method == "POST" {
			metrics.RecordSLAViolation(serviceName, "message_latency", "200ms")
		}
		if duration.Milliseconds() > 150 && endpoint == "/v1/calls/:id/signaling" {
			metrics.RecordSLAViolation(serviceName, "signaling_latency", "150ms")
		}
	}
}

// RecoveryMiddleware creates a Gin middleware for panic recovery with metrics
func RecoveryMiddleware(metrics *ServiceMetrics, serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				metrics.PanicsTotal.WithLabelValues(serviceName, c.FullPath()).Inc()
				metrics.ErrorsTotal.WithLabelValues(serviceName, "panic", "critical").Inc()
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}
