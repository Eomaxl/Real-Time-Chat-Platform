package region

import (
	"fmt"
	"time"
)

// Region represents a geographic region
type Region struct {
	Name         string        `json:"name"`
	Code         string        `json:"code"` // e.g., "us-east", "eu-west", "ap-south"
	IsPrimary    bool          `json:"isPrimary"`
	DatabaseURLs []string      `json:"databaseUrls"`
	RedisURLs    []string      `json:"redisUrls"`
	KafkaURLs    []string      `json:"kafkaUrls"`
	Priority     int           `json:"priority"` // For failover ordering
	HealthURL    string        `json:"healthUrl"`
	Latency      time.Duration `json:"-"` // Measured latency to this region
}

// RegionConfig holds multi-region configuration
type RegionConfig struct {
	CurrentRegion       string            `json:"currentRegion"`
	Regions             map[string]Region `json:"regions"`
	ReplicationMode     string            `json:"replicationMode"` // "async", "sync", "semi-sync"
	FailoverEnabled     bool              `json:"failoverEnabled"`
	FailoverTimeout     time.Duration     `json:"failoverTimeout"`
	HealthCheckInterval time.Duration     `json:"healthCheckInterval"`
	ConflictResolution  string            `json:"conflictResolution"` // "last-write-wins", "vector-clock", "custom"
}

// GetCurrentRegion returns the current region configuration
func (rc *RegionConfig) GetCurrentRegion() (*Region, error) {
	region, ok := rc.Regions[rc.CurrentRegion]
	if !ok {
		return nil, fmt.Errorf("current region %s not found in configuration", rc.CurrentRegion)
	}
	return &region, nil
}

// GetPrimaryRegion returns the primary region
func (rc *RegionConfig) GetPrimaryRegion() (*Region, error) {
	for _, region := range rc.Regions {
		if region.IsPrimary {
			return &region, nil
		}
	}
	return nil, fmt.Errorf("no primary region configured")
}

// GetRegionByCode returns a region by its code
func (rc *RegionConfig) GetRegionByCode(code string) (*Region, error) {
	region, ok := rc.Regions[code]
	if !ok {
		return nil, fmt.Errorf("region %s not found", code)
	}
	return &region, nil
}

// GetAllRegions returns all configured regions
func (rc *RegionConfig) GetAllRegions() []Region {
	regions := make([]Region, 0, len(rc.Regions))
	for _, region := range rc.Regions {
		regions = append(regions, region)
	}
	return regions
}

// GetSecondaryRegions returns all non-primary regions
func (rc *RegionConfig) GetSecondaryRegions() []Region {
	regions := make([]Region, 0)
	for _, region := range rc.Regions {
		if !region.IsPrimary {
			regions = append(regions, region)
		}
	}
	return regions
}

// IsMultiRegion returns true if multiple regions are configured
func (rc *RegionConfig) IsMultiRegion() bool {
	return len(rc.Regions) > 1
}

// DefaultRegionConfig returns a default single-region configuration
func DefaultRegionConfig() *RegionConfig {
	return &RegionConfig{
		CurrentRegion: "us-east",
		Regions: map[string]Region{
			"us-east": {
				Name:      "US East",
				Code:      "us-east",
				IsPrimary: true,
				Priority:  1,
			},
		},
		ReplicationMode:     "async",
		FailoverEnabled:     false,
		FailoverTimeout:     30 * time.Second,
		HealthCheckInterval: 10 * time.Second,
		ConflictResolution:  "last-write-wins",
	}
}
