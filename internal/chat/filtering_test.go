package chat

import (
	"context"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	redisclient "real-time-chat-system/internal/redis"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestMessageFilteringAccuracyProperty(t *testing.T) {
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
	properties.Property("Message filtering accuracy", prop.ForAll(
		func(numMessages int, filterType int) bool {
			// Create a fixed number of messages
			if numMessages < 3 || numMessages > 6 {
				return true
			}

			// Create messages in the database with known timestamps
			var createdMessages []Message

			for i := 0; i < numMessages; i++ {
				req := SendMessageRequest{
					ChannelID:      channelID,
					UserID:         userID,
					Content:        "filter test message " + string(rune(65+i)),
					IdempotencyKey: "filtering-test-" + string(rune(65+i)),
					MessageType:    "text",
				}

				message, err := repository.CreateMessage(ctx, req)
				if err != nil {
					t.Logf("Error creating message: %v", err)
					return false
				}
				createdMessages = append(createdMessages, *message)

				// Small delay to ensure different timestamps
				time.Sleep(time.Millisecond)
			}

			if len(createdMessages) < 3 {
				// Clean up and skip
				for _, msg := range createdMessages {
					pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
				}
				return true
			}

			// Test different filtering scenarios based on filterType
			switch filterType % 2 {
			case 0: // Test since timestamp filtering
				// Use the timestamp of the middle message
				middleIndex := len(createdMessages) / 2
				sinceTime := createdMessages[middleIndex].CreatedAt

				req := HistoryRequest{
					ChannelID: channelID,
					UserID:    userID,
					Since:     &sinceTime,
					Limit:     100,
				}

				filteredMessages, err := repository.GetMessageHistory(ctx, req)
				if err != nil {
					t.Logf("Error getting filtered messages: %v", err)
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}

				// Verify all returned messages are after the since timestamp
				for _, msg := range filteredMessages.Messages {
					if !msg.CreatedAt.After(sinceTime) {
						t.Logf("Message timestamp %v is not after since timestamp %v",
							msg.CreatedAt, sinceTime)
						// Clean up and fail
						for _, msg := range createdMessages {
							pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
						}
						return false
					}
				}

				// Verify no messages that should be included are missing
				expectedCount := 0
				for _, msg := range createdMessages {
					if msg.CreatedAt.After(sinceTime) {
						expectedCount++
					}
				}

				if len(filteredMessages.Messages) != expectedCount {
					t.Logf("Expected %d messages after timestamp, got %d",
						expectedCount, len(filteredMessages.Messages))
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}

			case 1: // Test since message ID filtering
				if len(createdMessages) < 2 {
					// Clean up and skip
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return true
				}

				// Use the first message as the since message
				sinceMessage := createdMessages[0]

				req := HistoryRequest{
					ChannelID: channelID,
					UserID:    userID,
					SinceID:   sinceMessage.ID,
					Limit:     100,
				}

				filteredMessages, err := repository.GetMessageHistory(ctx, req)
				if err != nil {
					t.Logf("Error getting filtered messages by ID: %v", err)
					// Clean up and fail
					for _, msg := range createdMessages {
						pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
					}
					return false
				}

				// Verify all returned messages are after the since message timestamp
				for _, msg := range filteredMessages.Messages {
					if !msg.CreatedAt.After(sinceMessage.CreatedAt) {
						t.Logf("Message timestamp %v is not after since message timestamp %v",
							msg.CreatedAt, sinceMessage.CreatedAt)
						// Clean up and fail
						for _, msg := range createdMessages {
							pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
						}
						return false
					}
				}

				// Verify the since message itself is not included
				for _, msg := range filteredMessages.Messages {
					if msg.ID == sinceMessage.ID {
						t.Logf("Since message %s should not be included in results", sinceMessage.ID)
						// Clean up and fail
						for _, msg := range createdMessages {
							pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
						}
						return false
					}
				}
			}

			// Clean up created messages for next iteration
			for _, msg := range createdMessages {
				pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", msg.ID)
			}

			return true
		},
		gen.IntRange(3, 6), // Number of messages between 3 and 6
		gen.IntRange(0, 1), // Filter type selector
	))

	// Run the property test with 30 iterations
	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
