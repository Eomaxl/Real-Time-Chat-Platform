package region

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PresenceSyncManager handles cross-region presence synchronization
type PresenceSyncManager struct {
	config        *RegionConfig
	localRedis    *redis.Client
	remoteClients map[string]*redis.Client
	syncInterval  time.Duration
	mu            sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
	metrics       *PresenceSyncMetrics
}

// PresenceUpdate represents a presence update
type PresenceUpdate struct {
	UserID     string    `json:"userId"`
	Status     string    `json:"status"` // "online", "offline"
	Region     string    `json:"region"`
	Timestamp  time.Time `json:"timestamp"`
	LastSeen   time.Time `json:"lastSeen"`
	ChannelIDs []string  `json:"channelIds"`
}

// PresenceSyncMetrics tracks synchronization metrics
type PresenceSyncMetrics struct {
	mu              sync.RWMutex
	totalSyncs      int64
	successfulSyncs int64
	failedSyncs     int64
	lastSyncTime    time.Time
	syncLatency     time.Duration
	presenceUpdates int64
}

// NewPresenceSyncManager creates a new presence sync manager
func NewPresenceSyncManager(config *RegionConfig, localRedis *redis.Client, syncInterval time.Duration) (*PresenceSyncManager, error) {
	psm := &PresenceSyncManager{
		config:        config,
		localRedis:    localRedis,
		remoteClients: make(map[string]*redis.Client),
		syncInterval:  syncInterval,
		stopCh:        make(chan struct{}),
		metrics:       &PresenceSyncMetrics{},
	}

	// Connect to remote Redis instances
	if err := psm.connectToRemoteRedis(); err != nil {
		return nil, fmt.Errorf("failed to connect to remote Redis: %w", err)
	}

	return psm, nil
}

// connectToRemoteRedis connects to Redis instances in other regions
func (psm *PresenceSyncManager) connectToRemoteRedis() error {
	currentRegion, err := psm.config.GetCurrentRegion()
	if err != nil {
		return err
	}

	for code, region := range psm.config.Regions {
		// Skip current region
		if code == currentRegion.Code {
			continue
		}

		// Connect to remote Redis
		if len(region.RedisURLs) > 0 {
			client := redis.NewClient(&redis.Options{
				Addr:         region.RedisURLs[0],
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			})

			// Test connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := client.Ping(ctx).Err(); err != nil {
				cancel()
				return fmt.Errorf("failed to ping Redis in region %s: %w", code, err)
			}
			cancel()

			psm.remoteClients[code] = client
		}
	}

	return nil
}

// Start begins presence synchronization
func (psm *PresenceSyncManager) Start(ctx context.Context) {
	psm.wg.Add(2)
	go psm.syncWorker(ctx)
	go psm.subscribeWorker(ctx)
}

// Stop stops presence synchronization
func (psm *PresenceSyncManager) Stop() {
	close(psm.stopCh)
	psm.wg.Wait()

	// Close remote connections
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for _, client := range psm.remoteClients {
		client.Close()
	}
}

// syncWorker periodically syncs presence data
func (psm *PresenceSyncManager) syncWorker(ctx context.Context) {
	defer psm.wg.Done()

	ticker := time.NewTicker(psm.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-psm.stopCh:
			return
		case <-ticker.C:
			psm.performSync(ctx)
		}
	}
}

// subscribeWorker subscribes to presence updates from other regions
func (psm *PresenceSyncManager) subscribeWorker(ctx context.Context) {
	defer psm.wg.Done()

	// Subscribe to presence updates channel
	pubsub := psm.localRedis.Subscribe(ctx, "presence:cross-region")
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-psm.stopCh:
			return
		case msg := <-ch:
			psm.handlePresenceUpdate(ctx, msg.Payload)
		}
	}
}

// performSync performs a full presence synchronization
func (psm *PresenceSyncManager) performSync(ctx context.Context) {
	start := time.Now()
	psm.metrics.incrementTotal()

	currentRegion, err := psm.config.GetCurrentRegion()
	if err != nil {
		psm.metrics.incrementFailed()
		return
	}

	// Get all local presence data
	localPresence, err := psm.getLocalPresence(ctx)
	if err != nil {
		psm.metrics.incrementFailed()
		return
	}

	// Sync to all remote regions
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	for regionCode, client := range psm.remoteClients {
		if err := psm.syncToRegion(ctx, client, regionCode, currentRegion.Code, localPresence); err != nil {
			psm.metrics.incrementFailed()
			continue
		}
	}

	psm.metrics.recordSync(time.Since(start))
	psm.metrics.incrementSuccessful()
}

