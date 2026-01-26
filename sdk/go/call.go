package chatplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CallClient handles call-related operations
type CallClient struct {
	client *Client
}

// CallSession represents a WebRTC call session
type CallSession struct {
	ID           string        `json:"id"`
	ChannelID    string        `json:"channel_id"`
	CreatedBy    string        `json:"created_by"`
	Status       string        `json:"status"`
	CallType     string        `json:"call_type"`
	CreatedAt    time.Time     `json:"created_at"`
	EndedAt      *time.Time    `json:"ended_at,omitempty"`
	Participants []Participant `json:"participants,omitempty"`
}

// Participant represents a call participant
type Participant struct {
	UserID         string     `json:"user_id"`
	CallID         string     `json:"call_id"`
	JoinedAt       time.Time  `json:"joined_at"`
	LeftAt         *time.Time `json:"left_at,omitempty"`
	SignalingState string     `json:"signaling_state"`
}

// CreateCallRequest represents a request to create a call
type CreateCallRequest struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	CallType  string `json:"call_type"`
}

// JoinCallRequest represents a request to join a call
type JoinCallRequest struct {
	CallID string `json:"call_id"`
	UserID string `json:"user_id"`
}

// SignalingMessage represents a WebRTC signaling message
type SignalingMessage struct {
	CallID      string          `json:"call_id"`
	FromUserID  string          `json:"from_user_id"`
	ToUserID    string          `json:"to_user_id"`
	MessageType string          `json:"message_type"`
	Payload     json.RawMessage `json:"payload"`
}

// CreateCall creates a new call session
func (c *CallClient) CreateCall(ctx context.Context, channelID, callType string) (*CallSession, error) {
	if callType == "" {
		callType = "audio"
	}

	req := CreateCallRequest{
		ChannelID: channelID,
		UserID:    c.client.userID,
		CallType:  callType,
	}

	path := "/v1/calls"

	var result CallSession
	if err := c.client.doRequest(ctx, "POST", path, req, &result); err != nil {
		return nil, fmt.Errorf("failed to create call: %w", err)
	}

	return &result, nil
}

// JoinCall joins an existing call session
func (c *CallClient) JoinCall(ctx context.Context, callID string) (*Participant, error) {
	req := JoinCallRequest{
		CallID: callID,
		UserID: c.client.userID,
	}

	path := fmt.Sprintf("/v1/calls/%s/join", callID)

	var result struct {
		CallID      string      `json:"call_id"`
		Participant Participant `json:"participant"`
	}

	if err := c.client.doRequest(ctx, "POST", path, req, &result); err != nil {
		return nil, fmt.Errorf("failed to join call: %w", err)
	}

	return &result.Participant, nil
}

// LeaveCall leaves a call session
func (c *CallClient) LeaveCall(ctx context.Context, callID string) error {
	req := map[string]string{
		"user_id": c.client.userID,
	}

	path := fmt.Sprintf("/v1/calls/%s/leave", callID)

	if err := c.client.doRequest(ctx, "POST", path, req, nil); err != nil {
		return fmt.Errorf("failed to leave call: %w", err)
	}

	return nil
}

// SendSignalingMessage sends a WebRTC signaling message
func (c *CallClient) SendSignalingMessage(ctx context.Context, msg SignalingMessage) error {
	if msg.FromUserID == "" {
		msg.FromUserID = c.client.userID
	}

	path := fmt.Sprintf("/v1/calls/%s/signaling", msg.CallID)

	if err := c.client.doRequest(ctx, "POST", path, msg, nil); err != nil {
		return fmt.Errorf("failed to send signaling message: %w", err)
	}

	return nil
}

// SendOffer sends a WebRTC offer
func (c *CallClient) SendOffer(ctx context.Context, callID, toUserID string, offer interface{}) error {
	payload, err := json.Marshal(offer)
	if err != nil {
		return fmt.Errorf("failed to marshal offer: %w", err)
	}

	msg := SignalingMessage{
		CallID:      callID,
		FromUserID:  c.client.userID,
		ToUserID:    toUserID,
		MessageType: "offer",
		Payload:     payload,
	}

	return c.SendSignalingMessage(ctx, msg)
}

// SendAnswer sends a WebRTC answer
func (c *CallClient) SendAnswer(ctx context.Context, callID, toUserID string, answer interface{}) error {
	payload, err := json.Marshal(answer)
	if err != nil {
		return fmt.Errorf("failed to marshal answer: %w", err)
	}

	msg := SignalingMessage{
		CallID:      callID,
		FromUserID:  c.client.userID,
		ToUserID:    toUserID,
		MessageType: "answer",
		Payload:     payload,
	}

	return c.SendSignalingMessage(ctx, msg)
}

// SendICECandidate sends an ICE candidate
func (c *CallClient) SendICECandidate(ctx context.Context, callID, toUserID string, candidate interface{}) error {
	payload, err := json.Marshal(candidate)
	if err != nil {
		return fmt.Errorf("failed to marshal ICE candidate: %w", err)
	}

	msg := SignalingMessage{
		CallID:      callID,
		FromUserID:  c.client.userID,
		ToUserID:    toUserID,
		MessageType: "ice-candidate",
		Payload:     payload,
	}

	return c.SendSignalingMessage(ctx, msg)
}

// ReconnectToCall reconnects to a call after network failure
func (c *CallClient) ReconnectToCall(ctx context.Context, callID string) error {
	req := map[string]string{
		"user_id": c.client.userID,
	}

	path := fmt.Sprintf("/v1/calls/%s/reconnect", callID)

	if err := c.client.doRequest(ctx, "POST", path, req, nil); err != nil {
		return fmt.Errorf("failed to reconnect to call: %w", err)
	}

	return nil
}

// RequestICERestart requests an ICE restart for connection recovery
func (c *CallClient) RequestICERestart(ctx context.Context, callID string) error {
	req := map[string]string{
		"user_id": c.client.userID,
	}

	path := fmt.Sprintf("/v1/calls/%s/ice-restart", callID)

	if err := c.client.doRequest(ctx, "POST", path, req, nil); err != nil {
		return fmt.Errorf("failed to request ICE restart: %w", err)
	}

	return nil
}

// ResumeSession resumes a call session after reconnection
func (c *CallClient) ResumeSession(ctx context.Context, callID string) (*CallSession, error) {
	path := fmt.Sprintf("/v1/calls/%s/resume?user_id=%s", callID, c.client.userID)

	var result CallSession
	if err := c.client.doRequest(ctx, "GET", path, nil, &result); err != nil {
		return nil, fmt.Errorf("failed to resume session: %w", err)
	}

	return &result, nil
}
