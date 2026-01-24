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

func TestMessageIdempotencyProperty(t *testing.T) {
	cfg, _ := config.Load()
	cfg.Database.Database = "chatplatform_test"

	db, err := database.NewPostgresDB(&cfg.Database)
	if err != nil {
		t.Skip("Skipping property test - database not available: %v", err)
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	if err := db.InitSchema(ctx); err != nil {
		t.Skipf("Skipping property test - failed to initialize schema : %v", err)
		return
	}

	redisClient, err := redisclient.NewClient(&cfg.Redis)
	if err != nil {
		t.Skipf("Skipping property test - Redis not available: %v", err)
		return
	}
	defer redisClient.Close()

	repository := NewRepository(db)

	// Create a simple test setup
	pool := db.GetShardByChannelID("test")

	// Create test user
	var userID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (username) DO UPDATE SET username = EXCLUDED.username
		RETURNING id
	`, "testuser", "test@example.com", "hashedpassword").Scan(&userID)
	if err != nil {
		t.Skipf("Skipping property test - failed to create test user: %v", err)
		return
	}

	// Create test channel
	var channelID string
	err = pool.QueryRow(ctx, `
		INSERT INTO channels (name, type, created_by)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "testchannel", "public", userID).Scan(&channelID)
	if err != nil {
		// Try to get existing channel
		err = pool.QueryRow(ctx, "SELECT id FROM channels WHERE name = $1", "testchannel").Scan(&channelID)
		if err != nil {
			t.Skipf("Skipping property test - failed to create test channel: %v", err)
			return
		}
	}

	// Add user as channel member
	_, err = pool.Exec(ctx, `
		INSERT INTO channel_members (channel_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, user_id) DO NOTHING
	`, channelID, userID, "member")
	if err != nil {
		t.Skipf("Skipping property test - failed to add user to channel: %v", err)
		return
	}

	// Property test configuration
	properties := gopter.NewProperties(nil)
	properties.Property("Message idempotency preservation", prop.ForAll(
		func(content string, idempotencyKey string) bool {
			// Skip empty content or idempotency key
			if content == "" || idempotencyKey == "" {
				return true
			}

			req := SendMessageRequest{
				ChannelID:      channelID,
				UserID:         userID,
				Content:        content,
				IdempotencyKey: idempotencyKey,
				MessageType:    "text",
			}

			// Send the same message multiple times
			message1, err1 := repository.CreateMessage(ctx, req)
			message2, err2 := repository.CreateMessage(ctx, req)

			// All operations should succeed
			if err1 != nil || err2 != nil {
				t.Logf("Error creating messages: %v, %v", err1, err2)
				return false
			}

			// All messages should have the same ID (idempotency)
			if message1.ID != message2.ID {
				t.Logf("Messages have different IDs: %s, %s", message1.ID, message2.ID)
				return false
			}

			// Clean up the message for next iteration
			_, err := pool.Exec(ctx, "DELETE FROM messages WHERE id = $1", message1.ID)
			if err != nil {
				t.Logf("Error cleaning up message: %v", err)
			}

			return true
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 && len(s) < 100 }),
		gen.Identifier().SuchThat(func(s string) bool { return len(s) > 0 }),
	))

	// Run the property test with 10 iterations for quick testing
	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
