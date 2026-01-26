package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"real-time-chat-system/internal/cache"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/events"
	"real-time-chat-system/internal/health"
	"real-time-chat-system/internal/metrics"
	redisclient "real-time-chat-system/internal/redis"
	"real-time-chat-system/internal/tracing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Service represents the Presence Service
type Service struct {
	config        *config.PresenceConfig
	healthChecker *health.Checker
	redis         *redisclient.Client
	publisher     *events.Publisher
	cache         *cache.LRUCache
	metrics       *metrics.ServiceMetrics

	// Batch processing
	heartbeatQueue chan HeartbeatRequest
	batchMutex     sync.Mutex
	batchBuffer    []HeartbeatRequest
	stopChan       chan struct{}
	wg             sync.WaitGroup

	// Cleanup tracking
	cleanupTicker *time.Ticker
	lastCleanup   time.Time
	cleanupMutex  sync.RWMutex
}

// HeartbeatRequest represents a heartbeat update request
type HeartbeatRequest struct {
	UserID    string   `json:"user_id" binding:"required"`
	Status    string   `json:"status"`
	Channels  []string `json:"channels"`
	Timestamp time.Time
}

// PresenceStatus represents a user's presence status
type PresenceStatus struct {
	UserID    string    `json:"user_id"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChannelPresence represents presence information for a channel
type ChannelPresence struct {
	ChannelID string           `json:"channel_id"`
	Users     []PresenceStatus `json:"users"`
}

// New creates a new Presence Service instance
func New(cfg *config.PresenceConfig, healthChecker *health.Checker, redisClient *redisclient.Client) (*Service, error) {
	// Initialize metrics
	serviceMetrics := metrics.NewServiceMetrics("presence-service")

	service := &Service{
		config:         cfg,
		healthChecker:  healthChecker,
		redis:          redisClient,
		publisher:      events.NewPublisher(redisClient),
		cache:          cache.NewLRUCache(50000), // Cache 50K presence statuses in memory
		metrics:        serviceMetrics,
		heartbeatQueue: make(chan HeartbeatRequest, cfg.BatchSize*10),
		batchBuffer:    make([]HeartbeatRequest, 0, cfg.BatchSize),
		stopChan:       make(chan struct{}),
		cleanupTicker:  time.NewTicker(10 * time.Second), // Cleanup every 10 seconds
		lastCleanup:    time.Now(),
	}

	// Add health checks
	healthChecker.AddCheck("redis", health.RedisHealthCheck(redisClient))

	// Start batch processor
	service.wg.Add(1)
	go service.processBatchHeartbeats()

	// Start cleanup processor
	service.wg.Add(1)
	go service.processExpiredPresenceCleanup()

	return service, nil
}

// Stop gracefully stops the presence service
func (s *Service) Stop() {
	close(s.stopChan)
	s.cleanupTicker.Stop()
	s.wg.Wait()
	close(s.heartbeatQueue)
}

// Router returns the HTTP router for the presence service
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(tracing.GinMiddleware("presence-service"))
	router.Use(metrics.RecoveryMiddleware(s.metrics, "presence-service"))
	router.Use(metrics.HTTPMetricsMiddleware(s.metrics, "presence-service"))

	// Health endpoints
	router.GET("/health", gin.WrapF(s.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(s.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Service endpoints
	v1 := router.Group("/v1")
	{
		v1.POST("/heartbeat", s.updateHeartbeat)
		v1.GET("/presence/:userID", s.getPresence)
		v1.GET("/channels/:channelID/presence", s.getChannelPresenceHandler)
		v1.POST("/channels/presence/bulk", s.getAggregatedChannelPresenceHandler)
		v1.GET("/stats", s.getStatsHandler)
		v1.POST("/sync", s.syncHandler)
		v1.DELETE("/users/:userID/presence", s.removeUserPresenceHandler)
	}

	return router
}

// getChannelPresenceHandler handles channel presence retrieval
func (s *Service) getChannelPresenceHandler(c *gin.Context) {
	channelID := c.Param("channelID")

	ctx := c.Request.Context()
	presence, err := s.GetChannelPresence(ctx, channelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get channel presence",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, presence)
}

// getAggregatedChannelPresenceHandler handles bulk channel presence retrieval
func (s *Service) getAggregatedChannelPresenceHandler(c *gin.Context) {
	var req struct {
		ChannelIDs []string `json:"channel_ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": err.Error(),
		})
		return
	}

	ctx := c.Request.Context()
	presence, err := s.GetAggregatedChannelPresence(ctx, req.ChannelIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get aggregated presence",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, presence)
}

