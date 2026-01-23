package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolManager manages connection pool health and failover
type PoolManager struct {
	db                  *PostgresDB
	healthCheckInterval time.Duration
	failoverEnabled     bool
	mu                  sync.RWMutex
	unhealthyShards     map[int]bool
	unhealthyReplicas   map[int]map[int]bool // shard -> replica -> unhealthy
	stopCh              chan struct{}
}

// NewPoolManager creates a new pool manager
func NewPoolManager(db *PostgresDB, healthCheckInterval time.Duration) *PoolManager {
	return &PoolManager{
		db:                  db,
		healthCheckInterval: healthCheckInterval,
		failoverEnabled:     true,
		unhealthyShards:     make(map[int]bool),
		unhealthyReplicas:   make(map[int]map[int]bool),
		stopCh:              make(chan struct{}),
	}
}

// Start begins health checking and monitoring
func (pm *PoolManager) Start(ctx context.Context) {
	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.performHealthChecks(ctx)
		}
	}
}

// Stop stops the pool manager
func (pm *PoolManager) Stop() {
	close(pm.stopCh)
}

// performHealthChecks checks health of all pools
func (pm *PoolManager) performHealthChecks(ctx context.Context) {
	// Check primary shards
	for i, pool := range pm.db.pools {
		if err := pm.checkPoolHealth(ctx, pool); err != nil {
			pm.markShardUnhealthy(i)
		} else {
			pm.markShardHealthy(i)
		}
	}

	// Check read replicas
	for shardID, replicas := range pm.db.readPools {
		if pm.unhealthyReplicas[shardID] == nil {
			pm.unhealthyReplicas[shardID] = make(map[int]bool)
		}

		for replicaID, pool := range replicas {
			if err := pm.checkPoolHealth(ctx, pool); err != nil {
				pm.markReplicaUnhealthy(shardID, replicaID)
			} else {
				pm.markReplicaHealthy(shardID, replicaID)
			}
		}
	}
}

// checkPoolHealth performs a health check on a pool
func (pm *PoolManager) checkPoolHealth(ctx context.Context, pool *pgxpool.Pool) error {
	// Create a timeout context for health check
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Ping the database
	if err := pool.Ping(checkCtx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Check pool stats
	stat := pool.Stat()

	// Check if pool is exhausted
	if stat.AcquiredConns() >= stat.MaxConns() {
		return fmt.Errorf("pool exhausted: %d/%d connections in use", stat.AcquiredConns(), stat.MaxConns())
	}

	// Check if there are too many idle connections (might indicate a problem)
	if stat.IdleConns() == 0 && stat.TotalConns() > 0 {
		// All connections are in use, might be under heavy load
		// This is not necessarily unhealthy, but worth noting
	}

	return nil
}

// markShardUnhealthy marks a shard as unhealthy
func (pm *PoolManager) markShardUnhealthy(shardID int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.unhealthyShards[shardID] {
		pm.unhealthyShards[shardID] = true
		// Log or emit metric
		fmt.Printf("WARNING: Shard %d marked as unhealthy\n", shardID)
	}
}

// markShardHealthy marks a shard as healthy
func (pm *PoolManager) markShardHealthy(shardID int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.unhealthyShards[shardID] {
		delete(pm.unhealthyShards, shardID)
		// Log or emit metric
		fmt.Printf("INFO: Shard %d recovered and marked as healthy\n", shardID)
	}
}

// markReplicaUnhealthy marks a replica as unhealthy
func (pm *PoolManager) markReplicaUnhealthy(shardID, replicaID int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.unhealthyReplicas[shardID] == nil {
		pm.unhealthyReplicas[shardID] = make(map[int]bool)
	}

	if !pm.unhealthyReplicas[shardID][replicaID] {
		pm.unhealthyReplicas[shardID][replicaID] = true
		// Log or emit metric
		fmt.Printf("WARNING: Shard %d replica %d marked as unhealthy\n", shardID, replicaID)
	}
}

// markReplicaHealthy marks a replica as healthy
func (pm *PoolManager) markReplicaHealthy(shardID, replicaID int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.unhealthyReplicas[shardID] != nil && pm.unhealthyReplicas[shardID][replicaID] {
		delete(pm.unhealthyReplicas[shardID], replicaID)
		// Log or emit metric
		fmt.Printf("INFO: Shard %d replica %d recovered and marked as healthy\n", shardID, replicaID)
	}
}

// IsShardHealthy checks if a shard is healthy
func (pm *PoolManager) IsShardHealthy(shardID int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return !pm.unhealthyShards[shardID]
}

// IsReplicaHealthy checks if a replica is healthy
func (pm *PoolManager) IsReplicaHealthy(shardID, replicaID int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.unhealthyReplicas[shardID] == nil {
		return true
	}

	return !pm.unhealthyReplicas[shardID][replicaID]
}

// GetHealthyReplicas returns healthy replicas for a shard
func (pm *PoolManager) GetHealthyReplicas(shardID int) []*pgxpool.Pool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	replicas := pm.db.readPools[shardID]
	var healthy []*pgxpool.Pool

	for i, pool := range replicas {
		if pm.IsReplicaHealthy(shardID, i) {
			healthy = append(healthy, pool)
		}
	}

	return healthy
}

