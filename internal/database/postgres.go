package database

import (
	"context"
	"fmt"
	"hash/fnv"
	"real-time-chat-system/internal/config"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDB represents a PostgreSQL database connection with sharding support
type PostgresDB struct {
	pools          []*pgxpool.Pool
	readPools      [][]*pgxpool.Pool // Read replicas per shard
	shards         int
	config         *config.DatabaseConfig
	hashRing       *ConsistentHashRing
	poolMetrics    *PoolMetrics
	poolManager    *PoolManager
	replicaManager *ReplicaManager
	mu             sync.RWMutex
}

// ShardKey represents a key used for sharding
type ShardKey string

// ConsistentHashRing implements consistent hashing for shard distribution
type ConsistentHashRing struct {
	nodes        []uint32
	nodeMap      map[uint32]int
	virtualNodes int
	mu           sync.RWMutex
}

// PoolMetrics tracks connection pool metrics
type PoolMetrics struct {
	mu            sync.RWMutex
	totalQueries  map[int]int64
	failedQueries map[int]int64
	readQueries   map[int]int64
	writeQueries  map[int]int64
}

// QueryType represents the type of database query
type QueryType int

const (
	QueryTypeRead QueryType = iota
	QueryTypeWrite
)

// NewConsistentHashRing creates a new consistent hash ring
func NewConsistentHashRing(shardCount int, virtualNodes int) *ConsistentHashRing {
	ring := &ConsistentHashRing{
		nodeMap:      make(map[uint32]int),
		virtualNodes: virtualNodes,
	}

	// Add virtual nodes for each shard
	for i := 0; i < shardCount; i++ {
		ring.addNode(i)
	}

	// Sort nodes for binary search
	sort.Slice(ring.nodes, func(i, j int) bool {
		return ring.nodes[i] < ring.nodes[j]
	})

	return ring
}

// addNode adds a shard with virtual nodes to the ring
func (r *ConsistentHashRing) addNode(shardID int) {
	for i := 0; i < r.virtualNodes; i++ {
		hash := r.hash(fmt.Sprintf("shard-%d-vnode-%d", shardID, i))
		r.nodes = append(r.nodes, hash)
		r.nodeMap[hash] = shardID
	}
}

// hash computes FNV-1a hash
func (r *ConsistentHashRing) hash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// GetShard returns the shard ID for a given key
func (r *ConsistentHashRing) GetShard(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.nodes) == 0 {
		return 0
	}

	hash := r.hash(key)

	// Binary search for the first node >= hash
	idx := sort.Search(len(r.nodes), func(i int) bool {
		return r.nodes[i] >= hash
	})

	// Wrap around if necessary
	if idx == len(r.nodes) {
		idx = 0
	}

	return r.nodeMap[r.nodes[idx]]
}

// NewPoolMetrics creates a new pool metrics tracker
func NewPoolMetrics(shardCount int) *PoolMetrics {
	return &PoolMetrics{
		totalQueries:  make(map[int]int64),
		failedQueries: make(map[int]int64),
		readQueries:   make(map[int]int64),
		writeQueries:  make(map[int]int64),
	}
}

