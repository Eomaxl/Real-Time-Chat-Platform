package chatplatform

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// ChatClient handles chat-related operations
type ChatClient struct {
	client *Client
}

// Message represents a chat message
type Message struct {
	ID             string    `json:"id"`
	ChannelID      string    `json:"channel_id"`
	UserID         string    `json:"user_id"`
	Content        string    `json:"content"`
	MessageType    string    `json:"message_type"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	IdempotencyKey *string   `json:"idempotency_key,omitempty"`
}

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	ChannelID      string  `json:"channel_id"`
	UserID         string  `json:"user_id"`
	Content        string  `json:"content"`
	MessageType    string  `json:"message_type,omitempty"`
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
}

// MessagePage represents a paginated list of messages
type MessagePage struct {
	Messages   []Message `json:"messages"`
	NextCursor *string   `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

// HistoryOptions represents options for retrieving message history
type HistoryOptions struct {
	Cursor  string
	Limit   int
	Since   *time.Time
	SinceID string
}

// SendMessage sends a message to a channel
func (c *ChatClient) SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error) {
	if req.UserID == "" {
		req.UserID = c.client.userID
	}
	
	if req.MessageType == "" {
		req.MessageType = "text"
	}

	path := fmt.Sprintf("/v1/channels/%s/messages", req.ChannelID)
	
	var result Message
	if err := c.client.doRequest(ctx, "POST", path, req, &result); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	return &result, nil
}

// GetMessageHistory retrieves message history for a channel
func (c *ChatClient) GetMessageHistory(ctx context.Context, channelID string, opts HistoryOptions) (*MessagePage, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}

	path := fmt.Sprintf("/v1/channels/%s/messages", channelID)
	
	// Build query parameters
	params := url.Values{}
	params.Set("user_id", c.client.userID)
	params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.SinceID != "" {
		params.Set("since_id", opts.SinceID)
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	path = path + "?" + params.Encode()

	var result MessagePage
	if err := c.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to get message history: %w", err)
	}

	return &result, nil
}

// GetMessagesSince retrieves messages created after a specific timestamp
func (c *ChatClient) GetMessagesSince(ctx context.Context, channelID string, since time.Time, limit int) (*MessagePage, error) {
	if limit == 0 {
		limit = 50
	}

	path := fmt.Sprintf("/v1/channels/%s/messages/since/%s", channelID, since.Format(time.RFC3339))
	
	params := url.Values{}
	params.Set("user_id", c.client.userID)
	params.Set("limit", fmt.Sprintf("%d", limit))
	
	path = path + "?" + params.Encode()

	var result MessagePage
	if err := c.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to get messages since timestamp: %w", err)
	}

	return &result, nil
}

// GetMessagesSinceID retrieves messages created after a specific message ID
func (c *ChatClient) GetMessagesSinceID(ctx context.Context, channelID, sinceID string, limit int) (*MessagePage, error) {
	if limit == 0 {
		limit = 50
	}

	path := fmt.Sprintf("/v1/channels/%s/messages/since-id/%s", channelID, sinceID)
	
	params := url.Values{}
	params.Set("user_id", c.client.userID)
	params.Set("limit", fmt.Sprintf("%d", limit))
	
	path = path + "?" + params.Encode()

	var result MessagePage
	if err := c.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to get messages since ID: %w", err)
	}

	return &result, nil
}

// MarkMessageRead marks a message as read
func (c *ChatClient) MarkMessageRead(ctx context.Context, channelID, messageID string) error {
	path := fmt.Sprintf("/v1/channels/%s/messages/%s/read", channelID, messageID)
	
	req := map[string]string{
		"user_id": c.client.userID,
	}

	if err := c.client.doRequest(ctx, "POST", path, req, nil); err != nil {
		return fmt.Errorf("failed to mark message as read: %w", err)
	}

	return nil
}
