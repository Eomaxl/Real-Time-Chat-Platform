package sfu

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SFUInstance represents an SFU service instance
type SFUInstance struct {
	ID              string
	Address         string
	Port            string
	Region          string
	ActiveSessions  int
	MaxSessions     int
	CPUUsage        float64
	MemoryUsage     float64
	BandwidthUsage  uint64
	LastHealthCheck time.Time
	Healthy         bool
	mu              sync.RWMutex
}

// LoadBalancer manages SFU instance selection and load balancing
type LoadBalancer struct {
	instances   map[string]*SFUInstance
	mu          sync.RWMutex
	strategy    LoadBalancingStrategy
	healthCheck *HealthChecker
}

// LoadBalancingStrategy defines the strategy for selecting SFU instances
type LoadBalancingStrategy string

const (
	StrategyRoundRobin    LoadBalancingStrategy = "round-robin"
	StrategyLeastLoaded   LoadBalancingStrategy = "least-loaded"
	StrategyRegionBased   LoadBalancingStrategy = "region-based"
	StrategyWeightedRound LoadBalancingStrategy = "weighted-round-robin"
)

// HealthChecker performs health checks on SFU instances
type HealthChecker struct {
	interval      time.Duration
	timeout       time.Duration
	failThreshold int
	mu            sync.RWMutex
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(strategy LoadBalancingStrategy) *LoadBalancer {
	return &LoadBalancer{
		instances: make(map[string]*SFUInstance),
		strategy:  strategy,
		healthCheck: &HealthChecker{
			interval:      10 * time.Second,
			timeout:       5 * time.Second,
			failThreshold: 3,
		},
	}
}

// RegisterInstance registers a new SFU instance
func (lb *LoadBalancer) RegisterInstance(instance *SFUInstance) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.instances[instance.ID] = instance
}

// DeregisterInstance removes an SFU instance
func (lb *LoadBalancer) DeregisterInstance(instanceID string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	delete(lb.instances, instanceID)
}

// SelectInstance selects an SFU instance based on the load balancing strategy
func (lb *LoadBalancer) SelectInstance(ctx context.Context, region string) (*SFUInstance, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if len(lb.instances) == 0 {
		return nil, fmt.Errorf("no SFU instances available")
	}

	switch lb.strategy {
	case StrategyLeastLoaded:
		return lb.selectLeastLoaded(region)
	case StrategyRegionBased:
		return lb.selectByRegion(region)
	case StrategyWeightedRound:
		return lb.selectWeightedRoundRobin(region)
	case StrategyRoundRobin:
		fallthrough
	default:
		return lb.selectRoundRobin()
	}
}

// selectRoundRobin selects an instance using round-robin
func (lb *LoadBalancer) selectRoundRobin() (*SFUInstance, error) {
	var healthyInstances []*SFUInstance

	for _, instance := range lb.instances {
		instance.mu.RLock()
		if instance.Healthy && instance.ActiveSessions < instance.MaxSessions {
			healthyInstances = append(healthyInstances, instance)
		}
		instance.mu.RUnlock()
	}

	if len(healthyInstances) == 0 {
		return nil, fmt.Errorf("no healthy SFU instances available")
	}

	// Simple round-robin: return first available
	return healthyInstances[0], nil
}

// selectLeastLoaded selects the instance with the least load
func (lb *LoadBalancer) selectLeastLoaded(region string) (*SFUInstance, error) {
	var bestInstance *SFUInstance
	var lowestLoad float64 = 1.0

	for _, instance := range lb.instances {
		instance.mu.RLock()

		// Skip unhealthy or full instances
		if !instance.Healthy || instance.ActiveSessions >= instance.MaxSessions {
			instance.mu.RUnlock()
			continue
		}

		// Calculate load score (0.0 = no load, 1.0 = full load)
		sessionLoad := float64(instance.ActiveSessions) / float64(instance.MaxSessions)
		cpuLoad := instance.CPUUsage / 100.0
		memoryLoad := instance.MemoryUsage / 100.0

		// Weighted average of different load metrics
		totalLoad := (sessionLoad * 0.5) + (cpuLoad * 0.3) + (memoryLoad * 0.2)

		instance.mu.RUnlock()

		if bestInstance == nil || totalLoad < lowestLoad {
			bestInstance = instance
			lowestLoad = totalLoad
		}
	}

	if bestInstance == nil {
		return nil, fmt.Errorf("no suitable SFU instance found")
	}

	return bestInstance, nil
}

// selectByRegion selects an instance in the specified region
func (lb *LoadBalancer) selectByRegion(region string) (*SFUInstance, error) {
	var regionalInstances []*SFUInstance

	for _, instance := range lb.instances {
		instance.mu.RLock()
		if instance.Region == region && instance.Healthy && instance.ActiveSessions < instance.MaxSessions {
			regionalInstances = append(regionalInstances, instance)
		}
		instance.mu.RUnlock()
	}

	if len(regionalInstances) == 0 {
		// Fallback to any healthy instance
		return lb.selectLeastLoaded("")
	}

	// Among regional instances, select least loaded
	var bestInstance *SFUInstance
	var lowestLoad float64 = 1.0

	for _, instance := range regionalInstances {
		instance.mu.RLock()
		sessionLoad := float64(instance.ActiveSessions) / float64(instance.MaxSessions)
		instance.mu.RUnlock()

		if bestInstance == nil || sessionLoad < lowestLoad {
			bestInstance = instance
			lowestLoad = sessionLoad
		}
	}

	return bestInstance, nil
}

