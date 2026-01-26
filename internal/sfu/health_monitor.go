package sfu

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// HealthMonitor monitors the health and performance of the SFU service
type HealthMonitor struct {
	service        *Service
	loadBalancer   *LoadBalancer
	instanceID     string
	region         string
	maxSessions    int
	reportInterval time.Duration
	mu             sync.RWMutex
}

// HealthMetrics represents health metrics for the SFU service
type HealthMetrics struct {
	InstanceID     string    `json:"instance_id"`
	Region         string    `json:"region"`
	ActiveSessions int       `json:"active_sessions"`
	MaxSessions    int       `json:"max_sessions"`
	CPUUsage       float64   `json:"cpu_usage"`
	MemoryUsage    float64   `json:"memory_usage"`
	BandwidthUsage uint64    `json:"bandwidth_usage"`
	Healthy        bool      `json:"healthy"`
	Timestamp      time.Time `json:"timestamp"`
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(service *Service, loadBalancer *LoadBalancer, instanceID, region string, maxSessions int) *HealthMonitor {
	return &HealthMonitor{
		service:        service,
		loadBalancer:   loadBalancer,
		instanceID:     instanceID,
		region:         region,
		maxSessions:    maxSessions,
		reportInterval: 10 * time.Second,
	}
}

// Start starts the health monitoring
func (hm *HealthMonitor) Start(ctx context.Context) {
	// Register this instance with the load balancer
	instance := &SFUInstance{
		ID:              hm.instanceID,
		Address:         "localhost", // In production, use actual address
		Port:            "8084",      // In production, use actual port
		Region:          hm.region,
		ActiveSessions:  0,
		MaxSessions:     hm.maxSessions,
		CPUUsage:        0,
		MemoryUsage:     0,
		BandwidthUsage:  0,
		LastHealthCheck: time.Now(),
		Healthy:         true,
	}
	hm.loadBalancer.RegisterInstance(instance)

	// Start periodic health reporting
	go hm.reportHealth(ctx)

	// Start monitoring for failover
	go hm.monitorForFailover(ctx)
}

// reportHealth periodically reports health metrics to the load balancer
func (hm *HealthMonitor) reportHealth(ctx context.Context) {
	ticker := time.NewTicker(hm.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Deregister on shutdown
			hm.loadBalancer.DeregisterInstance(hm.instanceID)
			return
		case <-ticker.C:
			metrics := hm.collectMetrics()
			hm.updateLoadBalancer(metrics)
		}
	}
}

// collectMetrics collects current health metrics
func (hm *HealthMonitor) collectMetrics() *HealthMetrics {
	// Get active session count
	sessionStats := hm.service.GetSessionStats()
	activeSessions := len(sessionStats)

	// Get CPU and memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	cpuUsage := hm.getCPUUsage()
	memoryUsage := float64(memStats.Alloc) / float64(memStats.Sys) * 100

	// Calculate bandwidth usage (simplified)
	bandwidthUsage := hm.calculateBandwidthUsage(sessionStats)

	// Determine health status
	healthy := hm.isHealthy(activeSessions, cpuUsage, memoryUsage)

	return &HealthMetrics{
		InstanceID:     hm.instanceID,
		Region:         hm.region,
		ActiveSessions: activeSessions,
		MaxSessions:    hm.maxSessions,
		CPUUsage:       cpuUsage,
		MemoryUsage:    memoryUsage,
		BandwidthUsage: bandwidthUsage,
		Healthy:        healthy,
		Timestamp:      time.Now(),
	}
}

// getCPUUsage gets the current CPU usage (simplified)
func (hm *HealthMonitor) getCPUUsage() float64 {
	// In a real implementation, this would use system calls to get actual CPU usage
	// For now, we'll use goroutine count as a proxy
	numGoroutines := runtime.NumGoroutine()

	// Normalize to percentage (assuming 1000 goroutines = 50% CPU)
	cpuUsage := float64(numGoroutines) / 1000.0 * 50.0
	if cpuUsage > 100.0 {
		cpuUsage = 100.0
	}

	return cpuUsage
}

// calculateBandwidthUsage calculates total bandwidth usage
func (hm *HealthMonitor) calculateBandwidthUsage(sessionStats map[string]*SFUStats) uint64 {
	var totalBandwidth uint64

	for _, stats := range sessionStats {
		// Estimate bandwidth: ~1 Mbps per publisher, ~500 Kbps per subscriber
		publisherBandwidth := uint64(stats.PublisherCount) * 1_000_000 // 1 Mbps
		subscriberBandwidth := uint64(stats.SubscriberCount) * 500_000 // 500 Kbps
		totalBandwidth += publisherBandwidth + subscriberBandwidth
	}

	return totalBandwidth
}

// isHealthy determines if the instance is healthy
func (hm *HealthMonitor) isHealthy(activeSessions int, cpuUsage, memoryUsage float64) bool {
	// Health checks
	if activeSessions >= hm.maxSessions {
		return false // At capacity
	}

	if cpuUsage > 90.0 {
		return false // CPU overloaded
	}

	if memoryUsage > 90.0 {
		return false // Memory overloaded
	}

	return true
}

// updateLoadBalancer updates the load balancer with current metrics
func (hm *HealthMonitor) updateLoadBalancer(metrics *HealthMetrics) {
	err := hm.loadBalancer.UpdateInstanceLoad(
		hm.instanceID,
		metrics.ActiveSessions,
		metrics.CPUUsage,
		metrics.MemoryUsage,
		metrics.BandwidthUsage,
	)

	if err != nil {
		fmt.Printf("Failed to update load balancer: %v\n", err)
	}
}

// monitorForFailover monitors for failover scenarios
func (hm *HealthMonitor) monitorForFailover(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hm.checkForFailover()
		}
	}
}

// checkForFailover checks if failover is needed
func (hm *HealthMonitor) checkForFailover() {
	metrics := hm.collectMetrics()

	// If this instance is unhealthy, trigger failover
	if !metrics.Healthy {
		fmt.Printf("Instance %s is unhealthy, failover may be needed\n", hm.instanceID)
		// In production, this would trigger session migration to healthy instances
	}

	// Check if other instances need help
	instances := hm.loadBalancer.GetInstances()
	for _, instance := range instances {
		instance.mu.RLock()
		overloaded := instance.ActiveSessions > int(float64(instance.MaxSessions)*0.9)
		instance.mu.RUnlock()

		if overloaded {
			fmt.Printf("Instance %s is overloaded, consider scaling\n", instance.ID)
			// In production, this would trigger auto-scaling
		}
	}
}

// GetMetrics returns current health metrics
func (hm *HealthMonitor) GetMetrics() *HealthMetrics {
	return hm.collectMetrics()
}

// Shutdown gracefully shuts down the health monitor
func (hm *HealthMonitor) Shutdown(ctx context.Context) error {
	// Deregister from load balancer
	hm.loadBalancer.DeregisterInstance(hm.instanceID)

	// Wait for graceful shutdown
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return nil
	}
}