// getStatsHandler handles presence statistics retrieval
func (s *Service) getStatsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	stats, err := s.GetPresenceStats(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get presence stats",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// syncHandler handles manual presence synchronization
func (s *Service) syncHandler(c *gin.Context) {
	ctx := c.Request.Context()
	if err := s.SyncPresenceAcrossShards(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to sync presence",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Presence synchronized successfully",
	})
}

// removeUserPresenceHandler handles user presence removal
func (s *Service) removeUserPresenceHandler(c *gin.Context) {
	userID := c.Param("userID")

	ctx := c.Request.Context()
	if err := s.RemoveUserFromAllChannels(ctx, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to remove user presence",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "User presence removed successfully",
		"user_id": userID,
	})
}

// updateHeartbeat handles presence heartbeat updates
func (s *Service) updateHeartbeat(c *gin.Context) {
	var req HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"message": err.Error(),
		})
		return
	}

	// Set default status if not provided
	if req.Status == "" {
		req.Status = "online"
	}

	// Set timestamp
	req.Timestamp = time.Now()

	// Record heartbeat metric
	s.metrics.PresenceHeartbeatsTotal.WithLabelValues("presence-service").Inc()

	// Queue for batch processing
	select {
	case s.heartbeatQueue <- req:
		c.JSON(http.StatusOK, gin.H{
			"message": "Heartbeat received",
			"user_id": req.UserID,
			"status":  req.Status,
		})
	default:
		// Queue is full, process immediately
		if err := s.processHeartbeat(c.Request.Context(), req); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to process heartbeat",
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "Heartbeat processed",
			"user_id": req.UserID,
			"status":  req.Status,
		})
	}
}

// getPresence handles presence status retrieval
func (s *Service) getPresence(c *gin.Context) {
	userID := c.Param("userID")
	ctx := c.Request.Context()

	// Try L1 cache first
	if cached, ok := s.cache.Get(userID); ok {
		if presenceStatus, ok := cached.(PresenceStatus); ok {
			c.JSON(http.StatusOK, presenceStatus)
			return
		}
	}

	// Get from Redis
	status, err := s.redis.GetPresence(ctx, userID)
	if err != nil {
		// User is offline or not found
		c.JSON(http.StatusOK, gin.H{
			"user_id": userID,
			"status":  "offline",
		})
		return
	}

	// Parse the stored presence data
	var presenceStatus PresenceStatus
	if err := json.Unmarshal([]byte(status), &presenceStatus); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to parse presence data",
			"message": err.Error(),
		})
		return
	}

	// Cache in L1
	s.cache.Set(userID, presenceStatus, 10*time.Second)

	c.JSON(http.StatusOK, presenceStatus)
}

// processBatchHeartbeats processes heartbeats in batches
func (s *Service) processBatchHeartbeats() {
	defer s.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Process batches every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			// Process remaining heartbeats before stopping
			s.flushBatch()
			return

		case <-ticker.C:
			// Flush batch on timer
			s.flushBatch()

		case heartbeat := <-s.heartbeatQueue:
			// Add to batch buffer
			s.batchMutex.Lock()
			s.batchBuffer = append(s.batchBuffer, heartbeat)
			shouldFlush := len(s.batchBuffer) >= s.config.BatchSize
			s.batchMutex.Unlock()

			// Flush if batch is full
			if shouldFlush {
				s.flushBatch()
			}
		}
	}
}

// flushBatch processes all heartbeats in the current batch
func (s *Service) flushBatch() {
	s.batchMutex.Lock()
	if len(s.batchBuffer) == 0 {
		s.batchMutex.Unlock()
		return
	}

	// Copy batch and clear buffer
	batch := make([]HeartbeatRequest, len(s.batchBuffer))
	copy(batch, s.batchBuffer)
	s.batchBuffer = s.batchBuffer[:0]
	s.batchMutex.Unlock()

	// Process batch
	ctx := context.Background()
	for _, heartbeat := range batch {
		if err := s.processHeartbeat(ctx, heartbeat); err != nil {
			log.Printf("Failed to process heartbeat for user %s: %v", heartbeat.UserID, err)
		}
	}
}