// GetPoolStats returns detailed statistics for all pools
func (pm *PoolManager) GetPoolStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]interface{})

	// Primary shard stats
	primaryStats := make([]map[string]interface{}, len(pm.db.pools))
	for i, pool := range pm.db.pools {
		stat := pool.Stat()
		primaryStats[i] = map[string]interface{}{
			"shard_id":               i,
			"healthy":                !pm.unhealthyShards[i],
			"acquired_conns":         stat.AcquiredConns(),
			"idle_conns":             stat.IdleConns(),
			"total_conns":            stat.TotalConns(),
			"max_conns":              stat.MaxConns(),
			"acquire_count":          stat.AcquireCount(),
			"acquire_duration_ms":    stat.AcquireDuration().Milliseconds(),
			"empty_acquire_count":    stat.EmptyAcquireCount(),
			"canceled_acquire_count": stat.CanceledAcquireCount(),
		}
	}
	stats["primary_shards"] = primaryStats

	// Replica stats
	replicaStats := make(map[int][]map[string]interface{})
	for shardID, replicas := range pm.db.readPools {
		replicaStats[shardID] = make([]map[string]interface{}, len(replicas))
		for i, pool := range replicas {
			stat := pool.Stat()
			healthy := true
			if pm.unhealthyReplicas[shardID] != nil {
				healthy = !pm.unhealthyReplicas[shardID][i]
			}

			replicaStats[shardID][i] = map[string]interface{}{
				"replica_id":             i,
				"healthy":                healthy,
				"acquired_conns":         stat.AcquiredConns(),
				"idle_conns":             stat.IdleConns(),
				"total_conns":            stat.TotalConns(),
				"max_conns":              stat.MaxConns(),
				"acquire_count":          stat.AcquireCount(),
				"acquire_duration_ms":    stat.AcquireDuration().Milliseconds(),
				"empty_acquire_count":    stat.EmptyAcquireCount(),
				"canceled_acquire_count": stat.CanceledAcquireCount(),
			}
		}
	}
	stats["replicas"] = replicaStats

	return stats
}

// OptimizePoolSizes adjusts pool sizes based on usage patterns
func (pm *PoolManager) OptimizePoolSizes(ctx context.Context) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// This is a placeholder for dynamic pool size optimization
	// In production, this would analyze usage patterns and adjust pool sizes
	// For now, we just log current usage

	for i, pool := range pm.db.pools {
		stat := pool.Stat()
		utilizationPercent := float64(stat.AcquiredConns()) / float64(stat.MaxConns()) * 100

		if utilizationPercent > 80 {
			fmt.Printf("WARNING: Shard %d pool utilization high: %.2f%%\n", i, utilizationPercent)
		}
	}
}
