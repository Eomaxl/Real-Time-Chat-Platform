package chat

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"real-time-chat-system/internal/database"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository handles database operations for chat
type Repository struct {
	db *database.PostgresDB
}

// NewRepository creates a new chat repository
func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{
		db: db,
	}
}

// CreateMessage creates a new message with idempotency support
func (r *Repository) CreateMessage(ctx context.Context, req SendMessageRequest) (*Message, error) {
	pool := r.db.GetShardByChannelID(req.ChannelID)

	// Set default message type if not provided
	messageType := req.MessageType
	if messageType == "" {
		messageType = "text"
	}

	// First, check if message with this idempotency key already exists
	if req.IdempotencyKey != "" {
		existingMessage, err := r.getMessageByIdempotencyKey(ctx, pool, req.IdempotencyKey)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to check idempotency key: %w", err)
		}
		if existingMessage != nil {
			// Message already exists, return it
			return existingMessage, nil
		}
	}

	// Create new message
	query := `
		INSERT INTO messages (channel_id, user_id, content, message_type, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, channel_id, user_id, content, message_type, created_at, updated_at, idempotency_key
	`

	var idempotencyKey *string
	if req.IdempotencyKey != "" {
		idempotencyKey = &req.IdempotencyKey
	}

	var message Message
	err := pool.QueryRow(ctx, query, req.ChannelID, req.UserID, req.Content, messageType, idempotencyKey).Scan(
		&message.ID,
		&message.ChannelID,
		&message.UserID,
		&message.Content,
		&message.MessageType,
		&message.CreatedAt,
		&message.UpdatedAt,
		&message.IdempotencyKey,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	return &message, nil
}

// getMessageByIdempotencyKey retrieves a message by its idempotency key
func (r *Repository) getMessageByIdempotencyKey(ctx context.Context, pool *pgxpool.Pool, idempotencyKey string) (*Message, error) {
	query := `
		SELECT id, channel_id, user_id, content, message_type, created_at, updated_at, idempotency_key
		FROM messages
		WHERE idempotency_key = $1
	`

	var message Message
	err := pool.QueryRow(ctx, query, idempotencyKey).Scan(
		&message.ID,
		&message.ChannelID,
		&message.UserID,
		&message.Content,
		&message.MessageType,
		&message.CreatedAt,
		&message.UpdatedAt,
		&message.IdempotencyKey,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}

	return &message, nil
}

// GetMessageHistory retrieves message history with pagination
func (r *Repository) GetMessageHistory(ctx context.Context, req HistoryRequest) (*MessagePage, error) {
	pool := r.db.GetShardByChannelID(req.ChannelID)

	// Set default limit if not provided
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50 // Default limit
	}

	// Build query with filters
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Always filter by channel
	conditions = append(conditions, fmt.Sprintf("channel_id = $%d", argIndex))
	args = append(args, req.ChannelID)
	argIndex++

	// Handle cursor-based pagination (cursor represents a timestamp)
	if req.Cursor != "" {
		cursorTime, err := r.decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIndex))
		args = append(args, cursorTime)
		argIndex++
	}

	// Handle since timestamp filter (messages created after this time)
	if req.Since != nil {
		conditions = append(conditions, fmt.Sprintf("created_at > $%d", argIndex))
		args = append(args, *req.Since)
		argIndex++
	}

	// Handle since message ID filter (messages created after this message)
	if req.SinceID != "" {
		// Get the timestamp of the since message
		sinceQuery := `SELECT created_at FROM messages WHERE id = $1 AND channel_id = $2`
		var sinceTime time.Time
		err := pool.QueryRow(ctx, sinceQuery, req.SinceID, req.ChannelID).Scan(&sinceTime)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, fmt.Errorf("since message not found")
			}
			return nil, fmt.Errorf("failed to get since message timestamp: %w", err)
		}

		// Use > for since_id to get messages after the specified message
		conditions = append(conditions, fmt.Sprintf("created_at > $%d", argIndex))
		args = append(args, sinceTime)
		argIndex++
	}

	// Build the main query with consistent ordering (newest first for pagination, oldest first for since filters)
	whereClause := strings.Join(conditions, " AND ")
	var orderBy string

	// If using since filters, order oldest first to get newer messages
	if req.Since != nil || req.SinceID != "" {
		orderBy = "ORDER BY created_at ASC"
	} else {
		// Default pagination order: newest first
		orderBy = "ORDER BY created_at DESC"
	}

	query := fmt.Sprintf(`
		SELECT id, channel_id, user_id, content, message_type, created_at, updated_at, idempotency_key
		FROM messages
		WHERE %s
		%s
		LIMIT $%d
	`, whereClause, orderBy, argIndex)

	args = append(args, limit+1) // Get one extra to check if there are more

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		err := rows.Scan(
			&message.ID,
			&message.ChannelID,
			&message.UserID,
			&message.Content,
			&message.MessageType,
			&message.CreatedAt,
			&message.UpdatedAt,
			&message.IdempotencyKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	// Check if there are more messages
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit] // Remove the extra message
	}

	// Generate next cursor if there are more messages and we're doing pagination (not since filtering)
	var nextCursor *string
	if hasMore && len(messages) > 0 && req.Since == nil && req.SinceID == "" {
		cursor := r.encodeCursor(messages[len(messages)-1].CreatedAt)
		nextCursor = &cursor
	}

	// Get total count for the channel (without filters for performance)
	totalQuery := `SELECT COUNT(*) FROM messages WHERE channel_id = $1`
	var total int
	err = pool.QueryRow(ctx, totalQuery, req.ChannelID).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	return &MessagePage{
		Messages:   messages,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      total,
	}, nil
}