// selectWeightedRoundRobin selects an instance using weighted round-robin
func (lb *LoadBalancer) selectWeightedRoundRobin(region string) (*SFUInstance, error) {
	var candidates []*SFUInstance
	var weights []int

	for _, instance := range lb.instances {
		instance.mu.RLock()

		if !instance.Healthy || instance.ActiveSessions >= instance.MaxSessions {
			instance.mu.RUnlock()
			continue
		}

		// Calculate weight based on available capacity
		availableCapacity := instance.MaxSessions - instance.ActiveSessions
		weight := availableCapacity

		// Boost weight for instances in the same region
		if region != "" && instance.Region == region {
			weight *= 2
		}

		candidates = append(candidates, instance)
		weights = append(weights, weight)

		instance.mu.RUnlock()
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable SFU instance found")
	}

	// Select based on weights (simple implementation: highest weight)
	maxWeight := 0
	selectedIndex := 0
	for i, weight := range weights {
		if weight > maxWeight {
			maxWeight = weight
			selectedIndex = i
		}
	}

	return candidates[selectedIndex], nil
}

// UpdateInstanceLoad updates the load metrics for an instance
func (lb *LoadBalancer) UpdateInstanceLoad(instanceID string, activeSessions int, cpuUsage, memoryUsage float64, bandwidthUsage uint64) error {
	lb.mu.RLock()
	instance, ok := lb.instances[instanceID]
	lb.mu.RUnlock()

	if !ok {
		return fmt.Errorf("instance not found: %s", instanceID)
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.ActiveSessions = activeSessions
	instance.CPUUsage = cpuUsage
	instance.MemoryUsage = memoryUsage
	instance.BandwidthUsage = bandwidthUsage

	return nil
}

// GetInstances returns all registered instances
func (lb *LoadBalancer) GetInstances() []*SFUInstance {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	instances := make([]*SFUInstance, 0, len(lb.instances))
	for _, instance := range lb.instances {
		instances = append(instances, instance)
	}
	return instances
}

// GetHealthyInstances returns all healthy instances
func (lb *LoadBalancer) GetHealthyInstances() []*SFUInstance {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	var healthy []*SFUInstance
	for _, instance := range lb.instances {
		instance.mu.RLock()
		if instance.Healthy {
			healthy = append(healthy, instance)
		}
		instance.mu.RUnlock()
	}
	return healthy
}

// StartHealthChecks starts periodic health checks for all instances
func (lb *LoadBalancer) StartHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(lb.healthCheck.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lb.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks performs health checks on all instances
func (lb *LoadBalancer) performHealthChecks(ctx context.Context) {
	lb.mu.RLock()
	instances := make([]*SFUInstance, 0, len(lb.instances))
	for _, instance := range lb.instances {
		instances = append(instances, instance)
	}
	lb.mu.RUnlock()

	for _, instance := range instances {
		go lb.checkInstanceHealth(ctx, instance)
	}
}

// checkInstanceHealth performs a health check on a single instance
func (lb *LoadBalancer) checkInstanceHealth(ctx context.Context, instance *SFUInstance) {
	checkCtx, cancel := context.WithTimeout(ctx, lb.healthCheck.timeout)
	defer cancel()

	// Perform health check (simplified - in production, make HTTP request to health endpoint)
	healthy := lb.performHealthCheckRequest(checkCtx, instance)

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.Healthy = healthy
	instance.LastHealthCheck = time.Now()
}

// performHealthCheckRequest performs the actual health check request
func (lb *LoadBalancer) performHealthCheckRequest(ctx context.Context, instance *SFUInstance) bool {
	// In a real implementation, this would make an HTTP request to the instance's health endpoint
	// For now, we'll simulate a health check

	// Check if instance was recently updated (within last 30 seconds)
	instance.mu.RLock()
	lastCheck := instance.LastHealthCheck
	instance.mu.RUnlock()

	// If no recent health check, assume healthy for now
	if time.Since(lastCheck) > 30*time.Second {
		return true
	}

	return true
}

// GetLoadBalancingStats returns statistics about load balancing
func (lb *LoadBalancer) GetLoadBalancingStats() map[string]interface{} {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	totalInstances := len(lb.instances)
	healthyInstances := 0
	totalSessions := 0
	totalCapacity := 0

	for _, instance := range lb.instances {
		instance.mu.RLock()
		if instance.Healthy {
			healthyInstances++
		}
		totalSessions += instance.ActiveSessions
		totalCapacity += instance.MaxSessions
		instance.mu.RUnlock()
	}

	utilizationRate := 0.0
	if totalCapacity > 0 {
		utilizationRate = float64(totalSessions) / float64(totalCapacity) * 100
	}

	return map[string]interface{}{
		"total_instances":   totalInstances,
		"healthy_instances": healthyInstances,
		"total_sessions":    totalSessions,
		"total_capacity":    totalCapacity,
		"utilization_rate":  utilizationRate,
		"strategy":          lb.strategy,
	}
}

// IncrementSessionCount increments the active session count for an instance
func (instance *SFUInstance) IncrementSessionCount() {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	instance.ActiveSessions++
}

// DecrementSessionCount decrements the active session count for an instance
func (instance *SFUInstance) DecrementSessionCount() {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.ActiveSessions > 0 {
		instance.ActiveSessions--
	}
}

// GetLoad returns the current load percentage of the instance
func (instance *SFUInstance) GetLoad() float64 {
	instance.mu.RLock()
	defer instance.mu.RUnlock()

	if instance.MaxSessions == 0 {
		return 0.0
	}

	return float64(instance.ActiveSessions) / float64(instance.MaxSessions) * 100
}

// IsAvailable checks if the instance is available for new sessions
func (instance *SFUInstance) IsAvailable() bool {
	instance.mu.RLock()
	defer instance.mu.RUnlock()

	return instance.Healthy && instance.ActiveSessions < instance.MaxSessions
}
