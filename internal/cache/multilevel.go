package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	redisclient "real-time-chat-system/internal/redis"
)

// MultiLevelCache implements a multi-level caching strategy with L1 (in-memory) and L2 (Redis)
type MultiLevelCache struct {
	l1Cache *LRUCache
	redis   *redisclient.Client
	prefix  string

	// Default TTLs
	l1TTL time.Duration
	l2TTL time.Duration
}

// MultiLevelCacheConfig configures the multi-level cache
type MultiLevelCacheConfig struct {
	L1Capacity int
	L1TTL      time.Duration
	L2TTL      time.Duration
	Prefix     string
}

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache(redis *redisclient.Client, config MultiLevelCacheConfig) *MultiLevelCache {
	if config.L1Capacity <= 0 {
		config.L1Capacity = 1000
	}
	if config.L1TTL <= 0 {
		config.L1TTL = 5 * time.Minute
	}
	if config.L2TTL <= 0 {
		config.L2TTL = 30 * time.Minute
	}
	if config.Prefix == "" {
		config.Prefix = "cache"
	}

	return &MultiLevelCache{
		l1Cache: NewLRUCache(config.L1Capacity),
		redis:   redis,
		prefix:  config.Prefix,
		l1TTL:   config.L1TTL,
		l2TTL:   config.L2TTL,
	}
}

// Get retrieves a value from the cache, checking L1 first, then L2
func (c *MultiLevelCache) Get(ctx context.Context, key string) (interface{}, bool) {
	// Try L1 cache first
	if value, ok := c.l1Cache.Get(key); ok {
		return value, true
	}

	// Try L2 cache (Redis)
	redisKey := c.makeRedisKey(key)
	data, err := c.redis.Get(ctx, redisKey)
	if err != nil {
		return nil, false
	}

	// Deserialize from Redis
	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return nil, false
	}

	// Populate L1 cache
	c.l1Cache.Set(key, value, c.l1TTL)

	return value, true
}

// GetString retrieves a string value from the cache
func (c *MultiLevelCache) GetString(ctx context.Context, key string) (string, bool) {
	value, ok := c.Get(ctx, key)
	if !ok {
		return "", false
	}

	str, ok := value.(string)
	return str, ok
}

// GetBytes retrieves a byte slice from the cache
func (c *MultiLevelCache) GetBytes(ctx context.Context, key string) ([]byte, bool) {
	// Try L1 cache first
	if value, ok := c.l1Cache.Get(key); ok {
		if bytes, ok := value.([]byte); ok {
			return bytes, true
		}
	}

	// Try L2 cache (Redis) - return raw bytes
	redisKey := c.makeRedisKey(key)
	data, err := c.redis.Get(ctx, redisKey)
	if err != nil {
		return nil, false
	}

	bytes := []byte(data)

	// Populate L1 cache
	c.l1Cache.Set(key, bytes, c.l1TTL)

	return bytes, true
}

// Set stores a value in both L1 and L2 caches
func (c *MultiLevelCache) Set(ctx context.Context, key string, value interface{}) error {
	return c.SetWithTTL(ctx, key, value, c.l1TTL, c.l2TTL)
}

// SetWithTTL stores a value with custom TTLs for each cache level
func (c *MultiLevelCache) SetWithTTL(ctx context.Context, key string, value interface{}, l1TTL, l2TTL time.Duration) error {
	// Store in L1 cache
	c.l1Cache.Set(key, value, l1TTL)

	// Serialize for Redis
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	// Store in L2 cache (Redis)
	redisKey := c.makeRedisKey(key)
	if err := c.redis.Set(ctx, redisKey, string(data), l2TTL); err != nil {
		return fmt.Errorf("failed to set Redis cache: %w", err)
	}

	return nil
}

// SetString stores a string value in both caches
func (c *MultiLevelCache) SetString(ctx context.Context, key, value string) error {
	return c.Set(ctx, key, value)
}

