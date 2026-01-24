package database

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReplicaManager manages read replica selection and failover
type ReplicaManager struct {
	db                *PostgresDB
	replicaCounters   map[int]*uint64               // Round-robin counters per shard
	replicationLag    map[int]map[int]time.Duration // shard -> replica -> lag
	maxReplicationLag time.Duration
	mu                sync.RWMutex
}

// NewReplicaManager creates a new replica manager
func NewReplicaManager(db *PostgresDB, maxReplicationLag time.Duration) *ReplicaManager {
	counters := make(map[int]*uint64)
	for i := 0; i < db.shards; i++ {
		counter := uint64(0)
		counters[i] = &counter
	}

	return &ReplicaManager{
		db:                db,
		replicaCounters:   counters,
		replicationLag:    make(map[int]map[int]time.Duration),
		maxReplicationLag: maxReplicationLag,
	}
}

// SelectReplica selects a read replica for a shard using weighted round-robin
func (rm *ReplicaManager) SelectReplica(shardID int) *pgxpool.Pool {
	replicas := rm.db.readPools[shardID]
	if len(replicas) == 0 {
		// No replicas available, use primary
		return rm.db.pools[shardID]
	}

	// Get healthy replicas
	healthyReplicas := rm.getHealthyReplicas(shardID)
	if len(healthyReplicas) == 0 {
		// No healthy replicas, fall back to primary
		return rm.db.pools[shardID]
	}

	// Use round-robin to select from healthy replicas
	counter := atomic.AddUint64(rm.replicaCounters[shardID], 1)
	idx := int(counter) % len(healthyReplicas)

	return healthyReplicas[idx]
}

// getHealthyReplicas returns replicas that are healthy and within replication lag threshold
func (rm *ReplicaManager) getHealthyReplicas(shardID int) []*pgxpool.Pool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	replicas := rm.db.readPools[shardID]
	var healthy []*pgxpool.Pool

	for i, pool := range replicas {
		// Check if replica is healthy via pool manager
		if rm.db.poolManager != nil && !rm.db.poolManager.IsReplicaHealthy(shardID, i) {
			continue
		}

		// Check replication lag
		if rm.replicationLag[shardID] != nil {
			lag := rm.replicationLag[shardID][i]
			if lag > rm.maxReplicationLag {
				// Replica is too far behind, skip it
				continue
			}
		}

		healthy = append(healthy, pool)
	}

	return healthy
}

// MonitorReplicationLag monitors replication lag for all replicas
func (rm *ReplicaManager) MonitorReplicationLag(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.checkReplicationLag(ctx)
		}
	}
}

// checkReplicationLag checks replication lag for all replicas
func (rm *ReplicaManager) checkReplicationLag(ctx context.Context) {
	for shardID, replicas := range rm.db.readPools {
		for replicaID, pool := range replicas {
			lag, err := rm.measureReplicationLag(ctx, pool)
			if err != nil {
				// Log error but continue
				fmt.Printf("WARNING: Failed to measure replication lag for shard %d replica %d: %v\n", shardID, replicaID, err)
				continue
			}

			rm.mu.Lock()
			if rm.replicationLag[shardID] == nil {
				rm.replicationLag[shardID] = make(map[int]time.Duration)
			}
			rm.replicationLag[shardID][replicaID] = lag
			rm.mu.Unlock()

			// Log warning if lag is high
			if lag > rm.maxReplicationLag {
				fmt.Printf("WARNING: Shard %d replica %d has high replication lag: %v\n", shardID, replicaID, lag)
			}
		}
	}
}

// measureReplicationLag measures replication lag for a replica
func (rm *ReplicaManager) measureReplicationLag(ctx context.Context, pool *pgxpool.Pool) (time.Duration, error) {
	// Query to check replication lag
	// This uses PostgreSQL's pg_stat_replication view
	query := `
		SELECT COALESCE(
			EXTRACT(EPOCH FROM (NOW() - pg_last_xact_replay_timestamp())),
			0
		) AS lag_seconds
	`

	var lagSeconds float64
	err := pool.QueryRow(ctx, query).Scan(&lagSeconds)
	if err != nil {
		return 0, fmt.Errorf("failed to query replication lag: %w", err)
	}

	return time.Duration(lagSeconds * float64(time.Second)), nil
}

// GetReplicationLag returns the current replication lag for a replica
func (rm *ReplicaManager) GetReplicationLag(shardID, replicaID int) time.Duration {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.replicationLag[shardID] == nil {
		return 0
	}

	return rm.replicationLag[shardID][replicaID]
}

// GetReplicationStats returns replication statistics for all replicas
func (rm *ReplicaManager) GetReplicationStats() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := make(map[string]interface{})

	replicaStats := make(map[int][]map[string]interface{})
	for shardID, replicas := range rm.db.readPools {
		replicaStats[shardID] = make([]map[string]interface{}, len(replicas))

		for i := range replicas {
			lag := time.Duration(0)
			if rm.replicationLag[shardID] != nil {
				lag = rm.replicationLag[shardID][i]
			}

			healthy := true
			if rm.db.poolManager != nil {
				healthy = rm.db.poolManager.IsReplicaHealthy(shardID, i)
			}

			replicaStats[shardID][i] = map[string]interface{}{
				"replica_id":         i,
				"healthy":            healthy,
				"replication_lag_ms": lag.Milliseconds(),
				"within_threshold":   lag <= rm.maxReplicationLag,
			}
		}
	}

	stats["replicas"] = replicaStats
	stats["max_replication_lag_ms"] = rm.maxReplicationLag.Milliseconds()

	return stats
}

// PreferPrimaryForConsistency returns the primary pool when strong consistency is needed
func (rm *ReplicaManager) PreferPrimaryForConsistency(shardID int) *pgxpool.Pool {
	return rm.db.pools[shardID]
}

// SelectReplicaWithFallback selects a replica with automatic fallback to primary
func (rm *ReplicaManager) SelectReplicaWithFallback(shardID int, requireStrongConsistency bool) *pgxpool.Pool {
	// If strong consistency is required, always use primary
	if requireStrongConsistency {
		return rm.PreferPrimaryForConsistency(shardID)
	}

	// Try to use a replica
	replica := rm.SelectReplica(shardID)

	// If we got the primary (no replicas available), that's fine
	return replica
}

// WaitForReplication waits for replicas to catch up to a certain point
func (rm *ReplicaManager) WaitForReplication(ctx context.Context, shardID int, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		allCaughtUp := true

		rm.mu.RLock()
		if rm.replicationLag[shardID] != nil {
			for _, lag := range rm.replicationLag[shardID] {
				if lag > rm.maxReplicationLag {
					allCaughtUp = false
					break
				}
			}
		}
		rm.mu.RUnlock()

		if allCaughtUp {
			return nil
		}

		// Wait a bit before checking again
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Continue loop
		}
	}

	return fmt.Errorf("timeout waiting for replication to catch up")
}