// processHeartbeat processes a single heartbeat request
func (s *Service) processHeartbeat(ctx context.Context, req HeartbeatRequest) error {
	// Get previous status to detect changes
	previousStatus, err := s.redis.GetPresence(ctx, req.UserID)
	var prevPresence PresenceStatus
	statusChanged := false

	if err != nil {
		// User was offline or not found
		statusChanged = true
	} else {
		if err := json.Unmarshal([]byte(previousStatus), &prevPresence); err == nil {
			statusChanged = prevPresence.Status != req.Status
		}
	}

	// Create presence status
	presence := PresenceStatus{
		UserID:    req.UserID,
		Status:    req.Status,
		LastSeen:  req.Timestamp,
		UpdatedAt: req.Timestamp,
	}

	// Serialize presence data
	presenceData, err := json.Marshal(presence)
	if err != nil {
		return fmt.Errorf("failed to marshal presence data: %w", err)
	}

	// Store in Redis with TTL
	ttl := s.config.GetTTL()
	if err := s.redis.SetPresence(ctx, req.UserID, string(presenceData), ttl); err != nil {
		return fmt.Errorf("failed to set presence: %w", err)
	}

	// Update L1 cache
	s.cache.Set(req.UserID, presence, 10*time.Second)

	// Update channel presence for each channel
	for _, channelID := range req.Channels {
		if err := s.redis.AddToChannelPresence(ctx, channelID, req.UserID); err != nil {
			log.Printf("Failed to add user %s to channel %s presence: %v", req.UserID, channelID, err)
		}
	}

	// Publish presence change event if status changed
	if statusChanged {
		if err := s.publisher.PublishPresenceChangeEvent(ctx, req.UserID, req.Status, req.Channels); err != nil {
			log.Printf("Failed to publish presence change event for user %s: %v", req.UserID, err)
		}
	}

	return nil
}

// GetChannelPresence retrieves presence information for a channel
func (s *Service) GetChannelPresence(ctx context.Context, channelID string) (*ChannelPresence, error) {
	// Get user IDs from channel presence
	userIDs, err := s.redis.GetChannelPresence(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel presence: %w", err)
	}

	// Get presence status for each user
	users := make([]PresenceStatus, 0, len(userIDs))
	for _, userID := range userIDs {
		status, err := s.redis.GetPresence(ctx, userID)
		if err != nil {
			// User is offline, skip
			continue
		}

		var presence PresenceStatus
		if err := json.Unmarshal([]byte(status), &presence); err != nil {
			log.Printf("Failed to unmarshal presence for user %s: %v", userID, err)
			continue
		}

		users = append(users, presence)
	}

	return &ChannelPresence{
		ChannelID: channelID,
		Users:     users,
	}, nil
}

// processExpiredPresenceCleanup periodically cleans up expired presence entries from channel presence sets
func (s *Service) processExpiredPresenceCleanup() {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopChan:
			return

		case <-s.cleanupTicker.C:
			s.cleanupExpiredPresence()
		}
	}
}

// cleanupExpiredPresence removes expired users from channel presence sets
func (s *Service) cleanupExpiredPresence() {
	ctx := context.Background()

	s.cleanupMutex.Lock()
	s.lastCleanup = time.Now()
	s.cleanupMutex.Unlock()

	// Get all channel presence keys
	// In a production system, you'd want to track channels more efficiently
	// For now, we'll use a pattern scan (note: SCAN is better than KEYS in production)
	pattern := "presence:channel:*"

	// Use SCAN to iterate through keys
	iter := s.redis.GetClient().Scan(ctx, 0, pattern, 100).Iterator()

	channelsProcessed := 0
	usersRemoved := 0

	for iter.Next(ctx) {
		channelKey := iter.Val()

		// Extract channel ID from key
		// Key format: "presence:channel:{channelID}"
		channelID := channelKey[len("presence:channel:"):]

		// Get all users in the channel
		userIDs, err := s.redis.GetChannelPresence(ctx, channelID)
		if err != nil {
			log.Printf("Failed to get channel presence for %s: %v", channelID, err)
			continue
		}

		// Check each user's presence status
		for _, userID := range userIDs {
			// Try to get user's presence
			_, err := s.redis.GetPresence(ctx, userID)
			if err != nil {
				// User's presence has expired, remove from channel
				if err := s.redis.RemoveFromChannelPresence(ctx, channelID, userID); err != nil {
					log.Printf("Failed to remove expired user %s from channel %s: %v", userID, channelID, err)
				} else {
					usersRemoved++

					// Publish presence change event
					s.publisher.PublishPresenceChangeEvent(ctx, userID, "offline", []string{channelID})
				}
			}
		}

		channelsProcessed++
	}

	if err := iter.Err(); err != nil {
		log.Printf("Error during presence cleanup scan: %v", err)
	}

	if usersRemoved > 0 {
		log.Printf("Presence cleanup: processed %d channels, removed %d expired users", channelsProcessed, usersRemoved)
	}
}

