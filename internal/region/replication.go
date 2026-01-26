package region

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReplicationManager handles cross-region database replication
type ReplicationManager struct {
	config           *RegionConfig
	localPool        *pgxpool.Pool
	remotePools      map[string]*pgxpool.Pool
	replicationLog   *ReplicationLog
	conflictResolver ConflictResolver
	mu               sync.RWMutex
	stopCh           chan struct{}
	wg               sync.WaitGroup
}

// ReplicationLog tracks replication operations
type ReplicationLog struct {
	entries []ReplicationEntry
	mu      sync.RWMutex
}

// ReplicationEntry represents a single replication operation
type ReplicationEntry struct {
	ID           string
	Timestamp    time.Time
	SourceRegion string
	TargetRegion string
	Operation    string // "insert", "update", "delete"
	Table        string
	RecordID     string
	Data         map[string]interface{}
	Status       string // "pending", "completed", "failed"
	RetryCount   int
	LastError    string
}

// ConflictResolver handles replication conflicts
type ConflictResolver interface {
	Resolve(ctx context.Context, local, remote ReplicationEntry) (ReplicationEntry, error)
}

// LastWriteWinsResolver resolves conflicts by choosing the most recent write
type LastWriteWinsResolver struct{}

// VectorClockResolver resolves conflicts using vector clocks
type VectorClockResolver struct {
	clocks map[string]map[string]int64 // region -> record_id -> version
	mu     sync.RWMutex
}

// NewReplicationManager creates a new replication manager
func NewReplicationManager(config *RegionConfig, localPool *pgxpool.Pool) (*ReplicationManager, error) {
	rm := &ReplicationManager{
		config:         config,
		localPool:      localPool,
		remotePools:    make(map[string]*pgxpool.Pool),
		replicationLog: &ReplicationLog{entries: make([]ReplicationEntry, 0)},
		stopCh:         make(chan struct{}),
	}

	// Initialize conflict resolver based on configuration
	switch config.ConflictResolution {
	case "last-write-wins":
		rm.conflictResolver = &LastWriteWinsResolver{}
	case "vector-clock":
		rm.conflictResolver = &VectorClockResolver{
			clocks: make(map[string]map[string]int64),
		}
	default:
		rm.conflictResolver = &LastWriteWinsResolver{}
	}

	// Connect to remote regions
	if err := rm.connectToRemoteRegions(); err != nil {
		return nil, fmt.Errorf("failed to connect to remote regions: %w", err)
	}

	return rm, nil
}

// connectToRemoteRegions establishes connections to all remote regions
func (rm *ReplicationManager) connectToRemoteRegions() error {
	currentRegion, err := rm.config.GetCurrentRegion()
	if err != nil {
		return err
	}

	for code, region := range rm.config.Regions {
		// Skip current region
		if code == currentRegion.Code {
			continue
		}

		// Connect to each database URL in the region
		if len(region.DatabaseURLs) > 0 {
			pool, err := pgxpool.New(context.Background(), region.DatabaseURLs[0])
			if err != nil {
				return fmt.Errorf("failed to connect to region %s: %w", code, err)
			}

			// Test connection
			if err := pool.Ping(context.Background()); err != nil {
				pool.Close()
				return fmt.Errorf("failed to ping region %s: %w", code, err)
			}

			rm.remotePools[code] = pool
		}
	}

	return nil
}

// Start begins the replication process
func (rm *ReplicationManager) Start(ctx context.Context) {
	rm.wg.Add(1)
	go rm.replicationWorker(ctx)
}

// Stop stops the replication process
func (rm *ReplicationManager) Stop() {
	close(rm.stopCh)
	rm.wg.Wait()

	// Close remote connections
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for _, pool := range rm.remotePools {
		pool.Close()
	}
}

// replicationWorker processes replication entries
func (rm *ReplicationManager) replicationWorker(ctx context.Context) {
	defer rm.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.processPendingReplications(ctx)
		}
	}
}

// processPendingReplications processes all pending replication entries
func (rm *ReplicationManager) processPendingReplications(ctx context.Context) {
	rm.replicationLog.mu.Lock()
	pendingEntries := make([]ReplicationEntry, 0)
	for _, entry := range rm.replicationLog.entries {
		if entry.Status == "pending" {
			pendingEntries = append(pendingEntries, entry)
		}
	}
	rm.replicationLog.mu.Unlock()

	for _, entry := range pendingEntries {
		if err := rm.replicateEntry(ctx, entry); err != nil {
			rm.updateEntryStatus(entry.ID, "failed", err.Error())
		} else {
			rm.updateEntryStatus(entry.ID, "completed", "")
		}
	}
}

// replicateEntry replicates a single entry to remote regions
func (rm *ReplicationManager) replicateEntry(ctx context.Context, entry ReplicationEntry) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Replicate to all remote regions
	for regionCode, pool := range rm.remotePools {
		if err := rm.replicateToRegion(ctx, pool, regionCode, entry); err != nil {
			return fmt.Errorf("failed to replicate to region %s: %w", regionCode, err)
		}
	}

	return nil
}

// replicateToRegion replicates an entry to a specific region
func (rm *ReplicationManager) replicateToRegion(ctx context.Context, pool *pgxpool.Pool, regionCode string, entry ReplicationEntry) error {
	switch entry.Operation {
	case "insert":
		return rm.replicateInsert(ctx, pool, entry)
	case "update":
		return rm.replicateUpdate(ctx, pool, entry)
	case "delete":
		return rm.replicateDelete(ctx, pool, entry)
	default:
		return fmt.Errorf("unknown operation: %s", entry.Operation)
	}
}

