package call

import (
	"time"
)

// CallSession represents a WebRTC call session
type CallSession struct {
	ID           string        `json:"id" db:"id"`
	ChannelID    string        `json:"channel_id" db:"channel_id"`
	CreatedBy    string        `json:"created_by" db:"created_by"`
	Status       string        `json:"status" db:"status"`
	CallType     string        `json:"call_type" db:"call_type"`
	CreatedAt    time.Time     `json:"created_at" db:"created_at"`
	EndedAt      *time.Time    `json:"ended_at,omitempty" db:"ended_at"`
	Participants []Participant `json:"participants,omitempty" db:"-"`
}

// Participant represents a call participant
type Participant struct {
	UserID         string     `json:"user_id" db:"user_id"`
	CallID         string     `json:"call_id" db:"call_id"`
	JoinedAt       time.Time  `json:"joined_at" db:"joined_at"`
	LeftAt         *time.Time `json:"left_at,omitempty" db:"left_at"`
	SignalingState string     `json:"signaling_state" db:"signaling_state"`
}

// CreateCallRequest represents a request to create a call
type CreateCallRequest struct {
	ChannelID string `json:"channel_id" binding:"required"`
	UserID    string `json:"user_id" binding:"required"`
	CallType  string `json:"call_type" binding:"required,oneof=audio video"`
}

// JoinCallRequest represents a request to join a call
type JoinCallRequest struct {
	CallID string `json:"call_id" binding:"required"`
	UserID string `json:"user_id" binding:"required"`
}

// LeaveCallRequest represents a request to leave a call
type LeaveCallRequest struct {
	CallID string `json:"call_id" binding:"required"`
	UserID string `json:"user_id" binding:"required"`
}

// SignalingMessage represents a WebRTC signaling message
type SignalingMessage struct {
	CallID      string `json:"call_id" binding:"required"`
	FromUserID  string `json:"from_user_id" binding:"required"`
	ToUserID    string `json:"to_user_id" binding:"required"`
	MessageType string `json:"message_type" binding:"required,oneof=offer answer ice-candidate ice-restart"`
	Payload     []byte `json:"payload" binding:"required"`
}

// Call status constants
const (
	CallStatusActive = "active"
	CallStatusEnded  = "ended"
)

// Call type constants
const (
	CallTypeAudio = "audio"
	CallTypeVideo = "video"
)

// Signaling state constants
const (
	SignalingStateJoining        = "joining"
	SignalingStateOfferSent      = "offer-sent"
	SignalingStateAnswerReceived = "answer-received"
	SignalingStateConnected      = "connected"
	SignalingStateReconnecting   = "reconnecting"
)

// Signaling message type constants
const (
	SignalingTypeOffer        = "offer"
	SignalingTypeAnswer       = "answer"
	SignalingTypeICECandidate = "ice-candidate"
	SignalingTypeICERestart   = "ice-restart"
)
