package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/health"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ChatService interface defines the chat service operations
type ChatService interface {
	SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error)
	GetMessageHistory(ctx context.Context, req HistoryRequest) (*MessagePage, error)
	MarkMessageRead(ctx context.Context, req ReadReceiptRequest) error
}

// ReadReceiptRequest represents a read receipt request
type ReadReceiptRequest struct {
	ChannelID string `json:"channel_id" binding:"required"`
	UserID    string `json:"user_id" binding:"required"`
	MessageID string `json:"message_id" binding:"required"`
}

// Service represents the chat service
type Service struct {
	config        *config.ChatConfig
	healthChecker *health.Checker
	db            *database.PostgresDB
	redis         *redis.Client
	repository    *Repository
}

// New creates a new Chat service instance
func New(config *config.ChatConfig, healthChecker *health.Checker, db *database.PostgresDB, redisClient *redis.Client) (*Service, error) {
	service := &Service{
		config:        config,
		healthChecker: healthChecker,
		db:            db,
		redis:         redisClient,
		repository:    NewRepository(db),
	}

	// Add health check
	healthChecker.AddCheck("database", health.DatabaseHealthCheck(db))
	healthChecker.AddCheck("redis", health.RedisHealthCheck(health.NewRedisAdapter(redisClient)))

	return service, nil
}

// SendMessage implements the ChatService interface
func (s *Service) SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error) {
	// Validate channel membership
	isMember, err := s.repository.IsChannelMember(ctx, req.ChannelID, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to check channel membership: %w", err)
	}
	if !isMember {
		return nil, fmt.Errorf("user is not a member of the channel")
	}

	// Create message with idempotency support
	message, err := s.repository.CreateMessage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	// Publish real-time event to Redis pub/sub
	if err := s.publishMessageEvent(ctx, message); err != nil {
		// Log error but don't fail the request - message was already persisted
		// In production, you might want to use a retry mechanism or dead letter queue
		fmt.Printf("Failed to publish message event: %v\n", err)
	}

	return message, nil
}

// GetMessageHistory implements the ChatService interface
func (s *Service) GetMessageHistory(ctx context.Context, req HistoryRequest) (*MessagePage, error) {
	// Validate request parameters and permissions
	if err := s.repository.ValidateMessageFilters(ctx, req); err != nil {
		return nil, err
	}

	// Get message history with enhanced filtering
	return s.repository.GetMessageHistory(ctx, req)
}

// GetMessagesSince retrieves messages created after a specific timestamp
func (s *Service) GetMessagesSince(ctx context.Context, channelID, userID string, since time.Time, limit int) (*MessagePage, error) {
	req := HistoryRequest{
		ChannelID: channelID,
		UserID:    userID,
		Since:     &since,
		Limit:     limit,
	}

	// Validate request parameters and permissions
	if err := s.repository.ValidateMessageFilters(ctx, req); err != nil {
		return nil, err
	}

	return s.repository.GetMessagesSince(ctx, channelID, userID, since, limit)
}

// GetMessagesSinceID retrieves messages created after a specific message ID
func (s *Service) GetMessagesSinceID(ctx context.Context, channelID, userID, sinceID string, limit int) (*MessagePage, error) {
	req := HistoryRequest{
		ChannelID: channelID,
		UserID:    userID,
		SinceID:   sinceID,
		Limit:     limit,
	}

	// Validate request parameters and permissions
	if err := s.repository.ValidateMessageFilters(ctx, req); err != nil {
		return nil, err
	}

	return s.repository.GetMessagesSinceID(ctx, channelID, userID, sinceID, limit)
}

// MarkMessageRead implements the ChatService interface
func (s *Service) MarkMessageRead(ctx context.Context, req ReadReceiptRequest) error {
	// Validate channel membership
	isMember, err := s.repository.IsChannelMember(ctx, req.ChannelID, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to check channel membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("user is not a member of the channel")
	}

	// For now, just validate that the message exists
	// In a full implementation, you would store read receipts
	_, err = s.repository.GetMessage(ctx, req.MessageID, req.ChannelID)
	if err != nil {
		return fmt.Errorf("message not found: %w", err)
	}

	return nil
}