// getLocalPresence retrieves all local presence data
func (psm *PresenceSyncManager) getLocalPresence(ctx context.Context) ([]PresenceUpdate, error) {
	// Get all presence keys
	keys, err := psm.localRedis.Keys(ctx, "presence:user:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get presence keys: %w", err)
	}

	updates := make([]PresenceUpdate, 0, len(keys))

	for _, key := range keys {
		// Get presence data
		data, err := psm.localRedis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var update PresenceUpdate
		if err := json.Unmarshal([]byte(data), &update); err != nil {
			continue
		}

		updates = append(updates, update)
	}

	return updates, nil
}

// syncToRegion syncs presence data to a specific region
func (psm *PresenceSyncManager) syncToRegion(ctx context.Context, client *redis.Client, targetRegion, sourceRegion string, updates []PresenceUpdate) error {
	pipe := client.Pipeline()

	for _, update := range updates {
		// Mark the source region
		update.Region = sourceRegion
		update.Timestamp = time.Now()

		data, err := json.Marshal(update)
		if err != nil {
			continue
		}

		// Store in remote Redis with TTL
		key := fmt.Sprintf("presence:remote:%s:%s", sourceRegion, update.UserID)
		pipe.Set(ctx, key, data, 60*time.Second)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// handlePresenceUpdate handles a presence update from another region
func (psm *PresenceSyncManager) handlePresenceUpdate(ctx context.Context, payload string) {
	var update PresenceUpdate
	if err := json.Unmarshal([]byte(payload), &update); err != nil {
		return
	}

	// Store remote presence data
	key := fmt.Sprintf("presence:remote:%s:%s", update.Region, update.UserID)
	data, err := json.Marshal(update)
	if err != nil {
		return
	}

	// Store with TTL
	psm.localRedis.Set(ctx, key, data, 60*time.Second)
	psm.metrics.incrementUpdates()
}

// PublishPresenceUpdate publishes a presence update to other regions
func (psm *PresenceSyncManager) PublishPresenceUpdate(ctx context.Context, update PresenceUpdate) error {
	currentRegion, err := psm.config.GetCurrentRegion()
	if err != nil {
		return err
	}

	update.Region = currentRegion.Code
	update.Timestamp = time.Now()

	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal presence update: %w", err)
	}

	// Publish to local Redis
	return psm.localRedis.Publish(ctx, "presence:cross-region", data).Err()
}

// GetGlobalPresence retrieves presence data from all regions for a user
func (psm *PresenceSyncManager) GetGlobalPresence(ctx context.Context, userID string) ([]PresenceUpdate, error) {
	updates := make([]PresenceUpdate, 0)

	// Get local presence
	localKey := fmt.Sprintf("presence:user:%s", userID)
	localData, err := psm.localRedis.Get(ctx, localKey).Result()
	if err == nil {
		var update PresenceUpdate
		if err := json.Unmarshal([]byte(localData), &update); err == nil {
			updates = append(updates, update)
		}
	}

	// Get remote presence from all regions
	remoteKeys, err := psm.localRedis.Keys(ctx, fmt.Sprintf("presence:remote:*:%s", userID)).Result()
	if err == nil {
		for _, key := range remoteKeys {
			data, err := psm.localRedis.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var update PresenceUpdate
			if err := json.Unmarshal([]byte(data), &update); err != nil {
				continue
			}

			updates = append(updates, update)
		}
	}

	return updates, nil
}

// GetMetrics returns synchronization metrics
func (psm *PresenceSyncManager) GetMetrics() map[string]interface{} {
	psm.metrics.mu.RLock()
	defer psm.metrics.mu.RUnlock()

	return map[string]interface{}{
		"total_syncs":      psm.metrics.totalSyncs,
		"successful_syncs": psm.metrics.successfulSyncs,
		"failed_syncs":     psm.metrics.failedSyncs,
		"last_sync_time":   psm.metrics.lastSyncTime,
		"sync_latency_ms":  psm.metrics.syncLatency.Milliseconds(),
		"presence_updates": psm.metrics.presenceUpdates,
	}
}

// incrementTotal increments total syncs counter
func (m *PresenceSyncMetrics) incrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalSyncs++
}

// incrementSuccessful increments successful syncs counter
func (m *PresenceSyncMetrics) incrementSuccessful() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successfulSyncs++
}

// incrementFailed increments failed syncs counter
func (m *PresenceSyncMetrics) incrementFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedSyncs++
}

// incrementUpdates increments presence updates counter
func (m *PresenceSyncMetrics) incrementUpdates() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.presenceUpdates++
}

// recordSync records sync statistics
func (m *PresenceSyncMetrics) recordSync(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSyncTime = time.Now()
	m.syncLatency = latency
}
