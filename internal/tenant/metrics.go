package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds tenant-specific Prometheus metrics
type Metrics struct {
	// API metrics
	apiRequestsTotal   *prometheus.CounterVec
	apiRequestDuration *prometheus.HistogramVec
	apiRateLimitHits   *prometheus.CounterVec

	// Message metrics
	messagesTotal        *prometheus.CounterVec
	messageRateLimitHits *prometheus.CounterVec

	// Resource usage metrics
	channelsTotal       *prometheus.GaugeVec
	activeCallsTotal    *prometheus.GaugeVec
	websocketConnsTotal *prometheus.GaugeVec
	storageBytes        *prometheus.GaugeVec

	// Quota metrics
	quotaUsagePercent *prometheus.GaugeVec
	quotaExceeded     *prometheus.CounterVec

	// Priority queue metrics
	queueDepth          *prometheus.GaugeVec
	queueProcessingTime *prometheus.HistogramVec
}

// NewMetrics creates a new tenant metrics collector
func NewMetrics() *Metrics {
	return &Metrics{
		apiRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tenant_api_requests_total",
				Help: "Total number of API requests per tenant",
			},
			[]string{"tenant_id", "tier", "method", "endpoint", "status"},
		),
		apiRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tenant_api_request_duration_seconds",
				Help:    "API request duration per tenant",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"tenant_id", "tier", "method", "endpoint"},
		),
		apiRateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tenant_api_rate_limit_hits_total",
				Help: "Total number of rate limit hits per tenant",
			},
			[]string{"tenant_id", "tier"},
		),
		messagesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tenant_messages_total",
				Help: "Total number of messages per tenant",
			},
			[]string{"tenant_id", "tier", "channel_type"},
		),
		messageRateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tenant_message_rate_limit_hits_total",
				Help: "Total number of message rate limit hits per tenant",
			},
			[]string{"tenant_id", "tier"},
		),
		channelsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_channels_total",
				Help: "Total number of channels per tenant",
			},
			[]string{"tenant_id", "tier"},
		),
		activeCallsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_active_calls_total",
				Help: "Total number of active calls per tenant",
			},
			[]string{"tenant_id", "tier"},
		),
		websocketConnsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_websocket_connections_total",
				Help: "Total number of WebSocket connections per tenant",
			},
			[]string{"tenant_id", "tier"},
		),
		storageBytes: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_storage_bytes",
				Help: "Total storage used by tenant in bytes",
			},
			[]string{"tenant_id", "tier"},
		),
		quotaUsagePercent: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_quota_usage_percent",
				Help: "Percentage of quota used per tenant and resource type",
			},
			[]string{"tenant_id", "tier", "resource_type"},
		),
		quotaExceeded: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tenant_quota_exceeded_total",
				Help: "Total number of quota exceeded events per tenant",
			},
			[]string{"tenant_id", "tier", "resource_type"},
		),
		queueDepth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "tenant_queue_depth",
				Help: "Number of messages in priority queue per tenant",
			},
			[]string{"tenant_id", "tier", "queue_type"},
		),
		queueProcessingTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tenant_queue_processing_time_seconds",
				Help:    "Time to process messages from priority queue",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"tenant_id", "tier", "queue_type"},
		),
	}
}

// RecordAPIRequest records an API request metric
func (m *Metrics) RecordAPIRequest(tenantID, tier, method, endpoint, status string, duration time.Duration) {
	m.apiRequestsTotal.WithLabelValues(tenantID, tier, method, endpoint, status).Inc()
	m.apiRequestDuration.WithLabelValues(tenantID, tier, method, endpoint).Observe(duration.Seconds())
}

// RecordRateLimitHit records a rate limit hit
func (m *Metrics) RecordRateLimitHit(tenantID, tier string) {
	m.apiRateLimitHits.WithLabelValues(tenantID, tier).Inc()
}

// RecordMessage records a message metric
func (m *Metrics) RecordMessage(tenantID, tier, channelType string) {
	m.messagesTotal.WithLabelValues(tenantID, tier, channelType).Inc()
}

// RecordMessageRateLimitHit records a message rate limit hit
func (m *Metrics) RecordMessageRateLimitHit(tenantID, tier string) {
	m.messageRateLimitHits.WithLabelValues(tenantID, tier).Inc()
}

