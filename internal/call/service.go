package call

import (
	"net/http"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/health"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Service represents the call service
type Service struct {
	config        *config.CallConfig
	healthChecker *health.Checker
	db            *database.PostgresDB
	redis         *redis.Client
}

// New create a new call service instance
func New(cfg *config.CallConfig, healthChecker *health.Checker, db *database.PostgresDB, redisClient *redis.Client) (*Service, error) {
	service := &Service{
		config:        cfg,
		healthChecker: healthChecker,
		db:            db,
		redis:         redisClient,
	}

	// Add health checks
	healthChecker.AddCheck("database", health.DatabaseHealthCheck(db))
	healthChecker.AddCheck("redis", health.RedisHealthCheck(health.NewRedisAdapter(redisClient)))

	return service, nil
}

// Router returns HTTP router for the call service
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	//Health endpoints
	router.GET("/health", gin.WrapF(s.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(s.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", s.metricsHandler)

	// Service endpoints
	v1 := router.Group("/v1")
	{
		v1.POST("/calls", s.createCall)
		v1.POST("/calls/:id/join", s.joinCall)
		v1.POST("/calls/:id/signaling", s.handleSignaling)
	}

	return router
}

// createCall handles call creation
func (s *Service) createCall(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Call service - create call endpoints",
		"service": "call-service",
		"status":  "Database and Redis connected",
	})
}

// joinCall handles joining a call
func (s *Service) joinCall(c *gin.Context) {
	callID := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"message": "Call service - join call endpoint",
		"service": "call-service",
		"callID":  callID,
		"status":  "Database and Redis connected",
	})
}

// handleSignaling handles WebRTC signaling messages
func (s *Service) handleSignaling(c *gin.Context) {
	callID := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"message": "Call service - signaling endpoint",
		"service": "call-service",
		"callID":  callID,
		"status":  "Database and Redis connected",
	})
}

// metricsHandler exposes Prometheus metrics
func (s *Service) metricsHandler(c *gin.Context) {
	// For now, return a placeholder response
	// In a real implementation, this would expose Prometheus metrics
	c.String(http.StatusOK, "# Prometheus metrics would be exposed here\n")
}
