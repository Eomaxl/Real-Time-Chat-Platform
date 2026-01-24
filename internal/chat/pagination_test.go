package chat

import (
	"context"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	redisclient "real-time-chat-system/internal/redis"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestMessageHistoryPaginationConsistencyPropertySimple(t *testing.T) {
	// Setup test database and dependencies
	cfg, _ := config.Load()
	cfg.Database.Database = "chatplatform_test"

	db, err := database.NewPostgresDB(&cfg.Database)
	if err != nil {
		t.Skipf("Skipping property test - database not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	if err := db.InitSchema(ctx); err != nil {
		t.Skipf("Skipping property test - failed to initialize schema: %v", err)
		return
	}

	redisClient, err := redisclient.NewClient(&cfg.Redis)
	if err != nil {
		t.Skipf("Skipping property test - Redis not available: %v", err)
		return
	}
	defer redisClient.Close()

	repository := NewRepository(db)

	// Get existing test setup
	pool := db.GetShardByChannelID("test")

	var userID, channelID string
	err = pool.QueryRow(ctx, "SELECT id FROM users WHERE username = $1", "testuser").Scan(&userID)
	if err != nil {
		t.Skipf("Skipping property test - test user not found: %v", err)
		return
	}

	err = pool.QueryRow(ctx, "SELECT id FROM channels WHERE name = $1", "testchannel").Scan(&channelID)
	if err != nil {
		t.Skipf("Skipping property test - test channel not found: %v", err)
		return
	}

	// Property test configuration
	properties := gopter.NewProperties(nil)
	properties.Property("Message history pagination consistency", prop.ForAll(
		func(numMessages int) bool {
			// Create a fixed number of messages
			if numMessages < 4 || numMessages > 8 {
				return true
			}

			// Create messages in the database
			var createdMessages []Message
			for i := 0; i < numMessages; i++ {
				req := SendMessageRequest{
					ChannelID:      channelID,
					UserID:         userID,
					Content:        "test message " + string(rune(65+i)), // A, B, C, etc.
					IdempotencyKey: "pagination-simple-test-" + string(rune(65+i)),
					MessageType:    "text",
				}

				message, err := repository.CreateMessage(ctx, req)
				if err != nil {
					t.Logf("Error creating message: %v", err)
					return false
				}
				createdMessages = append(createdMessages, *message)
			}

			// Test pagination with page size 2
			pageSize := 2
			var allPaginatedMessages []Message
			var cursor string
			pageCount := 0
			maxPages := 5

			for pageCount < maxPages {
				req := HistoryRequest{
					ChannelID: channelID,
					UserID:    userID,
					Cursor:    cursor,
					Limit:     pageSize,
				}

				page, err := repository.GetMessageHistory(ctx, req)
				if err != nil {
					t.Logf("Error getting message history: %v", err)
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}

				// Add messages from this page
				allPaginatedMessages = append(allPaginatedMessages, page.Messages...)

				// Check if there are more pages
				if !page.HasMore || page.NextCursor == nil {
					break
				}

				cursor = *page.NextCursor
				pageCount++
			}

			// Verify no duplicates in paginated results
			messageIDs := make(map[string]bool)
			for _, msg := range allPaginatedMessages {
				if messageIDs[msg.ID] {
					t.Logf("Duplicate message ID found in pagination: %s", msg.ID)
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}
				messageIDs[msg.ID] = true
			}

			// Verify consistent ordering (newest first)
			for i := 1; i < len(allPaginatedMessages); i++ {
				if allPaginatedMessages[i-1].CreatedAt.Before(allPaginatedMessages[i].CreatedAt) {
					t.Logf("Messages not in consistent order: %v should be after %v",
						allPaginatedMessages[i-1].CreatedAt, allPaginatedMessages[i].CreatedAt)
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}
			}

			// Clean up created messages for next iteration
			for _, msg := range createdMessages {
				pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
			}

			return true
		},
		gen.IntRange(4, 8), // Number of messages between 4 and 8
	))

	// Run the property test with 20 iterations
	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// **Validates: Requirements 2.1, 2.2**