// SetBytes stores a byte slice in both caches
func (c *MultiLevelCache) SetBytes(ctx context.Context, key string, value []byte) error {
	// Store in L1 cache
	c.l1Cache.Set(key, value, c.l1TTL)

	// Store in L2 cache (Redis) - store raw bytes
	redisKey := c.makeRedisKey(key)
	if err := c.redis.Set(ctx, redisKey, string(value), c.l2TTL); err != nil {
		return fmt.Errorf("failed to set Redis cache: %w", err)
	}

	return nil
}

// Delete removes a value from both cache levels
func (c *MultiLevelCache) Delete(ctx context.Context, key string) error {
	// Remove from L1
	c.l1Cache.Delete(key)

	// Remove from L2 (Redis)
	redisKey := c.makeRedisKey(key)
	if err := c.redis.Del(ctx, redisKey); err != nil {
		return fmt.Errorf("failed to delete from Redis: %w", err)
	}

	return nil
}

// Invalidate removes a value from both cache levels (alias for Delete)
func (c *MultiLevelCache) Invalidate(ctx context.Context, key string) error {
	return c.Delete(ctx, key)
}

// InvalidatePattern removes all keys matching a pattern from both cache levels
func (c *MultiLevelCache) InvalidatePattern(ctx context.Context, pattern string) error {
	// Clear matching keys from L1 cache
	// Note: LRU cache doesn't support pattern matching, so we clear all
	// In production, you might want to track keys by pattern
	c.l1Cache.Clear()

	// Delete matching keys from Redis using SCAN
	redisPattern := c.makeRedisKey(pattern)

	// Use SCAN to find matching keys
	iter := c.redis.GetClient().Scan(ctx, 0, redisPattern, 100).Iterator()
	var keysToDelete []string

	for iter.Next(ctx) {
		keysToDelete = append(keysToDelete, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan Redis keys: %w", err)
	}

	// Delete all matching keys
	if len(keysToDelete) > 0 {
		if err := c.redis.Del(ctx, keysToDelete...); err != nil {
			return fmt.Errorf("failed to delete pattern from Redis: %w", err)
		}
	}

	return nil
}

// Warm preloads data into the cache
func (c *MultiLevelCache) Warm(ctx context.Context, key string, loader func(ctx context.Context) (interface{}, error)) error {
	// Check if already cached
	if _, ok := c.Get(ctx, key); ok {
		return nil // Already cached
	}

	// Load data
	value, err := loader(ctx)
	if err != nil {
		return fmt.Errorf("failed to load data for warming: %w", err)
	}

	// Store in cache
	return c.Set(ctx, key, value)
}

// GetOrLoad retrieves a value from cache or loads it using the provided function
func (c *MultiLevelCache) GetOrLoad(ctx context.Context, key string, loader func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	// Try to get from cache
	if value, ok := c.Get(ctx, key); ok {
		return value, nil
	}

	// Load data
	value, err := loader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load data: %w", err)
	}

	// Store in cache
	if err := c.Set(ctx, key, value); err != nil {
		// Log error but return the loaded value
		fmt.Printf("Failed to cache loaded value: %v\n", err)
	}

	return value, nil
}

// Stats returns statistics for both cache levels
func (c *MultiLevelCache) Stats() MultiLevelCacheStats {
	return MultiLevelCacheStats{
		L1: c.l1Cache.Stats(),
	}
}

// CleanupExpired removes expired entries from L1 cache
func (c *MultiLevelCache) CleanupExpired() int {
	return c.l1Cache.CleanupExpired()
}

// makeRedisKey creates a Redis key with the configured prefix
func (c *MultiLevelCache) makeRedisKey(key string) string {
	return fmt.Sprintf("%s:%s", c.prefix, key)
}

// MultiLevelCacheStats represents statistics for multi-level cache
type MultiLevelCacheStats struct {
	L1 CacheStats
}
