package region

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// TrafficRouter handles routing requests to appropriate regions
type TrafficRouter struct {
	config          *RegionConfig
	failoverManager *FailoverManager
	routingStrategy RoutingStrategy
	mu              sync.RWMutex
}

// RoutingStrategy defines how traffic is routed between regions
type RoutingStrategy interface {
	SelectRegion(ctx context.Context, regions []Region) (*Region, error)
}

// LatencyBasedStrategy routes to the region with lowest latency
type LatencyBasedStrategy struct {
	latencyCache map[string]time.Duration
	mu           sync.RWMutex
}

// GeographicStrategy routes based on geographic proximity
type GeographicStrategy struct {
	userRegionMap map[string]string // user_id -> preferred_region
	mu            sync.RWMutex
}

// RoundRobinStrategy distributes traffic evenly across regions
type RoundRobinStrategy struct {
	counter uint64
	mu      sync.Mutex
}

// WeightedStrategy routes based on region weights
type WeightedStrategy struct {
	weights map[string]int
	mu      sync.RWMutex
}

// NewTrafficRouter creates a new traffic router
func NewTrafficRouter(config *RegionConfig, failoverManager *FailoverManager) *TrafficRouter {
	tr := &TrafficRouter{
		config:          config,
		failoverManager: failoverManager,
	}

	// Default to latency-based routing
	tr.routingStrategy = &LatencyBasedStrategy{
		latencyCache: make(map[string]time.Duration),
	}

	return tr
}

// SetRoutingStrategy sets the routing strategy
func (tr *TrafficRouter) SetRoutingStrategy(strategy RoutingStrategy) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.routingStrategy = strategy
}

// RouteRequest routes a request to the appropriate region
func (tr *TrafficRouter) RouteRequest(ctx context.Context) (*Region, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	// Get healthy regions
	healthyRegions := tr.getHealthyRegions()
	if len(healthyRegions) == 0 {
		return nil, fmt.Errorf("no healthy regions available")
	}

	// Use routing strategy to select region
	return tr.routingStrategy.SelectRegion(ctx, healthyRegions)
}

// RouteToNearestRegion routes to the geographically nearest region
func (tr *TrafficRouter) RouteToNearestRegion(ctx context.Context, userLocation string) (*Region, error) {
	// This would use geographic data to find the nearest region
	// For now, return current region
	return tr.config.GetCurrentRegion()
}

// RouteToRegion routes to a specific region
func (tr *TrafficRouter) RouteToRegion(regionCode string) (*Region, error) {
	return tr.config.GetRegionByCode(regionCode)
}

// getHealthyRegions returns all healthy regions
func (tr *TrafficRouter) getHealthyRegions() []Region {
	if tr.failoverManager == nil {
		return tr.config.GetAllRegions()
	}

	healthStatus := tr.failoverManager.GetHealthStatus()
	healthy := make([]Region, 0)

	for code, region := range tr.config.Regions {
		status, ok := healthStatus[code]
		if !ok || status.IsHealthy {
			healthy = append(healthy, region)
		}
	}

	return healthy
}

// UpdateLatency updates the latency measurement for a region
func (tr *TrafficRouter) UpdateLatency(regionCode string, latency time.Duration) {
	if strategy, ok := tr.routingStrategy.(*LatencyBasedStrategy); ok {
		strategy.UpdateLatency(regionCode, latency)
	}
}

// SelectRegion implements LatencyBasedStrategy
func (s *LatencyBasedStrategy) SelectRegion(ctx context.Context, regions []Region) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var bestRegion *Region
	var bestLatency time.Duration

	for i := range regions {
		region := &regions[i]
		latency, ok := s.latencyCache[region.Code]

		if !ok {
			// No latency data - use this region
			return region, nil
		}

		if bestRegion == nil || latency < bestLatency {
			bestRegion = region
			bestLatency = latency
		}
	}

	if bestRegion == nil {
		return &regions[0], nil
	}

	return bestRegion, nil
}

// UpdateLatency updates the latency cache
func (s *LatencyBasedStrategy) UpdateLatency(regionCode string, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latencyCache[regionCode] = latency
}

// SelectRegion implements GeographicStrategy
func (s *GeographicStrategy) SelectRegion(ctx context.Context, regions []Region) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	// Extract user ID from context
	userID, ok := ctx.Value("user_id").(string)
	if !ok {
		// No user ID - return first region
		return &regions[0], nil
	}

	s.mu.RLock()
	preferredRegion, ok := s.userRegionMap[userID]
	s.mu.RUnlock()

	if !ok {
		// No preference - return first region
		return &regions[0], nil
	}

	// Find preferred region
	for i := range regions {
		if regions[i].Code == preferredRegion {
			return &regions[i], nil
		}
	}

	// Preferred region not available - return first
	return &regions[0], nil
}

// SetUserRegion sets the preferred region for a user
func (s *GeographicStrategy) SetUserRegion(userID, regionCode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userRegionMap[userID] = regionCode
}

// SelectRegion implements RoundRobinStrategy
func (s *RoundRobinStrategy) SelectRegion(ctx context.Context, regions []Region) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.counter % uint64(len(regions))
	s.counter++

	return &regions[index], nil
}

// SelectRegion implements WeightedStrategy
func (s *WeightedStrategy) SelectRegion(ctx context.Context, regions []Region) (*Region, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions available")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate total weight
	totalWeight := 0
	for _, region := range regions {
		weight, ok := s.weights[region.Code]
		if !ok {
			weight = 1 // Default weight
		}
		totalWeight += weight
	}

	if totalWeight == 0 {
		return &regions[0], nil
	}

	// Select based on weight
	random := rand.Intn(totalWeight)
	currentWeight := 0

	for i := range regions {
		weight, ok := s.weights[regions[i].Code]
		if !ok {
			weight = 1
		}
		currentWeight += weight

		if random < currentWeight {
			return &regions[i], nil
		}
	}

	return &regions[0], nil
}

// SetWeight sets the weight for a region
func (s *WeightedStrategy) SetWeight(regionCode string, weight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.weights[regionCode] = weight
}

// GetRoutingStats returns routing statistics
func (tr *TrafficRouter) GetRoutingStats() map[string]interface{} {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	stats := map[string]interface{}{
		"strategy": fmt.Sprintf("%T", tr.routingStrategy),
		"regions":  len(tr.config.Regions),
	}

	// Add strategy-specific stats
	switch strategy := tr.routingStrategy.(type) {
	case *LatencyBasedStrategy:
		strategy.mu.RLock()
		stats["latency_cache"] = strategy.latencyCache
		strategy.mu.RUnlock()
	case *GeographicStrategy:
		strategy.mu.RLock()
		stats["user_mappings"] = len(strategy.userRegionMap)
		strategy.mu.RUnlock()
	case *WeightedStrategy:
		strategy.mu.RLock()
		stats["weights"] = strategy.weights
		strategy.mu.RUnlock()
	}

	return stats
}
