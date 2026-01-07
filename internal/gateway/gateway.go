package gateway

import (
	"net/http"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/discovery"
	"real-time-chat-system/internal/health"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// Gateway represents the API Gateway service
type Gateway struct {
	config           *config.GatewayConfig
	serviceDiscovery discovery.Discovery
	healthChecker    *health.Checker
	loadBalancer     *discovery.LoadBalancer
	db               *database.PostgresDB
	redis            *redis.Client
}

// New create a new API Gateway instance
func New(cfg *config.GatewayConfig, serviceDiscovery discovery.Discovery, healthChecker *health.Checker, db *database.PostgresDB, redisClient *redis.Client) (*Gateway, error) {
	loadBalancer := discovery.NewLoadBalancer(serviceDiscovery)

	gateway := &Gateway{
		config:           cfg,
		serviceDiscovery: serviceDiscovery,
		healthChecker:    healthChecker,
		loadBalancer:     loadBalancer,
		db:               db,
		redis:            redisClient,
	}

	// Add health checks
	healthChecker.AddCheck("service-discovery", health.ServiceDiscoveryHealthCheck(serviceDiscovery))
	healthChecker.AddCheck("database", health.DatabaseHealthCheck(db))
	healthChecker.AddCheck("redis", health.RedisHealthCheck(health.NewRedisAdapter(redisClient)))

	return gateway, nil
}

// Router returns the HTTP router for the gateway
func (g *Gateway) Router() http.Handler {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Health endpoints
	router.GET("/health", gin.WrapF(g.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(g.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Promemtheus
	router.GET("/metrics", g.metricsHandler)

	// API Routes
	v1 := router.Group("/v1")
	{
		// Authentication middleware would be added here
		v1.Use(g.authMiddleware())

		// Chat endpoints
		channels := v1.Group("/channels")
		{
			channels.POST("/:id/messages", g.proxyToService("chat-service"))
			channels.GET("/:id/message", g.proxyToService("chat-service"))
		}

		// Call endpoints
		calls := v1.Group("/calls")
		{
			calls.POST("", g.proxyToService("call-service"))
			calls.POST("/:id/join", g.proxyToService("call-service"))
		}

		// Presence endpints
		presence := v1.Group("/presence")
		{
			presence.POST("/heartbeat", g.proxyToService("presence-service"))
		}

		// Websocket endpoint
		v1.GET("/ws", g.handleWebSocket)
	}
	return router
}

// authMiddleware provides JWT authentication
func (g *Gateway) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// In a real implementation, this would validate JWT tokens
		c.Next()
	}
}

// proxyToService creates a handler that proxies request to a service
func (g *Gateway) proxyToService(serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		instance, err := g.loadBalancer.GetInstance(serviceName)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Service unavailable",
				"code":  "SERVICE_UNAVAILABLE",
			})
			return
		}

		// PLaceholder for now. Later on this would proxy the request
		c.JSON(http.StatusOK, gin.H{
			"message":  "Request would be proxied to " + serviceName,
			"instance": instance.Address + ":" + instance.Port,
		})
	}
}

// handleWebSocket handles WebSocket connections
func (g *Gateway) handleWebSocket(c *gin.Context) {
	// For now, return a placeholder response
	// In a real implementation, this would upgrade to WebSocket
	c.JSON(http.StatusOK, gin.H{
		"message": "WebSocket endpoint - would upgrade connection",
	})
}

// metricsHandler exposes Prometheus metrics
func (g *Gateway) metricsHandler(c *gin.Context) {
	// For now, return a placeholder response
	// In a real implementation, this would expose Prometheus metrics
	c.String(http.StatusOK, "# Prometheus metrics would be exposed here\n")
}
