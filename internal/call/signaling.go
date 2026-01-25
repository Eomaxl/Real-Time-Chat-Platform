package call

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SignalingStateMachine manages the signaling state transitions for a participant
type SignalingStateMachine struct {
	repo *Repository
}

// NewSignalingStateMachine creates a new signaling state machine
func NewSignalingStateMachine(repo *Repository) *SignalingStateMachine {
	return &SignalingStateMachine{repo: repo}
}

// ValidateTransition validates if a signaling message is valid given the current state
func (sm *SignalingStateMachine) ValidateTransition(ctx context.Context, callID, userID, messageType string) error {
	// Get current participant state
	participants, err := sm.repo.GetParticipants(ctx, callID)
	if err != nil {
		return fmt.Errorf("failed to get participants: %w", err)
	}

	var currentState string
	found := false
	for _, p := range participants {
		if p.UserID == userID {
			currentState = p.SignalingState
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("participant not found in call")
	}

	// Validate state transition based on message type
	switch messageType {
	case SignalingTypeOffer:
		// Offer can be sent from joining or reconnecting state
		if currentState != SignalingStateJoining && currentState != SignalingStateReconnecting {
			return fmt.Errorf("offer can only be sent from joining or reconnecting state, current state: %s", currentState)
		}
	case SignalingTypeAnswer:
		// Answer can only be sent after receiving an offer
		if currentState != SignalingStateOfferSent {
			return fmt.Errorf("answer can only be sent after offer, current state: %s", currentState)
		}
	case SignalingTypeICECandidate:
		// ICE candidates can be sent in any state except joining
		if currentState == SignalingStateJoining {
			return fmt.Errorf("ICE candidates cannot be sent before offer/answer exchange")
		}
	case SignalingTypeICERestart:
		// ICE restart can be sent from connected or reconnecting state
		if currentState != SignalingStateConnected && currentState != SignalingStateReconnecting {
			return fmt.Errorf("ICE restart can only be sent from connected or reconnecting state, current state: %s", currentState)
		}
	default:
		return fmt.Errorf("unknown message type: %s", messageType)
	}

	return nil
}

// UpdateState updates the participant's signaling state based on the message type
func (sm *SignalingStateMachine) UpdateState(ctx context.Context, callID, userID, messageType string) error {
	var newState string

	switch messageType {
	case SignalingTypeOffer:
		newState = SignalingStateOfferSent
	case SignalingTypeAnswer:
		newState = SignalingStateAnswerReceived
	case SignalingTypeICECandidate:
		// ICE candidates don't change the state, but we might want to track connection
		// For now, if we're in answer-received, move to connected
		participants, err := sm.repo.GetParticipants(ctx, callID)
		if err != nil {
			return fmt.Errorf("failed to get participants: %w", err)
		}
		for _, p := range participants {
			if p.UserID == userID && p.SignalingState == SignalingStateAnswerReceived {
				newState = SignalingStateConnected
				break
			}
		}
		if newState == "" {
			return nil // No state change needed
		}
	case SignalingTypeICERestart:
		newState = SignalingStateReconnecting
	default:
		return fmt.Errorf("unknown message type: %s", messageType)
	}

	if newState != "" {
		return sm.repo.UpdateParticipantSignalingState(ctx, callID, userID, newState)
	}

	return nil
}

// ProcessSignalingMessage processes a signaling message with state validation
func (s *Service) ProcessSignalingMessage(ctx context.Context, msg SignalingMessage) error {
	// Validate that the call exists and is active
	session, err := s.repo.GetCallSession(ctx, msg.CallID)
	if err != nil {
		return fmt.Errorf("call session not found: %w", err)
	}

	if session.Status != CallStatusActive {
		return fmt.Errorf("call session is not active")
	}

	// Validate that both sender and receiver are participants
	participants, err := s.repo.GetParticipants(ctx, msg.CallID)
	if err != nil {
		return fmt.Errorf("failed to get participants: %w", err)
	}

	fromFound := false
	toFound := false
	for _, p := range participants {
		if p.UserID == msg.FromUserID {
			fromFound = true
		}
		if p.UserID == msg.ToUserID {
			toFound = true
		}
	}

	if !fromFound {
		return fmt.Errorf("sender is not a participant in the call")
	}
	if !toFound {
		return fmt.Errorf("recipient is not a participant in the call")
	}

	// Validate state transition
	stateMachine := NewSignalingStateMachine(s.repo)
	if err := stateMachine.ValidateTransition(ctx, msg.CallID, msg.FromUserID, msg.MessageType); err != nil {
		return fmt.Errorf("invalid state transition: %w", err)
	}

	// Update sender's state
	if err := stateMachine.UpdateState(ctx, msg.CallID, msg.FromUserID, msg.MessageType); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Relay the message to the recipient via Redis pub/sub
	if err := s.relaySignalingMessage(ctx, session, msg); err != nil {
		return fmt.Errorf("failed to relay signaling message: %w", err)
	}

	return nil
}

// relaySignalingMessage relays a signaling message to the recipient via Redis
func (s *Service) relaySignalingMessage(ctx context.Context, session *CallSession, msg SignalingMessage) error {
	event := map[string]interface{}{
		"type":         "signaling",
		"timestamp":    time.Now(),
		"channel_id":   session.ChannelID,
		"call_id":      msg.CallID,
		"from_user_id": msg.FromUserID,
		"to_user_id":   msg.ToUserID,
		"message_type": msg.MessageType,
		"payload":      msg.Payload,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal signaling event: %w", err)
	}

	// Publish to user-specific channel for targeted delivery
	channel := fmt.Sprintf("user:%s:signaling", msg.ToUserID)
	if err := s.redis.Publish(ctx, channel, eventJSON); err != nil {
		return fmt.Errorf("failed to publish signaling message: %w", err)
	}

	return nil
}

// ReconnectParticipant handles participant reconnection to a call
func (s *Service) ReconnectParticipant(ctx context.Context, callID, userID string) error {
	// Get the call session
	session, err := s.repo.GetCallSession(ctx, callID)
	if err != nil {
		return fmt.Errorf("call session not found: %w", err)
	}

	if session.Status != CallStatusActive {
		return fmt.Errorf("call session is not active")
	}

	// Check if user is a member of the channel
	isMember, err := s.repo.IsChannelMember(ctx, session.ChannelID, userID)
	if err != nil {
		return fmt.Errorf("failed to verify channel membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("user is not a member of the channel")
	}

	// Get participants to check if user was previously in the call
	participants, err := s.repo.GetParticipants(ctx, callID)
	if err != nil {
		return fmt.Errorf("failed to get participants: %w", err)
	}

	// Check if user is already an active participant
	isActive := false
	for _, p := range participants {
		if p.UserID == userID {
			isActive = true
			break
		}
	}

	if isActive {
		// User is already active, update state to reconnecting
		if err := s.repo.UpdateParticipantSignalingState(ctx, callID, userID, SignalingStateReconnecting); err != nil {
			return fmt.Errorf("failed to update participant state: %w", err)
		}
	} else {
		// User was not active, add them back as a participant
		if _, err := s.repo.AddParticipant(ctx, callID, userID); err != nil {
			return fmt.Errorf("failed to add participant: %w", err)
		}
		// Set state to reconnecting
		if err := s.repo.UpdateParticipantSignalingState(ctx, callID, userID, SignalingStateReconnecting); err != nil {
			return fmt.Errorf("failed to update participant state: %w", err)
		}
	}

	// Publish reconnection event
	event := map[string]interface{}{
		"type":       "participant_reconnecting",
		"timestamp":  time.Now(),
		"channel_id": session.ChannelID,
		"call_id":    callID,
		"user_id":    userID,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal reconnection event: %w", err)
	}

	channel := fmt.Sprintf("channel:%s:events", session.ChannelID)
	if err := s.redis.Publish(ctx, channel, eventJSON); err != nil {
		return fmt.Errorf("failed to publish reconnection event: %w", err)
	}

	return nil
}

// HandleICERestart handles ICE restart signaling for connection recovery
func (s *Service) HandleICERestart(ctx context.Context, callID, userID string) error {
	// Get the call session
	session, err := s.repo.GetCallSession(ctx, callID)
	if err != nil {
		return fmt.Errorf("call session not found: %w", err)
	}

	if session.Status != CallStatusActive {
		return fmt.Errorf("call session is not active")
	}

	// Verify user is a participant
	participants, err := s.repo.GetParticipants(ctx, callID)
	if err != nil {
		return fmt.Errorf("failed to get participants: %w", err)
	}

	found := false
	currentState := ""
	for _, p := range participants {
		if p.UserID == userID {
			found = true
			currentState = p.SignalingState
			break
		}
	}

	if !found {
		return fmt.Errorf("user is not a participant in the call")
	}

	// Verify user is in a state that allows ICE restart
	if currentState != SignalingStateConnected && currentState != SignalingStateReconnecting {
		return fmt.Errorf("ICE restart can only be initiated from connected or reconnecting state")
	}

	// Update state to reconnecting
	if err := s.repo.UpdateParticipantSignalingState(ctx, callID, userID, SignalingStateReconnecting); err != nil {
		return fmt.Errorf("failed to update participant state: %w", err)
	}

	// Publish ICE restart event
	event := map[string]interface{}{
		"type":       "ice_restart",
		"timestamp":  time.Now(),
		"channel_id": session.ChannelID,
		"call_id":    callID,
		"user_id":    userID,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal ICE restart event: %w", err)
	}

	channel := fmt.Sprintf("channel:%s:events", session.ChannelID)
	if err := s.redis.Publish(ctx, channel, eventJSON); err != nil {
		return fmt.Errorf("failed to publish ICE restart event: %w", err)
	}

	return nil
}

// ResumeSession allows a participant to resume their session after reconnection
func (s *Service) ResumeSession(ctx context.Context, callID, userID string) (*CallSession, error) {
	// Get the call session
	session, err := s.repo.GetCallSession(ctx, callID)
	if err != nil {
		return nil, fmt.Errorf("call session not found: %w", err)
	}

	if session.Status != CallStatusActive {
		return nil, fmt.Errorf("call session is not active")
	}

	// Verify user is a participant
	participants, err := s.repo.GetParticipants(ctx, callID)
	if err != nil {
		return nil, fmt.Errorf("failed to get participants: %w", err)
	}

	found := false
	for _, p := range participants {
		if p.UserID == userID {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("user is not a participant in the call")
	}

	// Return the session with current participants
	session.Participants = participants

	return session, nil
}
