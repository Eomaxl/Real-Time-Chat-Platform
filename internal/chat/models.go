package chat

import "time"

// Message represents a chat message
type Message struct {
	ID             string    `json:"id" db:"id"`
	ChannelID      string    `json:"channel_id" db:"channel_id"`
	UserID         string    `json:"user_id" db:"user_id"`
	Content        string    `json:"content" db:"content"`
	MessageType    string    `json:"message_type" db:"message_type"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
	IdempotencyKey *string   `json:"-" db:"idempotency_key"`
}

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	ChannelID      string `json:"channel_id" binding:"required"`
	UserID         string `json:"user_id" binding:"required"`
	Content        string `json:"content" binding:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
	MessageType    string `json:"message_type"`
}

// MessagePage represents a paginated response of messages
type MessagePage struct {
	Messages   []Message `json:"messages"`
	NextCursor *string   `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
	Total      int       `json:"total"`
}

// HistoryRequest represents a request for message history
type HistoryRequest struct {
	ChannelID string     `form:"channel_id" binding:"required"`
	UserID    string     `form:"user_id" binding:"required"`
	Cursor    string     `form:"cursor"`
	Limit     int        `form:"limit"`
	Since     *time.Time `form:"since" time_format:"2006-01-02T15:04:05Z07:00"`
	SinceID   string     `form:"since_id"`
}

// Channel represents a chat channel
type Channel struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Type      string    `json:"type" db:"type"` // "public", "private", "dm"
	CreatedBy string    `json:"created_by" db:"created_by"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	Members   []string  `json:"members,omitempty"`
}

// ChannelMember represents a channel membership
type ChannelMember struct {
	ChannelID string    `json:"channel_id" db:"channel_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	JoinedAt  time.Time `json:"joined_at" db:"joined_at"`
	Role      string    `json:"role" db:"role"`
}

// User represents a user
type User struct {
	ID           string    `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// WebSocketEvent represents an event sent over WebSocket
type WebSocketEvent struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
	ChannelID *string     `json:"channel_id,omitempty"`
	CallID    *string     `json:"call_id,omitempty"`
}

// MessageEvent represents a message event for WebSocket
type MessageEvent struct {
	Message   Message `json:"message"`
	ChannelID string  `json:"channel_id"`
}