// replicateInsert replicates an insert operation
func (rm *ReplicationManager) replicateInsert(ctx context.Context, pool *pgxpool.Pool, entry ReplicationEntry) error {
	// Check for conflicts
	var exists bool
	err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1)", entry.Table),
		entry.RecordID,
	).Scan(&exists)

	if err != nil {
		return fmt.Errorf("failed to check for existing record: %w", err)
	}

	if exists {
		// Conflict detected - resolve it
		return rm.handleConflict(ctx, pool, entry)
	}

	// No conflict - perform insert
	// This is a simplified version - in production, you'd build the query dynamically
	// based on the entry.Data fields
	return nil
}

// replicateUpdate replicates an update operation
func (rm *ReplicationManager) replicateUpdate(ctx context.Context, pool *pgxpool.Pool, entry ReplicationEntry) error {
	// Check if record exists and get current version
	var currentTimestamp time.Time
	err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT updated_at FROM %s WHERE id = $1", entry.Table),
		entry.RecordID,
	).Scan(&currentTimestamp)

	if err != nil {
		// Record doesn't exist - treat as insert
		return rm.replicateInsert(ctx, pool, entry)
	}

	// Check for conflicts based on timestamp
	if currentTimestamp.After(entry.Timestamp) {
		// Remote is newer - conflict
		return rm.handleConflict(ctx, pool, entry)
	}

	// Perform update
	// This is a simplified version - in production, you'd build the query dynamically
	return nil
}

// replicateDelete replicates a delete operation
func (rm *ReplicationManager) replicateDelete(ctx context.Context, pool *pgxpool.Pool, entry ReplicationEntry) error {
	_, err := pool.Exec(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id = $1", entry.Table),
		entry.RecordID,
	)
	return err
}

// handleConflict handles replication conflicts
func (rm *ReplicationManager) handleConflict(ctx context.Context, pool *pgxpool.Pool, entry ReplicationEntry) error {
	// Get remote entry
	var remoteData map[string]interface{}
	// In production, you'd query the remote data here

	remoteEntry := ReplicationEntry{
		Data:      remoteData,
		Timestamp: time.Now(), // Would be actual remote timestamp
	}

	// Resolve conflict
	resolved, err := rm.conflictResolver.Resolve(ctx, entry, remoteEntry)
	if err != nil {
		return fmt.Errorf("failed to resolve conflict: %w", err)
	}

	// Apply resolved entry
	_ = resolved // Would apply the resolved entry here

	return nil
}

// AddReplicationEntry adds a new entry to the replication log
func (rm *ReplicationManager) AddReplicationEntry(entry ReplicationEntry) {
	rm.replicationLog.mu.Lock()
	defer rm.replicationLog.mu.Unlock()

	entry.Status = "pending"
	entry.Timestamp = time.Now()
	rm.replicationLog.entries = append(rm.replicationLog.entries, entry)
}

// updateEntryStatus updates the status of a replication entry
func (rm *ReplicationManager) updateEntryStatus(id, status, errorMsg string) {
	rm.replicationLog.mu.Lock()
	defer rm.replicationLog.mu.Unlock()

	for i, entry := range rm.replicationLog.entries {
		if entry.ID == id {
			rm.replicationLog.entries[i].Status = status
			rm.replicationLog.entries[i].LastError = errorMsg
			if status == "failed" {
				rm.replicationLog.entries[i].RetryCount++
			}
			break
		}
	}
}

// GetReplicationStats returns replication statistics
func (rm *ReplicationManager) GetReplicationStats() map[string]interface{} {
	rm.replicationLog.mu.RLock()
	defer rm.replicationLog.mu.RUnlock()

	stats := map[string]interface{}{
		"total_entries":     len(rm.replicationLog.entries),
		"pending_entries":   0,
		"completed_entries": 0,
		"failed_entries":    0,
	}

	for _, entry := range rm.replicationLog.entries {
		switch entry.Status {
		case "pending":
			stats["pending_entries"] = stats["pending_entries"].(int) + 1
		case "completed":
			stats["completed_entries"] = stats["completed_entries"].(int) + 1
		case "failed":
			stats["failed_entries"] = stats["failed_entries"].(int) + 1
		}
	}

	return stats
}

// Resolve implements LastWriteWinsResolver
func (r *LastWriteWinsResolver) Resolve(ctx context.Context, local, remote ReplicationEntry) (ReplicationEntry, error) {
	if local.Timestamp.After(remote.Timestamp) {
		return local, nil
	}
	return remote, nil
}

// Resolve implements VectorClockResolver
func (r *VectorClockResolver) Resolve(ctx context.Context, local, remote ReplicationEntry) (ReplicationEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get vector clocks for both entries
	localClock := r.getOrCreateClock(local.SourceRegion, local.RecordID)
	remoteClock := r.getOrCreateClock(remote.SourceRegion, remote.RecordID)

	// Compare vector clocks
	if localClock > remoteClock {
		return local, nil
	} else if remoteClock > localClock {
		return remote, nil
	}

	// Concurrent updates - use timestamp as tiebreaker
	if local.Timestamp.After(remote.Timestamp) {
		return local, nil
	}
	return remote, nil
}

// getOrCreateClock gets or creates a vector clock entry
func (r *VectorClockResolver) getOrCreateClock(region, recordID string) int64 {
	if _, ok := r.clocks[region]; !ok {
		r.clocks[region] = make(map[string]int64)
	}
	return r.clocks[region][recordID]
}

// IncrementClock increments the vector clock for a region and record
func (r *VectorClockResolver) IncrementClock(region, recordID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.clocks[region]; !ok {
		r.clocks[region] = make(map[string]int64)
	}
	r.clocks[region][recordID]++
}
