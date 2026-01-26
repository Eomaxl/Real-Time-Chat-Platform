package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter handles tenant-aware rate limiting
type RateLimiter struct {
	redis *redis.Client
}

// NewRateLimiter creates a new tenant-aware rate limiter
func NewRateLimiter(redis *redis.Client) *RateLimiter {
	return &RateLimiter{redis: redis}
}

// CheckRateLimit checks if a tenant has exceeded their rate limit
func (r *RateLimiter) CheckRateLimit(ctx context.Context, tenantID string, limits TenantLimits) (bool, error) {
	// Check API rate limit
	key := fmt.Sprintf("ratelimit:tenant:%s:api", tenantID)

	// If unlimited, allow
	if IsUnlimited(limits.APIRateLimit) {
		return true, nil
	}

	// Use sliding window rate limiting
	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Remove old entries
	_, err := r.redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.Unix())).Result()
	if err != nil {
		return false, fmt.Errorf("failed to clean old entries: %w", err)
	}

	// Count requests in current window
	count, err := r.redis.ZCard(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to count requests: %w", err)
	}

	// Check if limit exceeded
	if int(count) >= limits.APIRateLimit {
		return false, nil
	}

	// Add current request
	score := float64(now.Unix())
	member := fmt.Sprintf("%d:%d", now.Unix(), now.UnixNano())
	_, err = r.redis.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Result()
	if err != nil {
		return false, fmt.Errorf("failed to add request: %w", err)
	}

	// Set expiration on key
	r.redis.Expire(ctx, key, 2*time.Minute)

	return true, nil
}

// CheckMessageRateLimit checks if a tenant has exceeded their message rate limit
func (r *RateLimiter) CheckMessageRateLimit(ctx context.Context, tenantID string, limits TenantLimits) (bool, error) {
	key := fmt.Sprintf("ratelimit:tenant:%s:messages", tenantID)

	// If unlimited, allow
	if IsUnlimited(limits.MessageRateLimit) {
		return true, nil
	}

	// Use sliding window rate limiting
	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Remove old entries
	_, err := r.redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.Unix())).Result()
	if err != nil {
		return false, fmt.Errorf("failed to clean old entries: %w", err)
	}

	// Count messages in current window
	count, err := r.redis.ZCard(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to count messages: %w", err)
	}

	// Check if limit exceeded
	if int(count) >= limits.MessageRateLimit {
		return false, nil
	}

	// Add current message
	score := float64(now.Unix())
	member := fmt.Sprintf("%d:%d", now.Unix(), now.UnixNano())
	_, err = r.redis.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Result()
	if err != nil {
		return false, fmt.Errorf("failed to add message: %w", err)
	}

	// Set expiration on key
	r.redis.Expire(ctx, key, 2*time.Minute)

	return true, nil
}

// GetCurrentAPIRate returns the current API request rate for a tenant
func (r *RateLimiter) GetCurrentAPIRate(ctx context.Context, tenantID string) (int, error) {
	key := fmt.Sprintf("ratelimit:tenant:%s:api", tenantID)

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Remove old entries
	_, err := r.redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.Unix())).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to clean old entries: %w", err)
	}

	// Count requests in current window
	count, err := r.redis.ZCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count requests: %w", err)
	}

	return int(count), nil
}

// GetCurrentMessageRate returns the current message rate for a tenant
func (r *RateLimiter) GetCurrentMessageRate(ctx context.Context, tenantID string) (int, error) {
	key := fmt.Sprintf("ratelimit:tenant:%s:messages", tenantID)

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Remove old entries
	_, err := r.redis.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.Unix())).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to clean old entries: %w", err)
	}

	// Count messages in current window
	count, err := r.redis.ZCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}

	return int(count), nil
}

// ResetRateLimit resets the rate limit for a tenant (admin operation)
func (r *RateLimiter) ResetRateLimit(ctx context.Context, tenantID string) error {
	apiKey := fmt.Sprintf("ratelimit:tenant:%s:api", tenantID)
	messageKey := fmt.Sprintf("ratelimit:tenant:%s:messages", tenantID)

	_, err := r.redis.Del(ctx, apiKey, messageKey).Result()
	if err != nil {
		return fmt.Errorf("failed to reset rate limit: %w", err)
	}

	return nil
}