// publishMessageEvent publishes a message event to Redis pub/sub
func (s *Service) publishMessageEvent(ctx context.Context, message *Message) error {
	event := WebSocketEvent{
		Type:      "message",
		Timestamp: time.Now(),
		Data: MessageEvent{
			Message:   *message,
			ChannelID: message.ChannelID,
		},
		ChannelID: &message.ChannelID,
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to channel-specific topic
	channelTopic := fmt.Sprintf("channel:%s:events", message.ChannelID)
	if err := s.redis.Publish(ctx, channelTopic, eventData); err != nil {
		return fmt.Errorf("failed to publish to Redis: %s", err)
	}

	return nil
}

// Router returns the HTTP router for the chat service
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// Health endpoints
	router.GET("/health", gin.WrapF(s.healthChecker.Handler()))
	router.GET("/haelth/ready", gin.WrapF(s.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", s.metricsHandler)

	// Service endpoints
	v1 := router.Group("/v1")
	{
		v1.POST("/channels/:channel_id/messages", s.sendMessageHandler)
		v1.GET("/channels/:channel_id/messages", s.getMessagesHandler)
		v1.GET("/channels/:channel_id/messages/since/:timestamp", s.getMessagesSinceHandler)
		v1.GET("/channels/:channel_id/messages/since-id/:message_id", s.getMessagesSinceHandler)
		v1.POST("/channels/:channel_id/messages/:message_id/read", s.markMessageReadHandler)
	}
	return router
}

// sendMessageHandler handles message sending via HTTP
func (s *Service) sendMessageHandler(c *gin.Context) {
	channelID := c.Param("channel_id")
	if channelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id is required"})
		return
	}

	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Set channel ID from the URL parameter
	req.ChannelID = channelID

	// In a real implementation, we would extract the user_id from the JWT token. Currently using the user_id from the request body
	if req.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	message, err := s.SendMessage(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "user is not a member of the channel" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, message)
}

// getMessagesHandler handles message retrieval via HTTP
func (s *Service) getMessagesHandler(c *gin.Context) {
	channelID := c.Param("channel_id")
	if channelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "channel_id is required",
		})
		return
	}

	var req HistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set channel ID from URL parameter
	req.ChannelID = channelID

	// In a real implementation, we would extract user_id from JWT token
	if req.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	messages, err := s.GetMessageHistory(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "user is not a member of the channel" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, messages)
}

// getMessagesSinceHandler handles retrieving messages since a timestamp
func (s *Service) getMessagesSinceHandler(c *gin.Context) {
	channelID := c.Param("channel_id")
	timestampStr := c.Param("timestamp")

	if channelID == "" || timestampStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id and timestamp are required"})
		return
	}

	// Parse timestamp
	since, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid timestamp format, use RFC3339"})
		return
	}

	// Get query parameters
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	messages, err := s.GetMessagesSince(c.Request.Context(), channelID, userID, since, limit)
	if err != nil {
		if err.Error() == "user is not a member of the channel" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// getMessagesSinceIDHandler handles retrieving messages since a message ID
func (s *Service) getMessagesSinceIDHandler(c *gin.Context) {
	channelID := c.Param("channel_id")
	sinceID := c.Param("message_id")

	if channelID == "" || sinceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id and message_id are required"})
		return
	}

	// Get query parameters
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	messages, err := s.GetMessagesSinceID(c.Request.Context(), channelID, userID, sinceID, limit)
	if err != nil {
		if err.Error() == "user is not a member of the channel" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// markMessageReadHandler handles marking messages as read
func (s *Service) markMessageReadHandler(c *gin.Context) {
	channelID := c.Param("channel_id")
	messageID := c.Param("message_id")

	if channelID == "" || messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id and message_id are required"})
		return
	}

	var req ReadReceiptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set IDs from URL parameters
	req.ChannelID = channelID
	req.MessageID = messageID

	// In a real implementation, you would extract user_id from JWT token
	if req.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	err := s.MarkMessageRead(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "user is not a member of the channel" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "message marked as read"})
}

// metricHander exposes Prometheus metrics
func (s *Service) metricsHandler(c *gin.Context) {
	c.String(http.StatusOK, "# Prometheus metrics would be exposed here\n")
}
