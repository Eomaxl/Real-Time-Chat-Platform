package sfu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"real-time-chat-system/internal/health"
	"real-time-chat-system/internal/metrics"
	"real-time-chat-system/internal/tracing"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Service represents the SFU Service
type Service struct {
	config        *SFUConfig
	healthChecker *health.Checker
	sessions      map[string]*SFUSession
	sessionsMu    sync.RWMutex
	metrics       *metrics.ServiceMetrics
	slaMonitor    *metrics.SLAMonitor
	api           *webrtc.API
	loadBalancer  *LoadBalancer
	healthMonitor *HealthMonitor
	scaler        *Scaler
}

// New creates a new SFU Service instance
func New(cfg *SFUConfig, healthChecker *health.Checker) (*Service, error) {
	// Initialize metrics
	serviceMetrics := metrics.NewServiceMetrics("sfu-service")
	slaMonitor := metrics.NewSLAMonitor(serviceMetrics, "sfu-service")

	// Create WebRTC API with media engine
	mediaEngine := &webrtc.MediaEngine{}

	// Register default codecs
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register codecs: %w", err)
	}

	// Create WebRTC API
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
	)

	// Initialize load balancer
	loadBalancer := NewLoadBalancer(StrategyLeastLoaded)

	service := &Service{
		config:        cfg,
		healthChecker: healthChecker,
		sessions:      make(map[string]*SFUSession),
		metrics:       serviceMetrics,
		slaMonitor:    slaMonitor,
		api:           api,
		loadBalancer:  loadBalancer,
	}

	// Initialize health monitor
	instanceID := fmt.Sprintf("sfu-%d", time.Now().Unix())
	region := "us-east" // In production, get from config
	service.healthMonitor = NewHealthMonitor(service, loadBalancer, instanceID, region, cfg.MaxPublishers)

	// Initialize scaler
	service.scaler = NewScaler(loadBalancer, 1, 10) // Min 1, Max 10 instances

	return service, nil
}

// Start starts background tasks for the SFU service
func (s *Service) Start(ctx context.Context) {
	// Start SLA monitoring
	go s.slaMonitor.Start(ctx)

	// Start session cleanup
	go s.cleanupSessions(ctx)

	// Start health monitoring
	go s.healthMonitor.Start(ctx)

	// Start load balancer health checks
	go s.loadBalancer.StartHealthChecks(ctx)

	// Start auto-scaling
	go s.scaler.Start(ctx)
}

// cleanupSessions periodically cleans up expired sessions
func (s *Service) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sessionsMu.Lock()
			for id, session := range s.sessions {
				// Check if session has expired (no publishers or subscribers)
				session.mu.RLock()
				isEmpty := len(session.Publishers) == 0 && len(session.Subscribers) == 0
				isExpired := time.Since(session.CreatedAt) > s.config.SessionTimeout
				session.mu.RUnlock()

				if isEmpty && isExpired {
					delete(s.sessions, id)
				}
			}
			s.sessionsMu.Unlock()
		}
	}
}

// Router returns the HTTP router for the SFU service
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(tracing.GinMiddleware("sfu-service"))
	router.Use(metrics.RecoveryMiddleware(s.metrics, "sfu-service"))
	router.Use(metrics.HTTPMetricsMiddleware(s.metrics, "sfu-service"))

	// Health endpoints
	router.GET("/health", gin.WrapF(s.healthChecker.Handler()))
	router.GET("/health/ready", gin.WrapF(s.healthChecker.ReadinessHandler()))
	router.GET("/health/live", gin.WrapF(health.LivenessHandler()))

	// Metrics endpoint for Prometheus
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Service endpoints
	v1 := router.Group("/v1")
	{
		v1.POST("/sessions", s.createSession)
		v1.POST("/sessions/:id/publish", s.handlePublish)
		v1.POST("/sessions/:id/subscribe", s.handleSubscribe)
		v1.DELETE("/sessions/:id/publish", s.handleUnpublish)
		v1.DELETE("/sessions/:id/subscribe", s.handleUnsubscribe)
		v1.GET("/sessions/:id/stats", s.getSessionStats)

		// Load balancing and scaling endpoints
		v1.GET("/instances", s.getInstances)
		v1.GET("/loadbalancer/stats", s.getLoadBalancerStats)
		v1.GET("/scaler/metrics", s.getScalerMetrics)
		v1.POST("/scaler/scale", s.manualScale)
	}

	return router
}

