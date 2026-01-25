package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter provides distributed rate limiting using Redis
type Limiter struct {
	redis redis.UniversalClient
}

// NewLimiter creates a new rate limiter
func NewLimiter(redisClient redis.UniversalClient) *Limiter {
	return &Limiter{
		redis: redisClient,
	}
}

// Config holds rate limiting configuration
type Config struct {
	MaxRequests int           // Maximum requests allowed
	Window      time.Duration // Time window for rate limiting
	BurstSize   int           // Burst allowance (extra requests allowed)
}

// Result contains the result of a rate limit check
type Result struct {
	Allowed      bool
	Remaining    int
	RetryAfter   time.Duration
	ResetAt      time.Time
	CurrentCount int
	Limit        int
}

// Allow checks if a request is allowed under the rate limit
// Uses sliding window algorithm with Redis
func (l *Limiter) Allow(ctx context.Context, key string, cfg Config) (*Result, error) {
	now := time.Now()
	windowStart := now.Add(-cfg.Window)

	// Redis key for this rate limit
	redisKey := fmt.Sprintf("ratelimit:%s", key)

	// Use Redis sorted set with timestamps as scores
	pipe := l.redis.Pipeline()

	// Remove old entries outside the window
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", windowStart.UnixNano()))

	// Count current requests in window
	countCmd := pipe.ZCard(ctx, redisKey)

	// Add current request timestamp
	pipe.ZAdd(ctx, redisKey, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: fmt.Sprintf("%d", now.UnixNano()),
	})

	// Set expiration on the key
	pipe.Expire(ctx, redisKey, cfg.Window+time.Minute)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute rate limit check: %w", err)
	}

	currentCount := int(countCmd.Val())
	limit := cfg.MaxRequests + cfg.BurstSize

	result := &Result{
		Allowed:      currentCount < limit,
		Remaining:    max(0, limit-currentCount-1),
		CurrentCount: currentCount,
		Limit:        limit,
		ResetAt:      now.Add(cfg.Window),
	}

	if !result.Allowed {
		result.RetryAfter = cfg.Window
	}

	return result, nil
}

// AllowN checks if N requests are allowed under the rate limit
func (l *Limiter) AllowN(ctx context.Context, key string, cfg Config, n int) (*Result, error) {
	now := time.Now()
	windowStart := now.Add(-cfg.Window)

	redisKey := fmt.Sprintf("ratelimit:%s", key)

	pipe := l.redis.Pipeline()

	// Remove old entries
	pipe.ZRemRangeByScore(ctx, redisKey, "0", fmt.Sprintf("%d", windowStart.UnixNano()))

	// Count current requests
	countCmd := pipe.ZCard(ctx, redisKey)

	// Add N requests
	members := make([]redis.Z, n)
	for i := 0; i < n; i++ {
		timestamp := now.Add(time.Duration(i) * time.Nanosecond)
		members[i] = redis.Z{
			Score:  float64(timestamp.UnixNano()),
			Member: fmt.Sprintf("%d-%d", timestamp.UnixNano(), i),
		}
	}
	pipe.ZAdd(ctx, redisKey, members...)

	// Set expiration
	pipe.Expire(ctx, redisKey, cfg.Window+time.Minute)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute rate limit check: %w", err)
	}

	currentCount := int(countCmd.Val())
	limit := cfg.MaxRequests + cfg.BurstSize

	result := &Result{
		Allowed:      currentCount+n <= limit,
		Remaining:    max(0, limit-currentCount-n),
		CurrentCount: currentCount,
		Limit:        limit,
		ResetAt:      now.Add(cfg.Window),
	}

	if !result.Allowed {
		result.RetryAfter = cfg.Window
	}

	return result, nil
}

// Reset resets the rate limit for a key
func (l *Limiter) Reset(ctx context.Context, key string) error {
	redisKey := fmt.Sprintf("ratelimit:%s", key)
	return l.redis.Del(ctx, redisKey).Err()
}

// GetStatus returns the current status of a rate limit without incrementing
func (l *Limiter) GetStatus(ctx context.Context, key string, cfg Config) (*Result, error) {
	now := time.Now()
	windowStart := now.Add(-cfg.Window)

	redisKey := fmt.Sprintf("ratelimit:%s", key)

	// Count requests in current window
	count, err := l.redis.ZCount(ctx, redisKey, fmt.Sprintf("%d", windowStart.UnixNano()), "+inf").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get rate limit status: %w", err)
	}

	currentCount := int(count)
	limit := cfg.MaxRequests + cfg.BurstSize

	result := &Result{
		Allowed:      currentCount < limit,
		Remaining:    max(0, limit-currentCount),
		CurrentCount: currentCount,
		Limit:        limit,
		ResetAt:      now.Add(cfg.Window),
	}

	if !result.Allowed {
		result.RetryAfter = cfg.Window
	}

	return result, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