// NewPostgresDB creates a new PostgreSQL database connection with connection pooling
func NewPostgresDB(cfg *config.DatabaseConfig) (*PostgresDB, error) {
	// Determine number of shards
	shardCount := len(cfg.Shards)
	if shardCount == 0 {
		shardCount = 1 // Default to single shard
	}

	pools := make([]*pgxpool.Pool, shardCount)
	readPools := make([][]*pgxpool.Pool, shardCount)

	// Create connection pools for each shard
	for i := 0; i < shardCount; i++ {
		var connString string

		if len(cfg.Shards) > 0 {
			// Use shard-specific configuration
			connString = cfg.ShardURL(cfg.Shards[i])
		} else {
			// Use default configuration
			connString = cfg.DatabaseURL()
		}

		poolConfig, err := pgxpool.ParseConfig(connString)
		if err != nil {
			return nil, fmt.Errorf("failed to parse database config for shard %d: %w", i, err)
		}

		// Configure connection pool with optimized settings
		poolConfig.MaxConns = int32(cfg.MaxConnections)
		poolConfig.MinConns = int32(cfg.MaxIdleConns)
		poolConfig.MaxConnLifetime = cfg.GetConnMaxLifetime()
		poolConfig.MaxConnIdleTime = 30 * time.Minute
		poolConfig.HealthCheckPeriod = 1 * time.Minute

		// Optimized settings for high concurrency
		// These settings help prevent connection exhaustion and improve performance

		pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create connection pool for shard %d: %w", i, err)
		}

		// Test connection
		if err := pool.Ping(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to ping database shard %d: %w", i, err)
		}

		pools[i] = pool

		// Create read replica pools for this shard
		readPools[i] = make([]*pgxpool.Pool, 0)
		for _, replica := range cfg.ReadReplicas {
			if replica.ShardID == i {
				replicaPool, err := createReplicaPool(cfg, replica)
				if err != nil {
					return nil, fmt.Errorf("failed to create replica pool for shard %d: %w", i, err)
				}
				readPools[i] = append(readPools[i], replicaPool)
			}
		}
	}

	// Create consistent hash ring with 150 virtual nodes per shard
	hashRing := NewConsistentHashRing(shardCount, 150)

	db := &PostgresDB{
		pools:       pools,
		readPools:   readPools,
		shards:      shardCount,
		config:      cfg,
		hashRing:    hashRing,
		poolMetrics: NewPoolMetrics(shardCount),
	}

	// Create and start pool manager for health monitoring
	poolManager := NewPoolManager(db, 30*time.Second)
	db.poolManager = poolManager

	// Create replica manager for read replica management
	replicaManager := NewReplicaManager(db, 5*time.Second) // Max 5 second replication lag
	db.replicaManager = replicaManager

	// Start health monitoring in background
	go poolManager.Start(context.Background())

	// Start replication lag monitoring if replicas exist
	if len(cfg.ReadReplicas) > 0 {
		go replicaManager.MonitorReplicationLag(context.Background(), 10*time.Second)
	}

	return db, nil
}

// createReplicaPool creates a connection pool for a read replica
func createReplicaPool(cfg *config.DatabaseConfig, replica config.ReplicaConfig) (*pgxpool.Pool, error) {
	connString := cfg.ReplicaURL(replica)

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse replica config: %w", err)
	}

	// Configure connection pool for read replicas
	poolConfig.MaxConns = int32(cfg.MaxConnections)
	poolConfig.MinConns = int32(cfg.MaxIdleConns)
	poolConfig.MaxConnLifetime = cfg.GetConnMaxLifetime()
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create replica pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping replica: %w", err)
	}

	return pool, nil
}

// GetShard returns the appropriate database pool for the given shard key
func (db *PostgresDB) GetShard(key ShardKey) *pgxpool.Pool {
	return db.GetShardForWrite(key)
}

// GetShardForWrite returns the write pool for the given shard key
func (db *PostgresDB) GetShardForWrite(key ShardKey) *pgxpool.Pool {
	if db.shards == 1 {
		db.recordQuery(0, QueryTypeWrite)
		return db.pools[0]
	}

	shardIndex := db.hashRing.GetShard(string(key))
	db.recordQuery(shardIndex, QueryTypeWrite)
	return db.pools[shardIndex]
}

// GetShardForRead returns a read pool (replica if available) for the given shard key
func (db *PostgresDB) GetShardForRead(key ShardKey) *pgxpool.Pool {
	if db.shards == 1 {
		db.recordQuery(0, QueryTypeRead)
		// Use replica manager if available
		if db.replicaManager != nil && len(db.readPools[0]) > 0 {
			return db.replicaManager.SelectReplica(0)
		}
		return db.pools[0]
	}

	shardIndex := db.hashRing.GetShard(string(key))
	db.recordQuery(shardIndex, QueryTypeRead)

	// Use replica manager if available
	if db.replicaManager != nil && len(db.readPools[shardIndex]) > 0 {
		return db.replicaManager.SelectReplica(shardIndex)
	}

	// Fall back to primary if no replicas
	return db.pools[shardIndex]
}

// GetShardForReadWithConsistency returns a read pool with consistency preference
func (db *PostgresDB) GetShardForReadWithConsistency(key ShardKey, requireStrongConsistency bool) *pgxpool.Pool {
	if db.shards == 1 {
		db.recordQuery(0, QueryTypeRead)
		if db.replicaManager != nil {
			return db.replicaManager.SelectReplicaWithFallback(0, requireStrongConsistency)
		}
		return db.pools[0]
	}

	shardIndex := db.hashRing.GetShard(string(key))
	db.recordQuery(shardIndex, QueryTypeRead)

	if db.replicaManager != nil {
		return db.replicaManager.SelectReplicaWithFallback(shardIndex, requireStrongConsistency)
	}

	return db.pools[shardIndex]
}

