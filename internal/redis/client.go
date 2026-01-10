package redis

import (
	"context"
	"fmt"
	"real-time-chat-system/internal/config"
	"time"

	"github.com/redis/go-redis/v9"
	goredis "github.com/redis/go-redis/v9"
)

// Client represents a Redis Client with clustering support
type Client struct {
	client goredis.UniversalClient
	config *config.RedisConfig
}

// NewClient creates a new Redis Client
func NewClient(cfg *config.RedisConfig) (*Client, error) {
	var client goredis.UniversalClient

	// Configure Redis Client options
	opts := &redis.UniversalOptions{
		Addrs:        cfg.Addresses,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	}

	// Create client (will automatically use cluster client if multiple addresses)
	client = redis.NewUniversalClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Client{client: client, config: cfg}, nil
}

// GetClient returns the underlying Redis Client
func (c *Client) GetClient() goredis.UniversalClient {
	return c.client
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.Close()
}

// Health checks the health of the redis connection
func (c *Client) Health(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Presence-related operations
func (c *Client) SetPresence(ctx context.Context, userID string, status string, ttl time.Duration) error {
	key := fmt.Sprintf("presence:user:%s", userID)
	return c.client.Set(ctx, key, status, ttl).Err()
}

func (c *Client) GetPresence(ctx context.Context, userID string) (string, error) {
	key := fmt.Sprintf("presence:user:%s", userID)
	return c.client.Get(ctx, key).Result()
}

func (c *Client) DeletePresence(ctx context.Context, userID string) error {
	key := fmt.Sprintf("presence:user:%s", userID)
	return c.client.Del(ctx, key).Err()
}

// Channel presence operations
func (c *Client) AddToChannelPresence(ctx context.Context, channelID, userID string) error {
	key := fmt.Sprintf("presence:channel:%s", channelID)
	score := float64(time.Now().Unix())
	return c.client.ZAdd(ctx, key, redis.Z{Score: score, Member: userID}).Err()
}

func (c *Client) RemoveFromChannelPresence(ctx context.Context, channelID, userID string) error {
	key := fmt.Sprintf("presence:channel:%s", channelID)
	return c.client.ZRem(ctx, key, userID).Err()
}

func (c *Client) GetChannelPresence(ctx context.Context, channelID string) ([]string, error) {
	key := fmt.Sprintf("presence:channel:%s", channelID)
	return c.client.ZRange(ctx, key, 0, -1).Result()
}

// Pub/Sub operations
func (c *Client) Publish(ctx context.Context, channel string, message interface{}) error {
	return c.client.Publish(ctx, channel, message).Err()
}

func (c *Client) Subscribe(ctx context.Context, channles ...string) *redis.PubSub {
	return c.client.Subscribe(ctx, channles...)
}

// Rate limiting operations
func (c *Client) IncrementRateLimit(ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := c.client.Pipeline()

	// Increment counter
	incr := pipe.Incr(ctx, key)

	// Set expiration if this is the first increment
	pipe.Expire(ctx, key, window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	return incr.Val(), nil
}

// Caching operations
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(ctx, key, value, expiration).Err()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.client.Exists(ctx, keys...).Result()
}

// Hash operations for complex data structures
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.client.HSet(ctx, key, values...).Err()
}

func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	return c.client.HGet(ctx, key, field).Result()
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

func (c *Client) HDel(ctx context.Context, key string, fields ...string) error {
	return c.client.HDel(ctx, key, fields...).Err()
}

// List operations for message queues
func (c *Client) LPush(ctx context.Context, key string, values ...interface{}) error {
	return c.client.LPush(ctx, key, values...).Err()
}

func (c *Client) RPop(ctx context.Context, key string) (string, error) {
	return c.client.RPop(ctx, key).Result()
}

func (c *Client) BRPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	return c.client.BRPop(ctx, timeout, keys...).Result()
}