// GetMessage retrieves a single message by ID
func (r *Repository) GetMessage(ctx context.Context, messageID, channelID string) (*Message, error) {
	pool := r.db.GetShardByChannelID(channelID)

	query := `
		SELECT id, channel_id, user_id, content, message_type, created_at, updated_at, idempotency_key
		FROM messages
		WHERE id = $1 AND channel_id = $2
	`

	var message Message
	err := pool.QueryRow(ctx, query, messageID, channelID).Scan(
		&message.ID,
		&message.ChannelID,
		&message.UserID,
		&message.Content,
		&message.MessageType,
		&message.CreatedAt,
		&message.UpdatedAt,
		&message.IdempotencyKey,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	return &message, nil
}

// IsChannelMember checks if a user is a member of a channel
func (r *Repository) IsChannelMember(ctx context.Context, channelID, userID string) (bool, error) {
	pool := r.db.GetShardByChannelID(channelID)

	query := `
		SELECT EXISTS(
			SELECT 1 FROM channel_members 
			WHERE channel_id = $1 AND user_id = $2
		)
	`

	var exists bool
	err := pool.QueryRow(ctx, query, channelID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check channel membership: %w", err)
	}

	return exists, nil
}

// GetChannel retrieves channel information
func (r *Repository) GetChannel(ctx context.Context, channelID string) (*Channel, error) {
	pool := r.db.GetShardByChannelID(channelID)

	query := `
		SELECT id, name, type, created_by, created_at, updated_at
		FROM channels
		WHERE id = $1
	`

	var channel Channel
	err := pool.QueryRow(ctx, query, channelID).Scan(
		&channel.ID,
		&channel.Name,
		&channel.Type,
		&channel.CreatedBy,
		&channel.CreatedAt,
		&channel.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	return &channel, nil
}

// GetMessagesSince retrieves messages created after a specific timestamp
func (r *Repository) GetMessagesSince(ctx context.Context, channelID, userID string, since time.Time, limit int) (*MessagePage, error) {
	req := HistoryRequest{
		ChannelID: channelID,
		UserID:    userID,
		Since:     &since,
		Limit:     limit,
	}
	return r.GetMessageHistory(ctx, req)
}

// GetMessagesSinceID retrieves messages created after a specific message ID
func (r *Repository) GetMessagesSinceID(ctx context.Context, channelID, userID, sinceID string, limit int) (*MessagePage, error) {
	req := HistoryRequest{
		ChannelID: channelID,
		UserID:    userID,
		SinceID:   sinceID,
		Limit:     limit,
	}
	return r.GetMessageHistory(ctx, req)
}

// GetMessagesWithCursor retrieves messages using cursor-based pagination
func (r *Repository) GetMessagesWithCursor(ctx context.Context, channelID, userID, cursor string, limit int) (*MessagePage, error) {
	req := HistoryRequest{
		ChannelID: channelID,
		UserID:    userID,
		Cursor:    cursor,
		Limit:     limit,
	}
	return r.GetMessageHistory(ctx, req)
}

// ValidateMessageFilters validates the filtering parameters
func (r *Repository) ValidateMessageFilters(ctx context.Context, req HistoryRequest) error {
	// Validate channel exists
	_, err := r.GetChannel(ctx, req.ChannelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	// Validate user is channel member
	isMember, err := r.IsChannelMember(ctx, req.ChannelID, req.UserID)
	if err != nil {
		return fmt.Errorf("failed to check channel membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("user is not a member of the channel")
	}

	// Validate cursor if provided
	if req.Cursor != "" {
		_, err := r.decodeCursor(req.Cursor)
		if err != nil {
			return fmt.Errorf("invalid cursor: %w", err)
		}
	}

	// Validate since message ID if provided
	if req.SinceID != "" {
		_, err := r.GetMessage(ctx, req.SinceID, req.ChannelID)
		if err != nil {
			return fmt.Errorf("since message not found: %w", err)
		}
	}

	return nil
}

// encodeCursor encodes a timestamp into a cursor string
func (r *Repository) encodeCursor(t time.Time) string {
	// Use Unix timestamp in nanoseconds for precision
	timestamp := strconv.FormatInt(t.UnixNano(), 10)
	return base64.URLEncoding.EncodeToString([]byte(timestamp))
}

// decodeCursor decodes a cursor string into a timestamp
func (r *Repository) decodeCursor(cursor string) (time.Time, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	timestamp, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	return time.Unix(0, timestamp), nil
}