// selectReadReplica selects a read replica using round-robin with health checks
func (db *PostgresDB) selectReadReplica(shardIndex int) *pgxpool.Pool {
	replicas := db.readPools[shardIndex]
	if len(replicas) == 0 {
		return db.pools[shardIndex]
	}

	// If pool manager is available, use only healthy replicas
	if db.poolManager != nil {
		healthyReplicas := db.poolManager.GetHealthyReplicas(shardIndex)
		if len(healthyReplicas) > 0 {
			// Simple round-robin selection based on current time
			idx := int(time.Now().UnixNano()) % len(healthyReplicas)
			return healthyReplicas[idx]
		}
		// Fall back to primary if no healthy replicas
		return db.pools[shardIndex]
	}

	// Simple round-robin selection based on current time
	idx := int(time.Now().UnixNano()) % len(replicas)
	return replicas[idx]
}

// GetAllShards returns all shard pools for cross-shard queries
func (db *PostgresDB) GetAllShards() []*pgxpool.Pool {
	return db.pools
}

// GetShardCount returns the number of shards
func (db *PostgresDB) GetShardCount() int {
	return db.shards
}

// recordQuery records query metrics
func (db *PostgresDB) recordQuery(shardIndex int, queryType QueryType) {
	db.poolMetrics.mu.Lock()
	defer db.poolMetrics.mu.Unlock()

	db.poolMetrics.totalQueries[shardIndex]++

	if queryType == QueryTypeRead {
		db.poolMetrics.readQueries[shardIndex]++
	} else {
		db.poolMetrics.writeQueries[shardIndex]++
	}
}

// RecordFailedQuery records a failed query
func (db *PostgresDB) RecordFailedQuery(shardIndex int) {
	db.poolMetrics.mu.Lock()
	defer db.poolMetrics.mu.Unlock()

	db.poolMetrics.failedQueries[shardIndex]++
}

// GetMetrics returns current pool metrics
func (db *PostgresDB) GetMetrics() map[string]interface{} {
	db.poolMetrics.mu.RLock()
	defer db.poolMetrics.mu.RUnlock()

	metrics := make(map[string]interface{})
	metrics["shard_count"] = db.shards
	metrics["total_queries"] = db.poolMetrics.totalQueries
	metrics["failed_queries"] = db.poolMetrics.failedQueries
	metrics["read_queries"] = db.poolMetrics.readQueries
	metrics["write_queries"] = db.poolMetrics.writeQueries

	// Add pool stats from pool manager if available
	if db.poolManager != nil {
		poolStats := db.poolManager.GetPoolStats()
		metrics["pool_stats"] = poolStats
	} else {
		// Add basic pool stats for each shard
		poolStats := make([]map[string]interface{}, db.shards)
		for i := 0; i < db.shards; i++ {
			stat := db.pools[i].Stat()
			poolStats[i] = map[string]interface{}{
				"shard_id":         i,
				"acquired_conns":   stat.AcquiredConns(),
				"idle_conns":       stat.IdleConns(),
				"total_conns":      stat.TotalConns(),
				"max_conns":        stat.MaxConns(),
				"acquire_count":    stat.AcquireCount(),
				"acquire_duration": stat.AcquireDuration(),
				"empty_acquire":    stat.EmptyAcquireCount(),
				"canceled_acquire": stat.CanceledAcquireCount(),
			}
		}
		metrics["pool_stats"] = poolStats
	}

	// Add replication stats if replica manager is available
	if db.replicaManager != nil {
		replicationStats := db.replicaManager.GetReplicationStats()
		metrics["replication_stats"] = replicationStats
	}

	return metrics
}

// GetShardByChannelID returns the appropriate shard for a channel ID
func (db *PostgresDB) GetShardByChannelID(channelID string) *pgxpool.Pool {
	return db.GetShardForWrite(ShardKey(channelID))
}

// GetShardByChannelIDForRead returns the appropriate read shard for a channel ID
func (db *PostgresDB) GetShardByChannelIDForRead(channelID string) *pgxpool.Pool {
	return db.GetShardForRead(ShardKey(channelID))
}

// GetShardByChannelIDForReadWithConsistency returns read shard with consistency preference
func (db *PostgresDB) GetShardByChannelIDForReadWithConsistency(channelID string, requireStrongConsistency bool) *pgxpool.Pool {
	return db.GetShardForReadWithConsistency(ShardKey(channelID), requireStrongConsistency)
}

