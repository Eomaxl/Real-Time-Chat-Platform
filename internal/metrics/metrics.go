package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ServiceMetrics holds all Prometheus metrics for the platform
type ServiceMetrics struct {
	// HTTP/API metrics
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	HTTPRequestsInFlight *prometheus.GaugeVec
	HTTPResponseSize     *prometheus.HistogramVec

	// Business metrics - Active users
	ActiveUsersTotal *prometheus.GaugeVec
	OnlineUsersTotal *prometheus.GaugeVec

	// Business metrics - Messages
	MessagesSentTotal     *prometheus.CounterVec
	MessagesReceivedTotal *prometheus.CounterVec
	MessageRatePerSecond  *prometheus.GaugeVec
	MessageLatency        *prometheus.HistogramVec

	// Business metrics - Calls
	ActiveCallsTotal      *prometheus.GaugeVec
	CallsStartedTotal     *prometheus.CounterVec
	CallsEndedTotal       *prometheus.CounterVec
	CallDuration          *prometheus.HistogramVec
	CallParticipantsTotal *prometheus.GaugeVec

	// WebSocket metrics
	WebSocketConnectionsTotal   *prometheus.GaugeVec
	WebSocketMessagesTotal      *prometheus.CounterVec
	WebSocketConnectionDuration *prometheus.HistogramVec
	WebSocketReconnectsTotal    *prometheus.CounterVec

	// Database metrics
	DBQueriesTotal      *prometheus.CounterVec
	DBQueryDuration     *prometheus.HistogramVec
	DBConnectionsActive *prometheus.GaugeVec
	DBConnectionsIdle   *prometheus.GaugeVec
	DBConnectionErrors  *prometheus.CounterVec

	// Redis metrics
	RedisCommandsTotal     *prometheus.CounterVec
	RedisCommandDuration   *prometheus.HistogramVec
	RedisConnectionsActive *prometheus.GaugeVec
	RedisErrors            *prometheus.CounterVec

	// Presence metrics
	PresenceHeartbeatsTotal *prometheus.CounterVec
	PresenceUpdatesTotal    *prometheus.CounterVec
	PresenceCheckDuration   *prometheus.HistogramVec

	// Authentication metrics
	AuthAttemptsTotal     *prometheus.CounterVec
	AuthSuccessTotal      *prometheus.CounterVec
	AuthFailuresTotal     *prometheus.CounterVec
	TokenValidationsTotal *prometheus.CounterVec

	// Rate limiting metrics
	RateLimitHitsTotal    *prometheus.CounterVec
	RateLimitAllowedTotal *prometheus.CounterVec

	// Error metrics
	ErrorsTotal *prometheus.CounterVec
	PanicsTotal *prometheus.CounterVec

	// SLA metrics
	SLAViolationsTotal   *prometheus.CounterVec
	SLACompliancePercent *prometheus.GaugeVec
}

