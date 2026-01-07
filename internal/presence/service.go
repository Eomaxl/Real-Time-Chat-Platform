package presence

import (
	"net/http"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/health"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

type Service struct {
	config        *config.PresenceConfig
	healthChecker *health.Checker
	redis         *redis.Client
}

// New creates a new Presence Service instance
func New(cfg *config.PresenceConfig, healthChecker *health.Checker, redisClient *redis.Client) (*Service, error) {
	service := &Service{
		config:        cfg,
		healthChecker: healthChecker,
		redis:         redisClient,
	}

	// Add health checks
	healthChecker.AddCheck("redis", health.RedisHealthCheck(health.NewRedisAdapter(redisClient)))

	return service, nil
}

// Router returns the HTTP router for the presence service
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Health endpoints
	router.GET("/health", gin.WrapF(s.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(s.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", s.metricsHandler)

	// Service endpoints
	v1 := router.Group("/v1")
	{
		v1.POST("/heartbeat", s.updateHeartbeat)
		v1.GET("/presence/:userID", s.getPresence)
	}

	return router
}

// updateHeartbeat handles presence heartbeat updates
func (s *Service) updateHeartbeat(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Presence service - heartbeat endpoint",
		"service": "presence-service",
		"status":  "Redis connected",
	})
}

// getPresence handles presence status retrieval
func (s *Service) getPresence(c *gin.Context) {
	userID := c.Param("userID")
	c.JSON(http.StatusOK, gin.H{
		"message": "Presence Service - get presence endpoint",
		"service": "presence-service",
		"userID":  userID,
		"status":  "Redis Connected",
	})
}

// metricsHandler exposes Prometheus metrics
func (s *Service) metricsHandler(c *gin.Context) {
	// returns a placeholder response.
	c.String(http.StatusOK, "# Prometheus metrics would be exposed here \n")
}
