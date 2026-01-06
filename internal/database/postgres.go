package database

import (
	"context"
	"fmt"
	"hash/fnv"
	"real-time-chat-system/internal/config"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDB struct {
	pools  []*pgxpool.Pool
	shards int
	config *config.DatabaseConfig
}

type ShardKey string

// NewPostgreDB create a new PostgreSQL database connection with connection pooling
func NewPostgreDB(cfg *config.DatabaseConfig) (*PostgresDB, error) {
	// For basic setup, we are using single shard
	// For production, this would be configured with multiple database instance
	shards := 3
	pools := make([]*pgxpool.Pool, shards)

	for i := 0; i < shards; i++ {
		poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL())
		if err != nil {
			return nil, fmt.Errorf("failed to parse the database config %w", err)
		}

		// Configure connection pool
		poolConfig.MaxConns = int32(cfg.MaxConnections)
		poolConfig.MinConns = int32(cfg.MaxIdleConns)
		poolConfig.MaxConnLifetime = cfg.GetConnMaxLifetime()
		poolConfig.MaxConnIdleTime = 30 * time.Minute

		pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create connection pool : %w", err)
		}

		//  Test connection
		if err := pool.Ping(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}

		pools[i] = pool
	}

	return &PostgresDB{
		pools:  pools,
		shards: shards,
		config: cfg,
	}, nil
}

// GetShard returns the appropriate database pool for the given shard key
func (db *PostgresDB) GetShard(key ShardKey) *pgxpool.Pool {
	if db.shards == 1 {
		return db.pools[0]
	}

	// Use FNV hash for consistent sharding
	h := fnv.New32a()
	h.Write([]byte(key))
	shardIndex := int(h.Sum32()) % db.shards

	return db.pools[shardIndex]
}

// GetShardByChannelID returns the appropriate shard for the channel ID
func (db *PostgresDB) GetShardByChannelID(channelID string) *pgxpool.Pool {
	return db.GetShard(ShardKey(channelID))
}

// GetShardByUserID returns the appropriate shard for the user ID
func (db *PostgresDB) GetShardByUserID(userID string) *pgxpool.Pool {
	return db.GetShard(ShardKey(userID))
}

// Close closes all the database connections
func (db *PostgresDB) Close() {
	for _, pool := range db.pools {
		pool.Close()
	}
}

// Health checks the health of all database connections
func (db *PostgresDB) Health(ctx context.Context) error {
	for i, pool := range db.pools {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("shard %d health check failed : %w", i, err)
		}
	}
	return nil
}

// InitSchema initialize the database schema
func (db *PostgresDB) InitSchema(ctx context.Context) error {
	// Create tables on all shards
	for i, pool := range db.pools {
		if err := db.createTables(ctx, pool); err != nil {
			return fmt.Errorf("failed to create tables on shard %d: %w", i, err)
		}
	}
	return nil
}

// createTables create the necessary tables
func (db *PostgresDB) createTables(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXIST "uuid-ossp";`,
		`CREATE TABLE IF NOT EXIST users (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			username VARCHAR(255) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXIST channels (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			name VARCHAR(255) NOT NULL,
			type VARCHAR(255) NOT NULL,
			created_by UUID NOT NULL REFERENCES users(id),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS channel_members (
			channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			role VARCHAR(50) DEFAULT 'member',
			PRIMARY KEY (channel_id, user_id)
		);`,

		`CREATE TABLE IF NOT EXISTS messages (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			channel_id UUID NOT REFERENENCES channels(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id),
			content TEXT NOT NULL,
			message_type VARCHAR(50) NOT NULL DEFAULT 'text',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			idempotency_key VARCHAR(255) UNIQUE
		);`,

		`CREATE TABLE IF NOT EXISTS call_sessions (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
			created_by UUID NOT NULL REFERENCES users(id),
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			call_type VARHCAR(50) NOT NULL DEFAULT 'audio',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			ended_at TIMESTAMP WITH TIME ZONE
		);`,

		`CREATE TABLE IF NOT EXISTS call_participants (
			call_id UUID NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id),
			joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			left_at TIMESTAMP WITH TIME ZONE,
			signalling_state VARCHAR(50) DEFAULT 'joining',
			PRIMARY KEY (call_id, user_id)
		);`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_idempotency ON  messages(idempotency_key) WHERE idempotency_key IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_channel_members_user ON channel_members(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_call_sessions_channel ON call_sessions(channel_id);`,
		`CREATE INDEX IF NOT EXISTS idx_call_participants_user ON call_participants(user_id);`,
	}

	for _, query := range queries {
		if _, err := pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query : %s , error: %w", query, err)
		}
	}

	return nil
}