// NewServiceMetrics creates and registers all Prometheus metrics
func NewServiceMetrics(serviceName string) *ServiceMetrics {
	return &ServiceMetrics{
		// HTTP/API metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"service", "method", "endpoint", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"service", "method", "endpoint"},
		),
		HTTPRequestsInFlight: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Current number of HTTP requests being processed",
			},
			[]string{"service"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"service", "endpoint"},
		),

		// Business metrics - Active users
		ActiveUsersTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "active_users_total",
				Help: "Total number of active users",
			},
			[]string{"service", "time_window"},
		),
		OnlineUsersTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "online_users_total",
				Help: "Total number of currently online users",
			},
			[]string{"service"},
		),

		// Business metrics - Messages
		MessagesSentTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "messages_sent_total",
				Help: "Total number of messages sent",
			},
			[]string{"service", "channel_type"},
		),
		MessagesReceivedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "messages_received_total",
				Help: "Total number of messages received",
			},
			[]string{"service", "channel_type"},
		),
		MessageRatePerSecond: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "message_rate_per_second",
				Help: "Current message rate per second",
			},
			[]string{"service"},
		),
		MessageLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "message_latency_seconds",
				Help:    "Message end-to-end latency in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .2, .5, 1},
			},
			[]string{"service", "channel_type"},
		),

		// Business metrics - Calls
		ActiveCallsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "active_calls_total",
				Help: "Total number of active calls",
			},
			[]string{"service", "call_type"},
		),
		CallsStartedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "calls_started_total",
				Help: "Total number of calls started",
			},
			[]string{"service", "call_type"},
		),
		CallsEndedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "calls_ended_total",
				Help: "Total number of calls ended",
			},
			[]string{"service", "call_type", "reason"},
		),
		CallDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "call_duration_seconds",
				Help:    "Call duration in seconds",
				Buckets: []float64{10, 30, 60, 120, 300, 600, 1800, 3600, 7200},
			},
			[]string{"service", "call_type"},
		),
		CallParticipantsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "call_participants_total",
				Help: "Total number of participants in active calls",
			},
			[]string{"service", "call_id"},
		),

		// WebSocket metrics
		WebSocketConnectionsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "websocket_connections_total",
				Help: "Total number of active WebSocket connections",
			},
			[]string{"service"},
		),
		WebSocketMessagesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "websocket_messages_total",
				Help: "Total number of WebSocket messages",
			},
			[]string{"service", "direction", "message_type"},
		),
		WebSocketConnectionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "websocket_connection_duration_seconds",
				Help:    "WebSocket connection duration in seconds",
				Buckets: []float64{10, 60, 300, 600, 1800, 3600, 7200, 14400, 28800},
			},
			[]string{"service"},
		),
		WebSocketReconnectsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "websocket_reconnects_total",
				Help: "Total number of WebSocket reconnections",
			},
			[]string{"service", "reason"},
		),

		// Database metrics
		DBQueriesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"service", "operation", "table"},
		),
		DBQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "db_query_duration_seconds",
				Help:    "Database query duration in seconds",
				Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"service", "operation", "table"},
		),
		DBConnectionsActive: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "db_connections_active",
				Help: "Number of active database connections",
			},
			[]string{"service", "database"},
		),
		DBConnectionsIdle: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "db_connections_idle",
				Help: "Number of idle database connections",
			},
			[]string{"service", "database"},
		),
		DBConnectionErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_connection_errors_total",
				Help: "Total number of database connection errors",
			},
			[]string{"service", "database", "error_type"},
		),

		// Redis metrics
		RedisCommandsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_commands_total",
				Help: "Total number of Redis commands",
			},
			[]string{"service", "command"},
		),
		RedisCommandDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "redis_command_duration_seconds",
				Help:    "Redis command duration in seconds",
				Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1},
			},
			[]string{"service", "command"},
		),
		RedisConnectionsActive: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "redis_connections_active",
				Help: "Number of active Redis connections",
			},
			[]string{"service"},
		),
		RedisErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_errors_total",
				Help: "Total number of Redis errors",
			},
			[]string{"service", "error_type"},
		),

		// Presence metrics
		PresenceHeartbeatsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "presence_heartbeats_total",
				Help: "Total number of presence heartbeats received",
			},
			[]string{"service"},
		),
		PresenceUpdatesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "presence_updates_total",
				Help: "Total number of presence status updates",
			},
			[]string{"service", "status"},
		),
		PresenceCheckDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "presence_check_duration_seconds",
				Help:    "Presence check duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1},
			},
			[]string{"service"},
		),

		// Authentication metrics
		AuthAttemptsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_attempts_total",
				Help: "Total number of authentication attempts",
			},
			[]string{"service", "method"},
		),
		AuthSuccessTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_success_total",
				Help: "Total number of successful authentications",
			},
			[]string{"service", "method"},
		),
		AuthFailuresTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "auth_failures_total",
				Help: "Total number of failed authentications",
			},
			[]string{"service", "method", "reason"},
		),
		TokenValidationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "token_validations_total",
				Help: "Total number of token validations",
			},
			[]string{"service", "result"},
		),

		// Rate limiting metrics
		RateLimitHitsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limit_hits_total",
				Help: "Total number of rate limit hits",
			},
			[]string{"service", "limit_type", "user_id"},
		),
		RateLimitAllowedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limit_allowed_total",
				Help: "Total number of requests allowed by rate limiter",
			},
			[]string{"service", "limit_type"},
		),

		// Error metrics
		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "errors_total",
				Help: "Total number of errors",
			},
			[]string{"service", "error_type", "severity"},
		),
		PanicsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "panics_total",
				Help: "Total number of panics recovered",
			},
			[]string{"service", "location"},
		),

		// SLA metrics
		SLAViolationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "sla_violations_total",
				Help: "Total number of SLA violations",
			},
			[]string{"service", "sla_type", "threshold"},
		),
		SLACompliancePercent: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "sla_compliance_percent",
				Help: "SLA compliance percentage",
			},
			[]string{"service", "sla_type"},
		),
	}
}

