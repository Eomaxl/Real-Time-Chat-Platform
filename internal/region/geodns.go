package region

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"time"
)

// GeoDNSRouter handles geographic DNS routing
type GeoDNSRouter struct {
	config        *RegionConfig
	geoIPResolver *GeoIPResolver
	healthChecker *HealthChecker
	mu            sync.RWMutex
}

// GeoIPResolver resolves IP addresses to geographic locations
type GeoIPResolver struct {
	ipToLocation map[string]*GeoLocation
	mu           sync.RWMutex
}

// GeoLocation represents a geographic location
type GeoLocation struct {
	IP        string
	Country   string
	Region    string
	City      string
	Latitude  float64
	Longitude float64
	Timezone  string
}

// RegionDistance represents the distance to a region
type RegionDistance struct {
	Region   *Region
	Distance float64
	Latency  time.Duration
}

// NewGeoDNSRouter creates a new GeoDNS router
func NewGeoDNSRouter(config *RegionConfig, healthChecker *HealthChecker) *GeoDNSRouter {
	return &GeoDNSRouter{
		config:        config,
		geoIPResolver: NewGeoIPResolver(),
		healthChecker: healthChecker,
	}
}

// NewGeoIPResolver creates a new GeoIP resolver
func NewGeoIPResolver() *GeoIPResolver {
	return &GeoIPResolver{
		ipToLocation: make(map[string]*GeoLocation),
	}
}

// RouteByIP routes a request based on client IP address
func (gr *GeoDNSRouter) RouteByIP(ctx context.Context, clientIP string) (*Region, error) {
	// Resolve IP to location
	location, err := gr.geoIPResolver.ResolveIP(clientIP)
	if err != nil {
		// Fallback to current region if resolution fails
		return gr.config.GetCurrentRegion()
	}

	// Find nearest healthy region
	return gr.findNearestHealthyRegion(location)
}

// RouteByLocation routes based on geographic location
func (gr *GeoDNSRouter) RouteByLocation(ctx context.Context, location *GeoLocation) (*Region, error) {
	return gr.findNearestHealthyRegion(location)
}

// findNearestHealthyRegion finds the nearest healthy region to a location
func (gr *GeoDNSRouter) findNearestHealthyRegion(location *GeoLocation) (*Region, error) {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	var nearestRegion *Region
	var minDistance float64 = math.MaxFloat64

	for _, region := range gr.config.Regions {
		// Check if region is healthy
		if gr.healthChecker != nil && !gr.healthChecker.IsRegionHealthy(region.Code) {
			continue
		}

		// Calculate distance (simplified - in production use actual region coordinates)
		distance := gr.calculateDistance(location, &region)

		if distance < minDistance {
			minDistance = distance
			regionCopy := region
			nearestRegion = &regionCopy
		}
	}

	if nearestRegion == nil {
		return nil, fmt.Errorf("no healthy regions available")
	}

	return nearestRegion, nil
}

// calculateDistance calculates distance between location and region
func (gr *GeoDNSRouter) calculateDistance(location *GeoLocation, region *Region) float64 {
	// This is a simplified calculation
	// In production, you would:
	// 1. Store actual coordinates for each region
	// 2. Use Haversine formula for accurate distance
	// 3. Consider network topology and latency measurements

	// For now, use a simple heuristic based on region codes
	regionDistances := map[string]float64{
		"us-east":  1000,
		"us-west":  2000,
		"eu-west":  5000,
		"ap-south": 8000,
	}

	distance, ok := regionDistances[region.Code]
	if !ok {
		return 5000 // Default distance
	}

	return distance
}

// ResolveIP resolves an IP address to a geographic location
func (gr *GeoIPResolver) ResolveIP(ip string) (*GeoLocation, error) {
	gr.mu.RLock()
	location, ok := gr.ipToLocation[ip]
	gr.mu.RUnlock()

	if ok {
		return location, nil
	}

	// In production, use a GeoIP database like MaxMind GeoIP2
	// For now, return a default location
	location = &GeoLocation{
		IP:        ip,
		Country:   "US",
		Region:    "East",
		City:      "New York",
		Latitude:  40.7128,
		Longitude: -74.0060,
		Timezone:  "America/New_York",
	}

	// Cache the result
	gr.mu.Lock()
	gr.ipToLocation[ip] = location
	gr.mu.Unlock()

	return location, nil
}

// UpdateLocation updates the cached location for an IP
func (gr *GeoIPResolver) UpdateLocation(ip string, location *GeoLocation) {
	gr.mu.Lock()
	defer gr.mu.Unlock()
	gr.ipToLocation[ip] = location
}

// GetRegionDistances returns distances to all regions from a location
func (gr *GeoDNSRouter) GetRegionDistances(location *GeoLocation) []RegionDistance {
	gr.mu.RLock()
	defer gr.mu.RUnlock()

	distances := make([]RegionDistance, 0, len(gr.config.Regions))

	for _, region := range gr.config.Regions {
		distance := gr.calculateDistance(location, &region)
		regionCopy := region

		distances = append(distances, RegionDistance{
			Region:   &regionCopy,
			Distance: distance,
		})
	}

	return distances
}

// MeasureLatency measures actual network latency to a region
func (gr *GeoDNSRouter) MeasureLatency(ctx context.Context, region *Region) (time.Duration, error) {
	if region.HealthURL == "" {
		return 0, fmt.Errorf("no health URL configured for region %s", region.Code)
	}

	// Extract host from health URL
	// In production, use proper URL parsing
	host := region.HealthURL

	start := time.Now()

	// Perform TCP connection test
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to region %s: %w", region.Code, err)
	}
	defer conn.Close()

	latency := time.Since(start)
	return latency, nil
}

// GetOptimalRegion returns the optimal region based on distance and latency
func (gr *GeoDNSRouter) GetOptimalRegion(ctx context.Context, clientIP string) (*Region, error) {
	// Resolve IP to location
	location, err := gr.geoIPResolver.ResolveIP(clientIP)
	if err != nil {
		return gr.config.GetCurrentRegion()
	}

	// Get region distances
	distances := gr.GetRegionDistances(location)

	// Filter healthy regions and measure latency
	var bestRegion *Region
	var bestScore float64 = math.MaxFloat64

	for _, rd := range distances {
		// Check health
		if gr.healthChecker != nil && !gr.healthChecker.IsRegionHealthy(rd.Region.Code) {
			continue
		}

		// Measure latency
		latency, err := gr.MeasureLatency(ctx, rd.Region)
		if err != nil {
			continue
		}

		// Calculate score (weighted combination of distance and latency)
		// Lower is better
		score := rd.Distance*0.3 + float64(latency.Milliseconds())*0.7

		if score < bestScore {
			bestScore = score
			bestRegion = rd.Region
		}
	}

	if bestRegion == nil {
		return gr.config.GetCurrentRegion()
	}

	return bestRegion, nil
}

// GetStats returns GeoDNS routing statistics
func (gr *GeoDNSRouter) GetStats() map[string]interface{} {
	gr.geoIPResolver.mu.RLock()
	cachedIPs := len(gr.geoIPResolver.ipToLocation)
	gr.geoIPResolver.mu.RUnlock()

	return map[string]interface{}{
		"cached_ips":     cachedIPs,
		"total_regions":  len(gr.config.Regions),
		"routing_method": "geodns",
	}
}