// createSession creates a new SFU session
func (s *Service) createSession(c *gin.Context) {
	var req struct {
		CallID    string `json:"call_id" binding:"required"`
		ChannelID string `json:"channel_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	session := s.GetOrCreateSession(req.CallID, req.ChannelID)

	c.JSON(http.StatusCreated, gin.H{
		"session_id": session.ID,
		"call_id":    session.CallID,
		"channel_id": session.ChannelID,
	})
}

// handlePublish handles a publish request (user wants to send media)
func (s *Service) handlePublish(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		UserID string                    `json:"user_id" binding:"required"`
		Offer  webrtc.SessionDescription `json:"offer" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	ctx := c.Request.Context()
	answer, err := s.HandlePublish(ctx, sessionID, req.UserID, req.Offer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"answer": answer,
	})
}

// handleSubscribe handles a subscribe request (user wants to receive media)
func (s *Service) handleSubscribe(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		UserID      string                    `json:"user_id" binding:"required"`
		PublisherID string                    `json:"publisher_id" binding:"required"`
		Offer       webrtc.SessionDescription `json:"offer" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	ctx := c.Request.Context()
	answer, err := s.HandleSubscribe(ctx, sessionID, req.UserID, req.PublisherID, req.Offer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"answer": answer,
	})
}

// handleUnpublish handles an unpublish request
func (s *Service) handleUnpublish(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		UserID string `json:"user_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := s.HandleUnpublish(ctx, sessionID, req.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Unpublished successfully"})
}

