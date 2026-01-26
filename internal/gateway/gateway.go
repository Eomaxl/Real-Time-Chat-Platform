package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/discovery"
	"real-time-chat-system/internal/health"
	"real-time-chat-system/internal/logging"
	"real-time-chat-system/internal/metrics"
	"real-time-chat-system/internal/ratelimit"
	redisclient "real-time-chat-system/internal/redis"
	"real-time-chat-system/internal/tracing"
	"real-time-chat-system/internal/websocket"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Gateway represents the API Gateway service
type Gateway struct {
	config           *config.GatewayConfig
	serviceDiscovery discovery.Discovery
	healthChecker    *health.Checker
	loadBalancer     *discovery.LoadBalancer
	db               *database.PostgresDB
	redis            *redisclient.Client
	wsHub            *websocket.Hub
	rateLimiter      *ratelimit.Limiter
	logger           *logging.Logger
	metrics          *metrics.ServiceMetrics
}

// New creates a new API Gateway instance
func New(cfg *config.GatewayConfig, serviceDiscovery discovery.Discovery, healthChecker *health.Checker, db *database.PostgresDB, redisClient *redisclient.Client) (*Gateway, error) {
	loadBalancer := discovery.NewLoadBalancer(serviceDiscovery)

	// Initialize WebSocket hub with 10 shards for scalability
	wsHub := websocket.NewHub(redisClient, 10)

	// Initialize rate limiter
	rateLimiter := ratelimit.NewLimiter(redisClient.GetClient())

	// Initialize logger
	logger := logging.NewLogger("api-gateway", nil)

	// Initialize metrics
	serviceMetrics := metrics.NewServiceMetrics("api-gateway")

	gateway := &Gateway{
		config:           cfg,
		serviceDiscovery: serviceDiscovery,
		healthChecker:    healthChecker,
		loadBalancer:     loadBalancer,
		db:               db,
		redis:            redisClient,
		wsHub:            wsHub,
		rateLimiter:      rateLimiter,
		logger:           logger,
		metrics:          serviceMetrics,
	}

	// Add health checks
	healthChecker.AddCheck("service-discovery", health.ServiceDiscoveryHealthCheck(serviceDiscovery))
	healthChecker.AddCheck("database", health.DatabaseHealthCheck(db))
	healthChecker.AddCheck("redis", health.RedisHealthCheck(redisClient))

	// Start WebSocket hub
	wsHub.Start()

	return gateway, nil
}

// Router returns the HTTP router for the gateway
func (g *Gateway) Router() http.Handler {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Add tracing middleware first
	router.Use(tracing.GinMiddleware("api-gateway"))

	// Add correlation ID middleware
	router.Use(g.correlationMiddleware())

	// Add structured logging middleware
	router.Use(g.loggingMiddleware())

	// Add metrics middleware
	router.Use(metrics.RecoveryMiddleware(g.metrics, "api-gateway"))
	router.Use(metrics.HTTPMetricsMiddleware(g.metrics, "api-gateway"))

	// Health endpoints
	router.GET("/health", gin.WrapF(g.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(g.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API routes
	v1 := router.Group("/v1")
	{
		// Apply rate limiting middleware
		v1.Use(g.rateLimitMiddleware())

		// Authentication middleware would be added here
		v1.Use(g.authMiddleware())

		// Chat endpoints
		channels := v1.Group("/channels")
		{
			channels.POST("/:id/messages", g.proxyToService("chat-service"))
			channels.GET("/:id/messages", g.proxyToService("chat-service"))
		}

		// Call endpoints
		calls := v1.Group("/calls")
		{
			calls.POST("", g.proxyToService("call-service"))
			calls.POST("/:id/join", g.proxyToService("call-service"))
		}

		// Presence endpoints
		presence := v1.Group("/presence")
		{
			presence.POST("/heartbeat", g.proxyToService("presence-service"))
		}

		// WebSocket endpoint
		v1.GET("/ws", g.handleWebSocket)
	}

	return router
}

// correlationMiddleware generates and propagates correlation IDs
func (g *Gateway) correlationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if correlation ID already exists in header
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			// Generate new correlation ID
			correlationID = logging.GenerateCorrelationID()
		}

		// Store in context
		ctx := logging.WithCorrelationID(c.Request.Context(), correlationID)
		c.Request = c.Request.WithContext(ctx)

		// Store in Gin context for easy access
		c.Set("correlation_id", correlationID)

		// Add to response headers
		c.Header("X-Correlation-ID", correlationID)

		c.Next()
	}
}

// loggingMiddleware provides structured logging for all requests
func (g *Gateway) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Get correlation ID from context
		correlationID := logging.GetCorrelationID(c.Request.Context())

		// Log request start
		g.logger.WithContext(c.Request.Context()).
			WithField("method", method).
			WithField("path", path).
			WithField("client_ip", c.ClientIP()).
			Info("Request started")

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)
		statusCode := c.Writer.Status()

		// Log request completion
		logEntry := g.logger.WithContext(c.Request.Context()).
			WithField("method", method).
			WithField("path", path).
			WithField("status", statusCode).
			WithField("latency_ms", latency.Milliseconds()).
			WithField("client_ip", c.ClientIP())

		// Log at appropriate level based on status code
		if statusCode >= 500 {
			logEntry.Error("Request completed with server error", nil)
		} else if statusCode >= 400 {
			logEntry.Warn("Request completed with client error")
		} else {
			logEntry.Info("Request completed successfully")
		}

		// Log errors if any
		if len(c.Errors) > 0 {
			for _, err := range c.Errors {
				g.logger.WithContext(c.Request.Context()).
					WithField("method", method).
					WithField("path", path).
					WithField("correlation_id", correlationID).
					Error("Request error", err.Err)
			}
		}
	}
}

