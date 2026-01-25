package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	redisclient "real-time-chat-system/internal/redis"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ReplayManager handles event replay for reconnection scenarios
type ReplayManager struct {
	redis *redisclient.Client
}

// ReplayRequest represents a request for event replay
type ReplayRequest struct {
	UserID      string    `json:"user_id"`
	SessionID   string    `json:"session_id"`
	LastEventID string    `json:"last_event_id,omitempty"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
	Channels    []string  `json:"channels"`
	MaxEvents   int       `json:"max_events,omitempty"`
}

// ReplayResponse represents the response for event replay
type ReplayResponse struct {
	Events      []Event `json:"events"`
	HasMore     bool    `json:"has_more"`
	LastEventID string  `json:"last_event_id"`
}

// NewReplayManager creates a new event replay manager
func NewReplayManager(redis *redisclient.Client) *ReplayManager {
	return &ReplayManager{
		redis: redis,
	}
}

// StoreEventForReplay stores an event for potential replay
func (r *ReplayManager) StoreEventForReplay(ctx context.Context, event Event) error {
	// Store event in a sorted set with timestamp as score for each channel
	if event.ChannelID != "" {
		if err := r.storeChannelEvent(ctx, event); err != nil {
			return err
		}
	}

	// Store event for user-specific events (like signaling)
	if event.UserID != "" && (event.Type == EventTypeSignaling || event.Type == EventTypePresenceChange) {
		if err := r.storeUserEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

// ReplayEvents replays events for a reconnecting client
func (r *ReplayManager) ReplayEvents(ctx context.Context, req ReplayRequest) (*ReplayResponse, error) {
	if req.MaxEvents == 0 {
		req.MaxEvents = 100 // Default limit
	}

	var allEvents []Event
	cutoffTime := req.LastSeen
	if cutoffTime.IsZero() {
		cutoffTime = time.Now().Add(-5 * time.Minute) // Default to 5 minutes ago
	}

	// Replay channel events
	for _, channelID := range req.Channels {
		events, err := r.getChannelEvents(ctx, channelID, cutoffTime, req.MaxEvents)
		if err != nil {
			log.Printf("Failed to get channel events for %s: %v", channelID, err)
			continue
		}
		allEvents = append(allEvents, events...)
	}

	// Replay user-specific events
	userEvents, err := r.getUserEvents(ctx, req.UserID, cutoffTime, req.MaxEvents)
	if err != nil {
		log.Printf("Failed to get user events for %s: %v", req.UserID, err)
	} else {
		allEvents = append(allEvents, userEvents...)
	}

	// Sort events by timestamp
	allEvents = r.sortEventsByTimestamp(allEvents)

	// Limit to max events
	hasMore := len(allEvents) > req.MaxEvents
	if hasMore {
		allEvents = allEvents[:req.MaxEvents]
	}

	var lastEventID string
	if len(allEvents) > 0 {
		lastEventID = allEvents[len(allEvents)-1].ID
	}

	return &ReplayResponse{
		Events:      allEvents,
		HasMore:     hasMore,
		LastEventID: lastEventID,
	}, nil
}

// storeChannelEvent stores an event for channel replay
func (r *ReplayManager) storeChannelEvent(ctx context.Context, event Event) error {
	key := fmt.Sprintf("replay:channel:%s", event.ChannelID)
	score := float64(event.Timestamp.UnixNano())

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Store in sorted set
	if err := r.redis.GetClient().ZAdd(ctx, key, goredis.Z{
		Score:  score,
		Member: string(eventData),
	}).Err(); err != nil {
		return fmt.Errorf("failed to store channel event: %w", err)
	}

	// Set expiration for the key (24 hours)
	if err := r.redis.GetClient().Expire(ctx, key, 24*time.Hour).Err(); err != nil {
		log.Printf("Failed to set expiration for replay key %s: %v", key, err)
	}

	// Trim old events (keep only last 1000 events per channel)
	if err := r.redis.GetClient().ZRemRangeByRank(ctx, key, 0, -1001).Err(); err != nil {
		log.Printf("Failed to trim old events for key %s: %v", key, err)
	}

	return nil
}

// storeUserEvent stores an event for user-specific replay
func (r *ReplayManager) storeUserEvent(ctx context.Context, event Event) error {
	key := fmt.Sprintf("replay:user:%s", event.UserID)
	score := float64(event.Timestamp.UnixNano())

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Store in sorted set
	if err := r.redis.GetClient().ZAdd(ctx, key, goredis.Z{
		Score:  score,
		Member: string(eventData),
	}).Err(); err != nil {
		return fmt.Errorf("failed to store user event: %w", err)
	}

	// Set expiration for the key (1 hour for user events)
	if err := r.redis.GetClient().Expire(ctx, key, time.Hour).Err(); err != nil {
		log.Printf("Failed to set expiration for user replay key %s: %v", key, err)
	}

	// Trim old events (keep only last 100 events per user)
	if err := r.redis.GetClient().ZRemRangeByRank(ctx, key, 0, -101).Err(); err != nil {
		log.Printf("Failed to trim old user events for key %s: %v", key, err)
	}

	return nil
}

// getChannelEvents retrieves events for a channel since a given time
func (r *ReplayManager) getChannelEvents(ctx context.Context, channelID string, since time.Time, limit int) ([]Event, error) {
	key := fmt.Sprintf("replay:channel:%s", channelID)
	minScore := float64(since.UnixNano())
	maxScore := float64(time.Now().UnixNano())

	// Get events from sorted set
	results, err := r.redis.GetClient().ZRangeByScore(ctx, key, &goredis.ZRangeBy{
		Min:   fmt.Sprintf("%f", minScore),
		Max:   fmt.Sprintf("%f", maxScore),
		Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get channel events: %w", err)
	}

	var events []Event
	for _, result := range results {
		var event Event
		if err := json.Unmarshal([]byte(result), &event); err != nil {
			log.Printf("Failed to unmarshal event: %v", err)
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// getUserEvents retrieves user-specific events since a given time
func (r *ReplayManager) getUserEvents(ctx context.Context, userID string, since time.Time, limit int) ([]Event, error) {
	key := fmt.Sprintf("replay:user:%s", userID)
	minScore := float64(since.UnixNano())
	maxScore := float64(time.Now().UnixNano())

	// Get events from sorted set
	results, err := r.redis.GetClient().ZRangeByScore(ctx, key, &goredis.ZRangeBy{
		Min:   fmt.Sprintf("%f", minScore),
		Max:   fmt.Sprintf("%f", maxScore),
		Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get user events: %w", err)
	}

	var events []Event
	for _, result := range results {
		var event Event
		if err := json.Unmarshal([]byte(result), &event); err != nil {
			log.Printf("Failed to unmarshal user event: %v", err)
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// sortEventsByTimestamp sorts events by timestamp
func (r *ReplayManager) sortEventsByTimestamp(events []Event) []Event {
	// Simple bubble sort for small arrays (in production, use sort.Slice)
	for i := 0; i < len(events)-1; i++ {
		for j := 0; j < len(events)-i-1; j++ {
			if events[j].Timestamp.After(events[j+1].Timestamp) {
				events[j], events[j+1] = events[j+1], events[j]
			}
		}
	}
	return events
}

// CleanupOldEvents removes old events from replay storage
func (r *ReplayManager) CleanupOldEvents(ctx context.Context) error {
	// This would typically be run as a background job
	cutoffTime := time.Now().Add(-24 * time.Hour)
	maxScore := float64(cutoffTime.UnixNano())

	// Get all replay keys
	keys, err := r.redis.GetClient().Keys(ctx, "replay:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get replay keys: %w", err)
	}

	for _, key := range keys {
		// Remove old events
		if err := r.redis.GetClient().ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%f", maxScore)).Err(); err != nil {
			log.Printf("Failed to cleanup old events for key %s: %v", key, err)
		}
	}

	return nil
}