// handleUnsubscribe handles an unsubscribe request
func (s *Service) handleUnsubscribe(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		UserID      string `json:"user_id" binding:"required"`
		PublisherID string `json:"publisher_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := s.HandleUnsubscribe(ctx, sessionID, req.UserID, req.PublisherID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Unsubscribed successfully"})
}

// getSessionStats returns statistics for a session
func (s *Service) getSessionStats(c *gin.Context) {
	sessionID := c.Param("id")

	session, err := s.GetSession(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	stats := session.GetStats()
	c.JSON(http.StatusOK, stats)
}

// GetOrCreateSession gets or creates an SFU session
func (s *Service) GetOrCreateSession(callID, channelID string) *SFUSession {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, ok := s.sessions[callID]; ok {
		return session
	}

	session := NewSFUSession(callID, channelID)
	s.sessions[callID] = session
	return session
}

// GetSession gets an SFU session by ID
func (s *Service) GetSession(sessionID string) (*SFUSession, error) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

// HandlePublish handles a publish request from a user
func (s *Service) HandlePublish(ctx context.Context, sessionID, userID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Check publisher limit
	session.mu.RLock()
	publisherCount := len(session.Publishers)
	session.mu.RUnlock()

	if publisherCount >= s.config.MaxPublishers {
		return nil, fmt.Errorf("maximum publishers reached")
	}

	// Create peer connection configuration
	peerConnectionConfig := webrtc.Configuration{
		ICEServers: s.config.ICEServers,
	}

	// Create peer connection
	peerConnection, err := s.api.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// Add publisher to session
	publisher := session.AddPublisher(userID, peerConnection)

	// Handle incoming tracks
	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Create local track to forward to subscribers
		localTrack, err := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			remoteTrack.ID(),
			remoteTrack.StreamID(),
		)
		if err != nil {
			fmt.Printf("Failed to create local track: %v\n", err)
			return
		}

		// Add track to publisher
		publisher.AddTrack(localTrack)

		// Forward RTP packets from remote track to local track
		go s.forwardTrack(ctx, remoteTrack, localTrack)

		// Add track to all existing subscribers
		s.addTrackToSubscribers(session, userID, localTrack)
	})

	// Handle connection state changes
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			session.RemovePublisher(userID)
			peerConnection.Close()
		}
	})

	// Set remote description (offer)
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description (answer)
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	return &answer, nil
}

// HandleSubscribe handles a subscribe request from a user
func (s *Service) HandleSubscribe(ctx context.Context, sessionID, userID, publisherID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Check if publisher exists
	publisher, ok := session.GetPublisher(publisherID)
	if !ok {
		return nil, fmt.Errorf("publisher not found")
	}

	// Check subscriber limit
	session.mu.RLock()
	subscriberCount := len(session.Subscribers)
	session.mu.RUnlock()

	if subscriberCount >= s.config.MaxSubscribers {
		return nil, fmt.Errorf("maximum subscribers reached")
	}

	// Create peer connection configuration
	peerConnectionConfig := webrtc.Configuration{
		ICEServers: s.config.ICEServers,
	}

	// Get or create subscriber
	var subscriber *Subscriber
	if existingSub, ok := session.GetSubscriber(userID); ok {
		subscriber = existingSub
	} else {
		// Create peer connection
		peerConnection, err := s.api.NewPeerConnection(peerConnectionConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create peer connection: %w", err)
		}

		subscriber = session.AddSubscriber(userID, peerConnection)

		// Handle connection state changes
		peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
				session.RemoveSubscriber(userID)
				peerConnection.Close()
			}
		})
	}

	// Add publisher's tracks to subscriber
	tracks := publisher.GetTracks()
	for _, track := range tracks {
		if _, err := subscriber.PeerConnection.AddTrack(track); err != nil {
			return nil, fmt.Errorf("failed to add track: %w", err)
		}
	}

	// Mark as subscribed
	subscriber.Subscribe(publisherID)

	// Set remote description (offer)
	if err := subscriber.PeerConnection.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := subscriber.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description (answer)
	if err := subscriber.PeerConnection.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	return &answer, nil
}

// HandleUnpublish handles an unpublish request
func (s *Service) HandleUnpublish(ctx context.Context, sessionID, userID string) error {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return err
	}

	publisher, ok := session.GetPublisher(userID)
	if !ok {
		return fmt.Errorf("publisher not found")
	}

	// Close peer connection
	if err := publisher.PeerConnection.Close(); err != nil {
		return fmt.Errorf("failed to close peer connection: %w", err)
	}

	// Remove publisher from session
	session.RemovePublisher(userID)

	return nil
}

// HandleUnsubscribe handles an unsubscribe request
func (s *Service) HandleUnsubscribe(ctx context.Context, sessionID, userID, publisherID string) error {
	session, err := s.GetSession(sessionID)
	if err != nil {
		return err
	}

	subscriber, ok := session.GetSubscriber(userID)
	if !ok {
		return fmt.Errorf("subscriber not found")
	}

	// Unsubscribe from publisher
	subscriber.Unsubscribe(publisherID)

	// If not subscribed to anyone, remove subscriber
	subscriber.mu.RLock()
	isEmpty := len(subscriber.SubscribedTo) == 0
	subscriber.mu.RUnlock()

	if isEmpty {
		if err := subscriber.PeerConnection.Close(); err != nil {
			return fmt.Errorf("failed to close peer connection: %w", err)
		}
		session.RemoveSubscriber(userID)
	}

	return nil
}

// forwardTrack forwards RTP packets from remote track to local track
func (s *Service) forwardTrack(ctx context.Context, remoteTrack *webrtc.TrackRemote, localTrack *webrtc.TrackLocalStaticRTP) {
	buffer := make([]byte, 1500)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, _, err := remoteTrack.Read(buffer)
			if err != nil {
				if err == io.EOF {
					return
				}
				fmt.Printf("Error reading from remote track: %v\n", err)
				return
			}

			if _, err := localTrack.Write(buffer[:n]); err != nil {
				if err == io.ErrClosedPipe {
					return
				}
				fmt.Printf("Error writing to local track: %v\n", err)
			}
		}
	}
}

// addTrackToSubscribers adds a track to all subscribers of a publisher
func (s *Service) addTrackToSubscribers(session *SFUSession, publisherID string, track *webrtc.TrackLocalStaticRTP) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	for _, subscriber := range session.Subscribers {
		if subscriber.IsSubscribedTo(publisherID) {
			if _, err := subscriber.PeerConnection.AddTrack(track); err != nil {
				fmt.Printf("Failed to add track to subscriber: %v\n", err)
			}
		}
	}
}

// ApplyBandwidthLimit applies bandwidth limits to a peer connection
func (s *Service) ApplyBandwidthLimit(pc *webrtc.PeerConnection) error {
	// Note: Bandwidth limiting in Pion WebRTC v4 is typically done through
	// RTCP feedback and congestion control rather than direct parameter setting.
	// For production use, consider implementing custom interceptors or using
	// the built-in congestion control mechanisms.

	// This is a placeholder for bandwidth management
	// In a real implementation, you would:
	// 1. Use RTCP feedback to monitor bandwidth
	// 2. Implement custom interceptors for rate limiting
	// 3. Use simulcast with layer selection for adaptive bitrate

	return nil
}

// GetSessionStats returns aggregated statistics for all sessions
func (s *Service) GetSessionStats() map[string]*SFUStats {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()

	stats := make(map[string]*SFUStats)
	for id, session := range s.sessions {
		stats[id] = session.GetStats()
	}
	return stats
}

// MarshalJSON implements json.Marshaler for SessionDescription
func marshalSessionDescription(sd webrtc.SessionDescription) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type": sd.Type.String(),
		"sdp":  sd.SDP,
	})
}

// getInstances returns all SFU instances
func (s *Service) getInstances(c *gin.Context) {
	instances := s.loadBalancer.GetInstances()

	instanceData := make([]map[string]interface{}, 0, len(instances))
	for _, instance := range instances {
		instance.mu.RLock()
		data := map[string]interface{}{
			"id":                instance.ID,
			"address":           instance.Address,
			"port":              instance.Port,
			"region":            instance.Region,
			"active_sessions":   instance.ActiveSessions,
			"max_sessions":      instance.MaxSessions,
			"cpu_usage":         instance.CPUUsage,
			"memory_usage":      instance.MemoryUsage,
			"bandwidth_usage":   instance.BandwidthUsage,
			"healthy":           instance.Healthy,
			"last_health_check": instance.LastHealthCheck,
			"load_percentage":   instance.GetLoad(),
		}
		instance.mu.RUnlock()
		instanceData = append(instanceData, data)
	}

	c.JSON(http.StatusOK, gin.H{
		"instances": instanceData,
		"count":     len(instanceData),
	})
}

// getLoadBalancerStats returns load balancer statistics
func (s *Service) getLoadBalancerStats(c *gin.Context) {
	stats := s.loadBalancer.GetLoadBalancingStats()
	c.JSON(http.StatusOK, stats)
}

// getScalerMetrics returns scaler metrics
func (s *Service) getScalerMetrics(c *gin.Context) {
	metrics := s.scaler.GetScalingMetrics()
	c.JSON(http.StatusOK, metrics)
}

// manualScale handles manual scaling requests
func (s *Service) manualScale(c *gin.Context) {
	var req struct {
		Action string `json:"action" binding:"required,oneof=scale-up scale-down"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	ctx := c.Request.Context()
	action := ScalingAction(req.Action)

	if err := s.scaler.ManualScale(ctx, action); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Scaling action %s completed successfully", action),
	})
}