// GetShardByUserID returns the appropriate shard for a user ID
func (db *PostgresDB) GetShardByUserID(userID string) *pgxpool.Pool {
	return db.GetShardForWrite(ShardKey(userID))
}

// GetShardByUserIDForRead returns the appropriate read shard for a user ID
func (db *PostgresDB) GetShardByUserIDForRead(userID string) *pgxpool.Pool {
	return db.GetShardForRead(ShardKey(userID))
}

// GetShardByUserIDForReadWithConsistency returns read shard with consistency preference
func (db *PostgresDB) GetShardByUserIDForReadWithConsistency(userID string, requireStrongConsistency bool) *pgxpool.Pool {
	return db.GetShardForReadWithConsistency(ShardKey(userID), requireStrongConsistency)
}

// ExecuteOnAllShards executes a query on all shards (for cross-shard operations)
func (db *PostgresDB) ExecuteOnAllShards(ctx context.Context, query string, args ...interface{}) error {
	for i, pool := range db.pools {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			db.RecordFailedQuery(i)
			return fmt.Errorf("failed to execute on shard %d: %w", i, err)
		}
	}
	return nil
}

// QueryAllShards executes a query on all shards and aggregates results
func (db *PostgresDB) QueryAllShards(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	var allResults []map[string]interface{}

	for i, pool := range db.pools {
		rows, err := pool.Query(ctx, query, args...)
		if err != nil {
			db.RecordFailedQuery(i)
			return nil, fmt.Errorf("failed to query shard %d: %w", i, err)
		}
		defer rows.Close()

		// Collect results from this shard
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return nil, fmt.Errorf("failed to scan row from shard %d: %w", i, err)
			}

			result := make(map[string]interface{})
			for idx, col := range rows.FieldDescriptions() {
				result[string(col.Name)] = values[idx]
			}
			result["_shard_id"] = i
			allResults = append(allResults, result)
		}

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating rows from shard %d: %w", i, err)
		}
	}

	return allResults, nil
}

// Close closes all database connections
func (db *PostgresDB) Close() {
	// Stop pool manager
	if db.poolManager != nil {
		db.poolManager.Stop()
	}

	for _, pool := range db.pools {
		pool.Close()
	}

	// Close read replica pools
	for _, replicas := range db.readPools {
		for _, pool := range replicas {
			pool.Close()
		}
	}
}

// Health checks the health of all database connections
func (db *PostgresDB) Health(ctx context.Context) error {
	// Check primary shards
	for i, pool := range db.pools {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("shard %d health check failed: %w", i, err)
		}
	}

	// Check read replicas
	for i, replicas := range db.readPools {
		for j, pool := range replicas {
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("shard %d replica %d health check failed: %w", i, j, err)
			}
		}
	}

	return nil
}

// InitSchema initializes the database schema
func (db *PostgresDB) InitSchema(ctx context.Context) error {
	// Create tables on all shards
	for i, pool := range db.pools {
		if err := db.createTables(ctx, pool); err != nil {
			return fmt.Errorf("failed to create tables on shard %d: %w", i, err)
		}
	}
	return nil
}

// createTables creates the necessary tables
func (db *PostgresDB) createTables(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`,

		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			username VARCHAR(255) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);`,

		`CREATE TABLE IF NOT EXISTS channels (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			name VARCHAR(255) NOT NULL,
			type VARCHAR(50) NOT NULL DEFAULT 'public',
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
			channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
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
			call_type VARCHAR(50) NOT NULL DEFAULT 'audio',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			ended_at TIMESTAMP WITH TIME ZONE
		);`,

		`CREATE TABLE IF NOT EXISTS call_participants (
			call_id UUID NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id),
			joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			left_at TIMESTAMP WITH TIME ZONE,
			signaling_state VARCHAR(50) DEFAULT 'joining',
			PRIMARY KEY (call_id, user_id)
		);`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_idempotency ON messages(idempotency_key) WHERE idempotency_key IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_channel_members_user ON channel_members(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_call_sessions_channel ON call_sessions(channel_id);`,
		`CREATE INDEX IF NOT EXISTS idx_call_participants_user ON call_participants(user_id);`,
	}

	for _, query := range queries {
		if _, err := pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query: %s, error: %w", query, err)
		}
	}

	return nil
}
