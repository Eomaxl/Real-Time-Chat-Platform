package call

import (
	"context"
	"database/sql"
	"fmt"
	"real-time-chat-system/internal/database"
	"time"

	"github.com/google/uuid"
)

// Repository handles call data persistence
type Repository struct {
	db *database.PostgresDB
}

// NewRepository creates a new call repository
func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// CreateCallSession creates a new call session in the database
func (r *Repository) CreateCallSession(ctx context.Context, req CreateCallRequest) (*CallSession, error) {
	callID := uuid.New().String()
	now := time.Now()

	query := `
		INSERT INTO call_sessions (id, channel_id, created_by, status, call_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, channel_id, created_by, status, call_type, created_at, ended_at
	`

	pool := r.db.GetShardByChannelID(req.ChannelID)
	var session CallSession
	err := pool.QueryRow(ctx, query,
		callID,
		req.ChannelID,
		req.UserID,
		CallStatusActive,
		req.CallType,
		now,
	).Scan(
		&session.ID,
		&session.ChannelID,
		&session.CreatedBy,
		&session.Status,
		&session.CallType,
		&session.CreatedAt,
		&session.EndedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create call session: %w", err)
	}

	return &session, nil
}

// GetCallSession retrieves a call session by ID
func (r *Repository) GetCallSession(ctx context.Context, callID string) (*CallSession, error) {
	query := `
		SELECT id, channel_id, created_by, status, call_type, created_at, ended_at
		FROM call_sessions
		WHERE id = $1
	`

	// Use first shard for call lookups (in production, would need call->channel mapping)
	pool := r.db.GetShard(database.ShardKey(callID))
	var session CallSession
	err := pool.QueryRow(ctx, query, callID).Scan(
		&session.ID,
		&session.ChannelID,
		&session.CreatedBy,
		&session.Status,
		&session.CallType,
		&session.CreatedAt,
		&session.EndedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("call session not found: %s", callID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get call session: %w", err)
	}

	return &session, nil
}

// AddParticipant adds a participant to a call session
func (r *Repository) AddParticipant(ctx context.Context, callID, userID string) (*Participant, error) {
	now := time.Now()

	query := `
		INSERT INTO call_participants (call_id, user_id, joined_at, signaling_state)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (call_id, user_id) DO UPDATE
		SET left_at = NULL, signaling_state = $4
		RETURNING call_id, user_id, joined_at, left_at, signaling_state
	`

	pool := r.db.GetShard(database.ShardKey(callID))
	var participant Participant
	err := pool.QueryRow(ctx, query,
		callID,
		userID,
		now,
		SignalingStateJoining,
	).Scan(
		&participant.CallID,
		&participant.UserID,
		&participant.JoinedAt,
		&participant.LeftAt,
		&participant.SignalingState,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to add participant: %w", err)
	}

	return &participant, nil
}

// RemoveParticipant marks a participant as having left the call
func (r *Repository) RemoveParticipant(ctx context.Context, callID, userID string) error {
	now := time.Now()

	query := `
		UPDATE call_participants
		SET left_at = $1
		WHERE call_id = $2 AND user_id = $3 AND left_at IS NULL
	`

	pool := r.db.GetShard(database.ShardKey(callID))
	result, err := pool.Exec(ctx, query, now, callID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove participant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("participant not found or already left")
	}

	return nil
}

// GetParticipants retrieves all active participants in a call
func (r *Repository) GetParticipants(ctx context.Context, callID string) ([]Participant, error) {
	query := `
		SELECT call_id, user_id, joined_at, left_at, signaling_state
		FROM call_participants
		WHERE call_id = $1 AND left_at IS NULL
		ORDER BY joined_at ASC
	`

	pool := r.db.GetShard(database.ShardKey(callID))
	rows, err := pool.Query(ctx, query, callID)
	if err != nil {
		return nil, fmt.Errorf("failed to get participants: %w", err)
	}
	defer rows.Close()

	var participants []Participant
	for rows.Next() {
		var p Participant
		err := rows.Scan(
			&p.CallID,
			&p.UserID,
			&p.JoinedAt,
			&p.LeftAt,
			&p.SignalingState,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan participant: %w", err)
		}
		participants = append(participants, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating participants: %w", err)
	}

	return participants, nil
}

// UpdateParticipantSignalingState updates the signaling state of a participant
func (r *Repository) UpdateParticipantSignalingState(ctx context.Context, callID, userID, state string) error {
	query := `
		UPDATE call_participants
		SET signaling_state = $1
		WHERE call_id = $2 AND user_id = $3 AND left_at IS NULL
	`

	pool := r.db.GetShard(database.ShardKey(callID))
	result, err := pool.Exec(ctx, query, state, callID, userID)
	if err != nil {
		return fmt.Errorf("failed to update signaling state: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("participant not found or already left")
	}

	return nil
}

// EndCallSession marks a call session as ended
func (r *Repository) EndCallSession(ctx context.Context, callID string) error {
	now := time.Now()

	query := `
		UPDATE call_sessions
		SET status = $1, ended_at = $2
		WHERE id = $3 AND status = $4
	`

	pool := r.db.GetShard(database.ShardKey(callID))
	result, err := pool.Exec(ctx, query, CallStatusEnded, now, callID, CallStatusActive)
	if err != nil {
		return fmt.Errorf("failed to end call session: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("call session not found or already ended")
	}

	return nil
}

// IsChannelMember checks if a user is a member of a channel
func (r *Repository) IsChannelMember(ctx context.Context, channelID, userID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM channel_members
			WHERE channel_id = $1 AND user_id = $2
		)
	`

	pool := r.db.GetShardByChannelID(channelID)
	var exists bool
	err := pool.QueryRow(ctx, query, channelID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check channel membership: %w", err)
	}

	return exists, nil
}

// GetActiveCallForChannel retrieves the active call session for a channel if one exists
func (r *Repository) GetActiveCallForChannel(ctx context.Context, channelID string) (*CallSession, error) {
	query := `
		SELECT id, channel_id, created_by, status, call_type, created_at, ended_at
		FROM call_sessions
		WHERE channel_id = $1 AND status = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	pool := r.db.GetShardByChannelID(channelID)
	var session CallSession
	err := pool.QueryRow(ctx, query, channelID, CallStatusActive).Scan(
		&session.ID,
		&session.ChannelID,
		&session.CreatedBy,
		&session.Status,
		&session.CallType,
		&session.CreatedAt,
		&session.EndedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No active call
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active call: %w", err)
	}

	return &session, nil
}