// rateLimitMiddleware provides distributed rate limiting
func (g *Gateway) rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Get user ID from context (if authenticated)
		userID, userExists := c.Get("user_id")

		// Get client IP
		clientIP := c.ClientIP()

		// Rate limit configuration
		// Per-user: 10,000 requests per minute with 1,000 burst
		// Per-IP: 1,000 requests per minute with 100 burst
		var rateLimitKey string
		var cfg ratelimit.Config

		if userExists {
			// Per-user rate limiting (more generous)
			rateLimitKey = fmt.Sprintf("user:%s", userID)
			cfg = ratelimit.Config{
				MaxRequests: 10000,
				Window:      time.Minute,
				BurstSize:   1000,
			}
		} else {
			// Per-IP rate limiting (more restrictive for unauthenticated)
			rateLimitKey = fmt.Sprintf("ip:%s", clientIP)
			cfg = ratelimit.Config{
				MaxRequests: 1000,
				Window:      time.Minute,
				BurstSize:   100,
			}
		}

		// Check rate limit
		result, err := g.rateLimiter.Allow(ctx, rateLimitKey, cfg)
		if err != nil {
			// Log error but don't block request on rate limiter failure
			c.Next()
			return
		}

		// Set rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt.Unix()))

		if !result.Allowed {
			c.Header("Retry-After", fmt.Sprintf("%d", int(result.RetryAfter.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many requests",
				"code":        "RATE_LIMIT_EXCEEDED",
				"message":     "Rate limit exceeded. Please try again later.",
				"retry_after": int(result.RetryAfter.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// authMiddleware provides JWT authentication
func (g *Gateway) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// For now, just pass through
		// In a real implementation, this would validate JWT tokens

		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Log security event for missing authentication
			g.logger.WithContext(c.Request.Context()).
				WithField("path", c.Request.URL.Path).
				WithField("method", c.Request.Method).
				WithField("client_ip", c.ClientIP()).
				Security("Authentication failed: missing authorization header")

			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Unauthorized",
				"code":    "MISSING_AUTH_HEADER",
				"message": "Authorization header is required",
			})
			c.Abort()
			return
		}

		// TODO: Validate JWT token here
		// For now, extract user_id from header for testing
		userID := c.GetHeader("X-User-ID")
		if userID != "" {
			// Store user context
			ctx := logging.WithUserID(c.Request.Context(), userID)
			c.Request = c.Request.WithContext(ctx)
			c.Set("user_id", userID)
		}

		tenantID := c.GetHeader("X-Tenant-ID")
		if tenantID != "" {
			// Store tenant context
			ctx := logging.WithTenantID(c.Request.Context(), tenantID)
			c.Request = c.Request.WithContext(ctx)
			c.Set("tenant_id", tenantID)
		}

		c.Next()
	}
}

// proxyToService creates a handler that proxies requests to a service
func (g *Gateway) proxyToService(serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get a healthy service instance using load balancer
		instance, err := g.loadBalancer.GetInstance(serviceName)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "Service unavailable",
				"code":    "SERVICE_UNAVAILABLE",
				"message": fmt.Sprintf("No healthy instances found for %s", serviceName),
			})
			return
		}

		// Build target URL
		targetURL := fmt.Sprintf("http://%s:%s%s", instance.Address, instance.Port, c.Request.URL.Path)
		if c.Request.URL.RawQuery != "" {
			targetURL += "?" + c.Request.URL.RawQuery
		}

		// Parse target URL
		target, err := url.Parse(targetURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Internal server error",
				"code":    "INTERNAL_ERROR",
				"message": "Failed to parse target URL",
			})
			return
		}

		// Read request body
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Bad request",
					"code":    "BAD_REQUEST",
					"message": "Failed to read request body",
				})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// Create proxy request
		proxyReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), bytes.NewBuffer(bodyBytes))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Internal server error",
				"code":    "INTERNAL_ERROR",
				"message": "Failed to create proxy request",
			})
			return
		}

		// Copy headers from original request
		for key, values := range c.Request.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		// Add user context headers (from JWT token)
		if userID, exists := c.Get("user_id"); exists {
			proxyReq.Header.Set("X-User-ID", userID.(string))
		}
		if tenantID, exists := c.Get("tenant_id"); exists {
			proxyReq.Header.Set("X-Tenant-ID", tenantID.(string))
		}

		// Add correlation ID for distributed tracing
		if correlationID, exists := c.Get("correlation_id"); exists {
			proxyReq.Header.Set("X-Correlation-ID", correlationID.(string))
		}

		// Execute proxy request with timeout
		client := &http.Client{
			Timeout: 30 * time.Second,
		}
		resp, err := client.Do(proxyReq)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":   "Bad gateway",
				"code":    "BAD_GATEWAY",
				"message": fmt.Sprintf("Failed to reach %s service", serviceName),
			})
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}

		// Copy response body
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Internal server error",
				"code":    "INTERNAL_ERROR",
				"message": "Failed to read response from service",
			})
			return
		}

		// Return response with original status code
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
	}
}

// handleWebSocket handles WebSocket connections
func (g *Gateway) handleWebSocket(c *gin.Context) {
	// Extract user ID from JWT token (placeholder for now)
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User ID required",
			"code":  "UNAUTHORIZED",
		})
		return
	}

	// Upgrade to WebSocket
	g.wsHub.HandleWebSocket(c.Writer, c.Request, userID)
}

// Stop gracefully stops the gateway and its components
func (g *Gateway) Stop() {
	if g.wsHub != nil {
		g.wsHub.Stop()
	}
}

// GetWebSocketHub returns the WebSocket hub (for testing/monitoring)
func (g *Gateway) GetWebSocketHub() *websocket.Hub {
	return g.wsHub
}
