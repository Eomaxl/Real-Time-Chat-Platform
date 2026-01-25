package batch

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageBatch handles batch insertion of messages
type MessageBatch struct {
	pool      *pgxpool.Pool
	processor *Processor
}

// MessageItem represents a message to be inserted
type MessageItem struct {
	ChannelID      string
	UserID         string
	Content        string
	MessageType    string
	IdempotencyKey *string
	CreatedAt      time.Time
}

// NewMessageBatch creates a new message batch processor
func NewMessageBatch(pool *pgxpool.Pool, config Config) *MessageBatch {
	mb := &MessageBatch{
		pool: pool,
	}

	// Create processor with message handler
	mb.processor = NewProcessor(config, mb.handleBatch)

	return mb
}

// Start begins batch processing
func (mb *MessageBatch) Start(ctx context.Context) {
	mb.processor.Start(ctx)
}

// Stop stops batch processing
func (mb *MessageBatch) Stop() {
	mb.processor.Stop()
}

// Submit adds a message to the batch queue
func (mb *MessageBatch) Submit(msg MessageItem) error {
	return mb.processor.Submit(msg)
}

// handleBatch processes a batch of messages
func (mb *MessageBatch) handleBatch(ctx context.Context, items []interface{}) error {
	if len(items) == 0 {
		return nil
	}

	// Convert items to messages
	messages := make([]MessageItem, 0, len(items))
	for _, item := range items {
		if msg, ok := item.(MessageItem); ok {
			messages = append(messages, msg)
		}
	}

	if len(messages) == 0 {
		return nil
	}

	// Build bulk insert query
	query := `
		INSERT INTO messages (channel_id, user_id, content, message_type, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`

	// Use a transaction for batch insert
	tx, err := mb.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Prepare statement
	stmt, err := tx.Prepare(ctx, "batch_insert_messages", query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	// Execute batch inserts
	successCount := 0
	for _, msg := range messages {
		_, err := tx.Exec(ctx, stmt.Name,
			msg.ChannelID,
			msg.UserID,
			msg.Content,
			msg.MessageType,
			msg.IdempotencyKey,
			msg.CreatedAt,
		)
		if err != nil {
			log.Printf("Failed to insert message in batch: %v", err)
			continue
		}
		successCount++
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	log.Printf("Batch inserted %d/%d messages", successCount, len(messages))
	return nil
}

// Stats returns batch processing statistics
func (mb *MessageBatch) Stats() ProcessorStats {
	return mb.processor.Stats()
}
