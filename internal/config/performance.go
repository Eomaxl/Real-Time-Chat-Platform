package config

import (
	"time"
)

// PerformanceConfig contains performance optimization settings
type PerformanceConfig struct {
	// Caching configuration
	Cache CacheConfig `json:"cache"`

	// Batching configuration
	Batch BatchConfig `json:"batch"`

	// Connection pooling configuration
	ConnectionPool ConnectionPoolConfig `json:"connection_pool"`

	// GC configuration
	GC GCConfig `json:"gc"`

	// Object pooling configuration
	ObjectPool ObjectPoolConfig `json:"object_pool"`
}

// CacheConfig configures caching behavior
type CacheConfig struct {
	// L1 (in-memory) cache settings
	L1Capacity int           `json:"l1_capacity"`
	L1TTL      time.Duration `json:"l1_ttl"`

	// L2 (Redis) cache settings
	L2TTL time.Duration `json:"l2_ttl"`

	// Cache warming settings
	EnableWarming   bool          `json:"enable_warming"`
	WarmingInterval time.Duration `json:"warming_interval"`
}

// BatchConfig configures batch processing
type BatchConfig struct {
	// Message batch settings
	MessageBatchSize     int           `json:"message_batch_size"`
	MessageFlushInterval time.Duration `json:"message_flush_interval"`

	// Presence batch settings
	PresenceBatchSize     int           `json:"presence_batch_size"`
	PresenceFlushInterval time.Duration `json:"presence_flush_interval"`

	// Event batch settings
	EventBatchSize     int           `json:"event_batch_size"`
	EventFlushInterval time.Duration `json:"event_flush_interval"`
}

// ConnectionPoolConfig configures connection pooling
type ConnectionPoolConfig struct {
	// Database connection pool settings
	MaxConnections    int           `json:"max_connections"`
	MinConnections    int           `json:"min_connections"`
	MaxIdleTime       time.Duration `json:"max_idle_time"`
	MaxLifetime       time.Duration `json:"max_lifetime"`
	HealthCheckPeriod time.Duration `json:"health_check_period"`

	// Redis connection pool settings
	RedisMaxConnections int           `json:"redis_max_connections"`
	RedisMinConnections int           `json:"redis_min_connections"`
	RedisMaxIdleTime    time.Duration `json:"redis_max_idle_time"`
}

// GCConfig configures garbage collection
type GCConfig struct {
	// GC target percentage (GOGC)
	TargetPercent int `json:"target_percent"`

	// Memory limit in MB
	MemoryLimitMB int64 `json:"memory_limit_mb"`

	// Enable memory limit
	EnableMemoryLimit bool `json:"enable_memory_limit"`

	// GC monitoring
	EnableMonitoring   bool          `json:"enable_monitoring"`
	MonitoringInterval time.Duration `json:"monitoring_interval"`
}

// ObjectPoolConfig configures object pooling
type ObjectPoolConfig struct {
	// Message pool settings
	EnableMessagePool bool `json:"enable_message_pool"`

	// Buffer pool settings
	EnableBufferPool bool `json:"enable_buffer_pool"`
	BufferSize       int  `json:"buffer_size"`

	// Connection pool settings
	EnableConnectionPool bool `json:"enable_connection_pool"`
}

// DefaultPerformanceConfig returns default performance configuration
func DefaultPerformanceConfig() PerformanceConfig {
	return PerformanceConfig{
		Cache: CacheConfig{
			L1Capacity:      10000,
			L1TTL:           5 * time.Minute,
			L2TTL:           30 * time.Minute,
			EnableWarming:   true,
			WarmingInterval: 10 * time.Minute,
		},
		Batch: BatchConfig{
			MessageBatchSize:      100,
			MessageFlushInterval:  100 * time.Millisecond,
			PresenceBatchSize:     200,
			PresenceFlushInterval: 100 * time.Millisecond,
			EventBatchSize:        50,
			EventFlushInterval:    50 * time.Millisecond,
		},
		ConnectionPool: ConnectionPoolConfig{
			MaxConnections:      100,
			MinConnections:      10,
			MaxIdleTime:         30 * time.Minute,
			MaxLifetime:         1 * time.Hour,
			HealthCheckPeriod:   1 * time.Minute,
			RedisMaxConnections: 50,
			RedisMinConnections: 5,
			RedisMaxIdleTime:    10 * time.Minute,
		},
		GC: GCConfig{
			TargetPercent:      200, // Less frequent GC for low latency
			MemoryLimitMB:      0,   // Disabled by default
			EnableMemoryLimit:  false,
			EnableMonitoring:   true,
			MonitoringInterval: 1 * time.Minute,
		},
		ObjectPool: ObjectPoolConfig{
			EnableMessagePool:    true,
			EnableBufferPool:     true,
			BufferSize:           4096,
			EnableConnectionPool: true,
		},
	}
}

// HighThroughputConfig returns configuration optimized for high throughput
func HighThroughputConfig() PerformanceConfig {
	config := DefaultPerformanceConfig()

	// Larger batches for higher throughput
	config.Batch.MessageBatchSize = 500
	config.Batch.PresenceBatchSize = 1000
	config.Batch.EventBatchSize = 200

	// Less frequent GC
	config.GC.TargetPercent = 300

	// Larger caches
	config.Cache.L1Capacity = 50000

	return config
}

// LowLatencyConfig returns configuration optimized for low latency
func LowLatencyConfig() PerformanceConfig {
	config := DefaultPerformanceConfig()

	// Smaller batches for lower latency
	config.Batch.MessageBatchSize = 50
	config.Batch.MessageFlushInterval = 50 * time.Millisecond
	config.Batch.PresenceBatchSize = 100
	config.Batch.PresenceFlushInterval = 50 * time.Millisecond

	// More frequent GC to reduce pause times
	config.GC.TargetPercent = 100

	return config
}

// MemoryEfficientConfig returns configuration optimized for memory efficiency
func MemoryEfficientConfig() PerformanceConfig {
	config := DefaultPerformanceConfig()

	// Smaller caches
	config.Cache.L1Capacity = 5000
	config.Cache.L1TTL = 2 * time.Minute

	// More frequent GC
	config.GC.TargetPercent = 50
	config.GC.EnableMemoryLimit = true
	config.GC.MemoryLimitMB = 2048 // 2GB limit

	// Smaller connection pools
	config.ConnectionPool.MaxConnections = 50
	config.ConnectionPool.RedisMaxConnections = 25

	return config
}