// UpdateResourceUsage updates resource usage metrics
func (m *Metrics) UpdateResourceUsage(tenantID, tier string, usage *ResourceUsage) {
	m.channelsTotal.WithLabelValues(tenantID, tier).Set(float64(usage.ChannelCount))
	m.activeCallsTotal.WithLabelValues(tenantID, tier).Set(float64(usage.ActiveCalls))
	m.websocketConnsTotal.WithLabelValues(tenantID, tier).Set(float64(usage.WebSocketConns))
	m.storageBytes.WithLabelValues(tenantID, tier).Set(float64(usage.StorageBytes))
}

// UpdateQuotaUsage updates quota usage percentage metrics
func (m *Metrics) UpdateQuotaUsage(tenantID, tier string, limits TenantLimits, usage *ResourceUsage) {
	// Channels quota
	if !IsUnlimited(limits.MaxChannels) {
		percent := GetUsagePercentage(usage.ChannelCount, limits.MaxChannels)
		m.quotaUsagePercent.WithLabelValues(tenantID, tier, "channels").Set(percent)
	}

	// Storage quota
	if !IsUnlimitedInt64(limits.StorageQuota) {
		percent := GetUsagePercentageInt64(usage.StorageBytes, limits.StorageQuota)
		m.quotaUsagePercent.WithLabelValues(tenantID, tier, "storage").Set(percent)
	}

	// Active calls quota
	if !IsUnlimited(limits.ConcurrentCalls) {
		percent := GetUsagePercentage(usage.ActiveCalls, limits.ConcurrentCalls)
		m.quotaUsagePercent.WithLabelValues(tenantID, tier, "calls").Set(percent)
	}

	// WebSocket connections quota
	if !IsUnlimited(limits.MaxWebSocketConns) {
		percent := GetUsagePercentage(usage.WebSocketConns, limits.MaxWebSocketConns)
		m.quotaUsagePercent.WithLabelValues(tenantID, tier, "websockets").Set(percent)
	}
}

// RecordQuotaExceeded records a quota exceeded event
func (m *Metrics) RecordQuotaExceeded(tenantID, tier, resourceType string) {
	m.quotaExceeded.WithLabelValues(tenantID, tier, resourceType).Inc()
}

// UpdateQueueDepth updates the priority queue depth metric
func (m *Metrics) UpdateQueueDepth(tenantID, tier, queueType string, depth int) {
	m.queueDepth.WithLabelValues(tenantID, tier, queueType).Set(float64(depth))
}

// RecordQueueProcessing records queue processing time
func (m *Metrics) RecordQueueProcessing(tenantID, tier, queueType string, duration time.Duration) {
	m.queueProcessingTime.WithLabelValues(tenantID, tier, queueType).Observe(duration.Seconds())
}

// MetricsCollector periodically collects and updates tenant metrics
type MetricsCollector struct {
	metrics      *Metrics
	quotaManager *QuotaManager
	repo         *Repository
	interval     time.Duration
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(metrics *Metrics, quotaManager *QuotaManager, repo *Repository, interval time.Duration) *MetricsCollector {
	return &MetricsCollector{
		metrics:      metrics,
		quotaManager: quotaManager,
		repo:         repo,
		interval:     interval,
	}
}

// Start starts the metrics collection loop
func (c *MetricsCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectMetrics(ctx)
		}
	}
}

// collectMetrics collects metrics for all tenants
func (c *MetricsCollector) collectMetrics(ctx context.Context) {
	// Get all tenants
	tenants, err := c.repo.ListTenants(ctx, 1000, 0)
	if err != nil {
		// Log error but continue
		return
	}

	for _, tenant := range tenants {
		// Get resource usage
		usage, err := c.quotaManager.GetResourceUsage(ctx, tenant.TenantID)
		if err != nil {
			continue
		}

		// Update metrics
		c.metrics.UpdateResourceUsage(tenant.TenantID, tenant.Tier, usage)
		c.metrics.UpdateQuotaUsage(tenant.TenantID, tenant.Tier, tenant.Limits, usage)
	}
}

// AuditLogger handles tenant audit logging
type AuditLogger struct {
	repo *Repository
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(repo *Repository) *AuditLogger {
	return &AuditLogger{repo: repo}
}

// LogAction logs a tenant action to the audit log
func (a *AuditLogger) LogAction(ctx context.Context, tenantID, userID, action, resourceType, resourceID string, details map[string]interface{}) error {
	// This would insert into tenant_audit_log table
	// Implementation depends on your database setup
	return nil
}

// GetAuditLog retrieves audit log entries for a tenant
func (a *AuditLogger) GetAuditLog(ctx context.Context, tenantID string, limit, offset int) ([]map[string]interface{}, error) {
	// This would query tenant_audit_log table
	// Implementation depends on your database setup
	return nil, fmt.Errorf("not implemented")
}
