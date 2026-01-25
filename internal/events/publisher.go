package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	redisclient "real-time-chat-system/internal/redis"
)

// Publisher handles event publishing to Redis pub/sub
type Publisher struct {
	redis *redisclient.Client
}

// Event represents a system event
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	UserID    string                 `json:"user_id,omitempty"`
	ChannelID string                 `json:"channel_id,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Data      map[string]interface{} `json:"data"`
}

// EventType constants
const (
	EventTypeMessage           = "message"
	EventTypePresenceChange    = "presence_change"
	EventTypeCallStarted       = "call_started"
	EventTypeCallEnded         = "call_ended"
	EventTypeParticipantJoined = "participant_joined"
	EventTypeParticipantLeft   = "participant_left"
	EventTypeSignaling         = "signaling"
	EventTypeTyping            = "typing"
	EventTypeChannelUpdated    = "channel_updated"
)

// NewPublisher creates a new event publisher
func NewPublisher(redis *redisclient.Client) *Publisher {
	return &Publisher{
		redis: redis,
	}
}

// PublishMessageEvent publishes a message event
func (p *Publisher) PublishMessageEvent(ctx context.Context, channelID, userID, messageID, content string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeMessage,
		Timestamp: time.Now(),
		UserID:    userID,
		ChannelID: channelID,
		Data: map[string]interface{}{
			"message_id": messageID,
			"content":    content,
		},
	}

	return p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event)
}

// PublishPresenceChangeEvent publishes a presence change event
func (p *Publisher) PublishPresenceChangeEvent(ctx context.Context, userID, status string, channels []string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypePresenceChange,
		Timestamp: time.Now(),
		UserID:    userID,
		Data: map[string]interface{}{
			"status":   status,
			"channels": channels,
		},
	}

	// Publish to presence events channel
	if err := p.publishEvent(ctx, "presence:events", event); err != nil {
		return err
	}

	// Also publish to each channel the user is in
	for _, channelID := range channels {
		if err := p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event); err != nil {
			log.Printf("Failed to publish presence event to channel %s: %v", channelID, err)
		}
	}

	return nil
}

// PublishCallStartedEvent publishes a call started event
func (p *Publisher) PublishCallStartedEvent(ctx context.Context, callID, channelID, createdBy string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeCallStarted,
		Timestamp: time.Now(),
		UserID:    createdBy,
		ChannelID: channelID,
		CallID:    callID,
		Data: map[string]interface{}{
			"call_id":    callID,
			"created_by": createdBy,
		},
	}

	// Publish to channel events
	if err := p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event); err != nil {
		return err
	}

	// Also publish to calls events channel
	return p.publishEvent(ctx, "calls:events", event)
}

// PublishCallEndedEvent publishes a call ended event
func (p *Publisher) PublishCallEndedEvent(ctx context.Context, callID, channelID string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeCallEnded,
		Timestamp: time.Now(),
		ChannelID: channelID,
		CallID:    callID,
		Data: map[string]interface{}{
			"call_id": callID,
		},
	}

	// Publish to channel events
	if err := p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event); err != nil {
		return err
	}

	// Also publish to calls events channel
	return p.publishEvent(ctx, "calls:events", event)
}

// PublishParticipantJoinedEvent publishes a participant joined event
func (p *Publisher) PublishParticipantJoinedEvent(ctx context.Context, callID, channelID, userID string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeParticipantJoined,
		Timestamp: time.Now(),
		UserID:    userID,
		ChannelID: channelID,
		CallID:    callID,
		Data: map[string]interface{}{
			"call_id": callID,
			"user_id": userID,
		},
	}

	// Publish to channel events
	if err := p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event); err != nil {
		return err
	}

	// Also publish to calls events channel
	return p.publishEvent(ctx, "calls:events", event)
}

// PublishParticipantLeftEvent publishes a participant left event
func (p *Publisher) PublishParticipantLeftEvent(ctx context.Context, callID, channelID, userID string) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeParticipantLeft,
		Timestamp: time.Now(),
		UserID:    userID,
		ChannelID: channelID,
		CallID:    callID,
		Data: map[string]interface{}{
			"call_id": callID,
			"user_id": userID,
		},
	}

	// Publish to channel events
	if err := p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event); err != nil {
		return err
	}

	// Also publish to calls events channel
	return p.publishEvent(ctx, "calls:events", event)
}

// PublishSignalingEvent publishes a signaling event
func (p *Publisher) PublishSignalingEvent(ctx context.Context, callID, fromUserID, toUserID, messageType string, payload map[string]interface{}) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeSignaling,
		Timestamp: time.Now(),
		UserID:    fromUserID,
		CallID:    callID,
		Data: map[string]interface{}{
			"call_id":      callID,
			"from_user_id": fromUserID,
			"to_user_id":   toUserID,
			"message_type": messageType,
			"payload":      payload,
		},
	}

	// Publish to specific user's signaling channel
	return p.publishEvent(ctx, fmt.Sprintf("signaling:user:%s", toUserID), event)
}

// PublishTypingEvent publishes a typing indicator event
func (p *Publisher) PublishTypingEvent(ctx context.Context, channelID, userID string, isTyping bool) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeTyping,
		Timestamp: time.Now(),
		UserID:    userID,
		ChannelID: channelID,
		Data: map[string]interface{}{
			"is_typing": isTyping,
		},
	}

	return p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event)
}

// PublishChannelUpdatedEvent publishes a channel updated event
func (p *Publisher) PublishChannelUpdatedEvent(ctx context.Context, channelID string, updateData map[string]interface{}) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeChannelUpdated,
		Timestamp: time.Now(),
		ChannelID: channelID,
		Data:      updateData,
	}

	return p.publishEvent(ctx, fmt.Sprintf("events:channel:%s", channelID), event)
}

// publishEvent publishes an event to a Redis channel
func (p *Publisher) publishEvent(ctx context.Context, channel string, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := p.redis.Publish(ctx, channel, string(data)); err != nil {
		return fmt.Errorf("failed to publish event to channel %s: %w", channel, err)
	}

	log.Printf("Published event %s to channel %s", event.Type, channel)
	return nil
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("event_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}
