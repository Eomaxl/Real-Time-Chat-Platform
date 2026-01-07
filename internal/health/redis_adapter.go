package health

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type RedisAdapter struct {
	client *redis.Client
}

func NewRedisAdapter(client *redis.Client) *RedisAdapter {
	return &RedisAdapter{client: client}
}

func (r *RedisAdapter) Health(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