// RecordHTTPRequest records an HTTP request with timing
func (m *ServiceMetrics) RecordHTTPRequest(service, method, endpoint, status string, duration time.Duration) {
	m.HTTPRequestsTotal.WithLabelValues(service, method, endpoint, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(service, method, endpoint).Observe(duration.Seconds())
}

// RecordMessageSent records a message being sent
func (m *ServiceMetrics) RecordMessageSent(service, channelType string, latency time.Duration) {
	m.MessagesSentTotal.WithLabelValues(service, channelType).Inc()
	m.MessageLatency.WithLabelValues(service, channelType).Observe(latency.Seconds())
}

// RecordCallStarted records a call being started
func (m *ServiceMetrics) RecordCallStarted(service, callType string) {
	m.CallsStartedTotal.WithLabelValues(service, callType).Inc()
	m.ActiveCallsTotal.WithLabelValues(service, callType).Inc()
}

// RecordCallEnded records a call ending
func (m *ServiceMetrics) RecordCallEnded(service, callType, reason string, duration time.Duration) {
	m.CallsEndedTotal.WithLabelValues(service, callType, reason).Inc()
	m.ActiveCallsTotal.WithLabelValues(service, callType).Dec()
	m.CallDuration.WithLabelValues(service, callType).Observe(duration.Seconds())
}

// RecordWebSocketConnection records a WebSocket connection
func (m *ServiceMetrics) RecordWebSocketConnection(service string, connected bool) {
	if connected {
		m.WebSocketConnectionsTotal.WithLabelValues(service).Inc()
	} else {
		m.WebSocketConnectionsTotal.WithLabelValues(service).Dec()
	}
}

// RecordDBQuery records a database query
func (m *ServiceMetrics) RecordDBQuery(service, operation, table string, duration time.Duration) {
	m.DBQueriesTotal.WithLabelValues(service, operation, table).Inc()
	m.DBQueryDuration.WithLabelValues(service, operation, table).Observe(duration.Seconds())
}

// RecordRedisCommand records a Redis command
func (m *ServiceMetrics) RecordRedisCommand(service, command string, duration time.Duration) {
	m.RedisCommandsTotal.WithLabelValues(service, command).Inc()
	m.RedisCommandDuration.WithLabelValues(service, command).Observe(duration.Seconds())
}

// RecordAuthAttempt records an authentication attempt
func (m *ServiceMetrics) RecordAuthAttempt(service, method string, success bool, reason string) {
	m.AuthAttemptsTotal.WithLabelValues(service, method).Inc()
	if success {
		m.AuthSuccessTotal.WithLabelValues(service, method).Inc()
	} else {
		m.AuthFailuresTotal.WithLabelValues(service, method, reason).Inc()
	}
}

// RecordSLAViolation records an SLA violation
func (m *ServiceMetrics) RecordSLAViolation(service, slaType, threshold string) {
	m.SLAViolationsTotal.WithLabelValues(service, slaType, threshold).Inc()
}

// UpdateSLACompliance updates the SLA compliance percentage
func (m *ServiceMetrics) UpdateSLACompliance(service, slaType string, percent float64) {
	m.SLACompliancePercent.WithLabelValues(service, slaType).Set(percent)
}
