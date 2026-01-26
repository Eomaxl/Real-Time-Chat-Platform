package region

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// LoadBalancer handles regional load balancing
type LoadBalancer struct {
	config        *RegionConfig
	healthChecker *HealthChecker
	strategy      LoadBalancingStrategy
	mu            sync.RWMutex
	metrics       *LoadBalancerMetrics
}

// LoadBalancingStrategy defines load balancing behavior
type LoadBalancingStrategy interface {
	SelectRegion(ctx context.Context, regions []Region, metrics *LoadBalancerMetrics) (*Region, error)
}

// RoundRobinLB implements round-robin load balancing
type RoundRobinLB struct {
	counter uint64
}

// LeastConnectionsLB selects region with fewest connections
type LeastConnectionsLB struct {
	connections map[string]*atomic.Int64
	mu          sync.RWMutex
}

// WeightedRoundRobinLB implements weighted round-robin
type WeightedRoundRobinLB struct {
	weights map[string]int
	counter uint64
	mu      sync.RWMutex
}

// LoadBalancerMetrics tracks load balancing metrics
type LoadBalancerMetrics struct {
	mu                   sync.RWMutex
	requestsPerRegion    map[string]int64
	connectionsPerRegion map[string]int64
	lastRequestTime      time.Time
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(config *RegionConfig, healthChecker *HealthChecker) *LoadBalancer {
	return &LoadBalancer{
		config:        config,
		healthChecker: healthChecker,
		strategy:      &RoundRobinLB{},
		metrics:       NewLoadBalancerMetrics(),
	}
}

// NewLoadBalancerMetrics creates new metrics tracker
func NewLoadBalancerMetrics() *LoadBalancerMetrics {
	return &LoadBalancerMetrics{
		requestsPerRegion:    make(map[string]int64),
		connectionsPerRegion: make(map[string]int64),
	}
}

// SetStrategy sets the load balancing strategy
func (lb *LoadBalancer) SetStrategy(strategy LoadBalancingStrategy) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.strategy = strategy
}

// SelectRegion selects a region for a request
func (lb *LoadBalancer) SelectRegion(ctx context.Context) (*Region, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Get healthy regions
	healthyRegions := lb.getHealthyRegions()
	if len(healthyRegions) == 0 {
		return nil, fmt.Errorf("no healthy regions available")
	}

	// Use strategy to select region
	region, err := lb.strategy.SelectRegion(ctx, healthyRegions, lb.metrics)
	if err != nil {
		return nil, err
	}

	// Record request
	lb.metrics.RecordRequest(region.Code)

	return region, nil
}

// getHealthyRegions returns all healthy regions
func (lb *LoadBalancer) getHealthyRegions() []Region {
	healthy := make([]Region, 0)

	for _, region := range lb.config.Regions {
		if lb.healthChecker == nil || lb.healthChecker.IsRegionHealthy(region.Code) {
			healthy = append(healthy, region)
		}
	}

	return healthy
}

// RecordConnection records a new connection to a region
func (lb *LoadBalancer) RecordConnection(regionCode string) {
	lb.metrics.RecordConnection(regionCode)
}

// ReleaseConnection releases a connection from a region
func (lb *LoadBalancer) ReleaseConnection(regionCode string) {
	lb.metrics.ReleaseConnection(regionCode)
}

// GetMetrics returns load balancer metrics
func (lb *LoadBalancer) GetMetrics() map[string]interface{} {
	return lb.metrics.GetMetrics()
}

// SelectRegion implements RoundRobinLB
func (rr *RoundRobinLB) SelectRegion(ctx context.Context, regions []Region, metrics *LoadBalancerMetrics) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	index := atomic.AddUint64(&rr.counter, 1) % uint64(len(regions))
	return &regions[index], nil
}

// SelectRegion implements LeastConnectionsLB
func (lc *LeastConnectionsLB) SelectRegion(ctx context.Context, regions []Region, metrics *LoadBalancerMetrics) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	lc.mu.RLock()
	defer lc.mu.RUnlock()

	var bestRegion *Region
	var minConnections int64 = -1

	for i := range regions {
		connections := metrics.GetConnections(regions[i].Code)
		if minConnections == -1 || connections < minConnections {
			minConnections = connections
			bestRegion = &regions[i]
		}
	}

	return bestRegion, nil
}

// SelectRegion implements WeightedRoundRobinLB
func (wrr *WeightedRoundRobinLB) SelectRegion(ctx context.Context, regions []Region, metrics *LoadBalancerMetrics) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	wrr.mu.RLock()
	defer wrr.mu.RUnlock()

	// Calculate total weight
	totalWeight := 0
	for _, region := range regions {
		weight := wrr.getWeight(region.Code)
		totalWeight += weight
	}

	if totalWeight == 0 {
		return &regions[0], nil
	}

	// Select based on weight
	counter := atomic.AddUint64(&wrr.counter, 1)
	position := int(counter % uint64(totalWeight))
	currentWeight := 0

	for i := range regions {
		weight := wrr.getWeight(regions[i].Code)
		currentWeight += weight
		if position < currentWeight {
			return &regions[i], nil
		}
	}

	return &regions[0], nil
}

// getWeight returns the weight for a region
func (wrr *WeightedRoundRobinLB) getWeight(regionCode string) int {
	weight, ok := wrr.weights[regionCode]
	if !ok {
		return 1 // Default weight
	}
	return weight
}

// SetWeight sets the weight for a region
func (wrr *WeightedRoundRobinLB) SetWeight(regionCode string, weight int) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()
	if wrr.weights == nil {
		wrr.weights = make(map[string]int)
	}
	wrr.weights[regionCode] = weight
}

// RecordRequest records a request to a region
func (m *LoadBalancerMetrics) RecordRequest(regionCode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsPerRegion[regionCode]++
	m.lastRequestTime = time.Now()
}

// RecordConnection records a connection to a region
func (m *LoadBalancerMetrics) RecordConnection(regionCode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionsPerRegion[regionCode]++
}

// ReleaseConnection releases a connection from a region
func (m *LoadBalancerMetrics) ReleaseConnection(regionCode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectionsPerRegion[regionCode] > 0 {
		m.connectionsPerRegion[regionCode]--
	}
}

// GetConnections returns the number of connections for a region
func (m *LoadBalancerMetrics) GetConnections(regionCode string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectionsPerRegion[regionCode]
}

// GetMetrics returns all metrics
func (m *LoadBalancerMetrics) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"requests_per_region":    m.requestsPerRegion,
		"connections_per_region": m.connectionsPerRegion,
		"last_request_time":      m.lastRequestTime,
	}
}