// SyncPresenceAcrossShards synchronizes presence state between service instances
// This is called periodically or on-demand to ensure consistency
func (s *Service) SyncPresenceAcrossShards(ctx context.Context) error {
	// In a distributed system, each service instance maintains its own view
	// Redis acts as the source of truth, so synchronization happens naturally
	// through Redis operations. This method can be used for additional sync logic.

	// For now, we'll trigger a cleanup to ensure consistency
	s.cleanupExpiredPresence()

	return nil
}

// GetAggregatedChannelPresence retrieves and aggregates presence for multiple channels
func (s *Service) GetAggregatedChannelPresence(ctx context.Context, channelIDs []string) (map[string]*ChannelPresence, error) {
	result := make(map[string]*ChannelPresence)

	// Use goroutines to fetch presence for multiple channels concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	errChan := make(chan error, len(channelIDs))

	for _, channelID := range channelIDs {
		wg.Add(1)
		go func(chID string) {
			defer wg.Done()

			presence, err := s.GetChannelPresence(ctx, chID)
			if err != nil {
				errChan <- fmt.Errorf("failed to get presence for channel %s: %w", chID, err)
				return
			}

			mu.Lock()
			result[chID] = presence
			mu.Unlock()
		}(channelID)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	if len(errChan) > 0 {
		return result, <-errChan
	}

	return result, nil
}

// GetPresenceStats returns statistics about presence service
func (s *Service) GetPresenceStats(ctx context.Context) (map[string]interface{}, error) {
	s.cleanupMutex.RLock()
	lastCleanup := s.lastCleanup
	s.cleanupMutex.RUnlock()

	// Count total online users
	pattern := "presence:user:*"
	var totalUsers int64

	iter := s.redis.GetClient().Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		totalUsers++
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to count users: %w", err)
	}

	// Count total channels with presence
	channelPattern := "presence:channel:*"
	var totalChannels int64

	channelIter := s.redis.GetClient().Scan(ctx, 0, channelPattern, 100).Iterator()
	for channelIter.Next(ctx) {
		totalChannels++
	}

	if err := channelIter.Err(); err != nil {
		return nil, fmt.Errorf("failed to count channels: %w", err)
	}

	stats := map[string]interface{}{
		"total_online_users":    totalUsers,
		"total_active_channels": totalChannels,
		"last_cleanup":          lastCleanup,
		"heartbeat_queue_size":  len(s.heartbeatQueue),
		"batch_buffer_size":     len(s.batchBuffer),
	}

	return stats, nil
}

// BulkUpdatePresence updates presence for multiple users in a single batch
func (s *Service) BulkUpdatePresence(ctx context.Context, updates []HeartbeatRequest) error {
	for _, update := range updates {
		if err := s.processHeartbeat(ctx, update); err != nil {
			log.Printf("Failed to process bulk heartbeat for user %s: %v", update.UserID, err)
			// Continue processing other updates even if one fails
		}
	}
	return nil
}

// RemoveUserFromAllChannels removes a user from all channel presence sets
func (s *Service) RemoveUserFromAllChannels(ctx context.Context, userID string) error {
	// Get all channels the user is in
	// This would typically be tracked in a separate data structure
	// For now, we'll scan all channel presence sets

	pattern := "presence:channel:*"
	iter := s.redis.GetClient().Scan(ctx, 0, pattern, 100).Iterator()

	for iter.Next(ctx) {
		channelKey := iter.Val()
		channelID := channelKey[len("presence:channel:"):]

		// Remove user from this channel
		if err := s.redis.RemoveFromChannelPresence(ctx, channelID, userID); err != nil {
			log.Printf("Failed to remove user %s from channel %s: %v", userID, channelID, err)
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan channels: %w", err)
	}

	// Delete user's presence
	return s.redis.DeletePresence(ctx, userID)
}
