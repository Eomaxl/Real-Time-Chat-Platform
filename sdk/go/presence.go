package chatplatform

import (
	"context"
	"fmt"
	"time"
)

// PresenceClient handles presence-related operations
type PresenceClient struct {
	client *Client
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

// HeartbeatRequest represents a heartbeat update request
type HeartbeatRequest struct {
	UserID   string   `json:"user_id"`
	Status   string   `json:"status,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// SendHeartbeat sends a heartbeat to update presence status
func (p *PresenceClient) SendHeartbeat(ctx context.Context, channels []string, status string) error {
	if status == "" {
		status = "online"
	}

	req := HeartbeatRequest{
		UserID:   p.client.userID,
		Status:   status,
		Channels: channels,
	}

	path := "/v1/heartbeat"
	if err := p.client.doRequest(ctx, "POST", path, req, nil); err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

// GetPresence retrieves presence status for a specific user
func (p *PresenceClient) GetPresence(ctx context.Context, userID string) (*PresenceStatus, error) {
	path := fmt.Sprintf("/v1/presence/%s", userID)

	var result PresenceStatus
	if err := p.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to get presence: %w", err)
	}

	return &result, nil
}

// GetChannelPresence retrieves presence information for a channel
func (p *PresenceClient) GetChannelPresence(ctx context.Context, channelID string) (*ChannelPresence, error) {
	path := fmt.Sprintf("/v1/channels/%s/presence", channelID)

	var result ChannelPresence
	if err := p.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to get channel presence: %w", err)
	}

	return &result, nil
}

// GetAggregatedChannelPresence retrieves presence for multiple channels
func (p *PresenceClient) GetAggregatedChannelPresence(ctx context.Context, channelIDs []string) (map[string]*ChannelPresence, error) {
	path := "/v1/channels/presence/bulk"

	req := map[string][]string{
		"channel_ids": channelIDs,
	}

	var result map[string]*ChannelPresence
	if err := p.client.doRequest(ctx, "POST", path, req, &result); err != nil {
		return nil, fmt.Errorf("failed to get aggregated channel presence: %w", err)
	}

	return result, nil
}

// StartHeartbeatLoop starts a background goroutine that sends heartbeats periodically
func (p *PresenceClient) StartHeartbeatLoop(ctx context.Context, channels []string, interval time.Duration) error {
	if interval == 0 {
		interval = 15 * time.Second // Default to 15 seconds
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send initial heartbeat
	if err := p.SendHeartbeat(ctx, channels, "online"); err != nil {
		return fmt.Errorf("failed to send initial heartbeat: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			// Send offline status before exiting
			_ = p.SendHeartbeat(context.Background(), channels, "offline")
			return ctx.Err()
		case <-ticker.C:
			if err := p.SendHeartbeat(ctx, channels, "online"); err != nil {
				// Log error but continue
				fmt.Printf("Failed to send heartbeat: %v\n", err)
			}
		}
	}
}
