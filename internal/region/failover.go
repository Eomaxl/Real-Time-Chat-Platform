package region

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// FailoverManager handles regional failover and traffic routing
type FailoverManager struct {
	config           *RegionConfig
	healthChecker    *HealthChecker
	currentRegion    *Region
	activeRegions    map[string]*Region
	mu               sync.RWMutex
	stopCh           chan struct{}
	wg               sync.WaitGroup
	failoverCallback func(from, to *Region)
}

// HealthChecker monitors region health
type HealthChecker struct {
	config       *RegionConfig
	healthStatus map[string]*HealthStatus
	mu           sync.RWMutex
	httpClient   *http.Client
}

// HealthStatus represents the health status of a region
type HealthStatus struct {
	Region       string
	IsHealthy    bool
	LastCheck    time.Time
	LastSuccess  time.Time
	FailureCount int
	Latency      time.Duration
	ErrorMessage string
}

// NewFailoverManager creates a new failover manager
func NewFailoverManager(config *RegionConfig) (*FailoverManager, error) {
	currentRegion, err := config.GetCurrentRegion()
	if err != nil {
		return nil, err
	}

	fm := &FailoverManager{
		config:        config,
		currentRegion: currentRegion,
		activeRegions: make(map[string]*Region),
		stopCh:        make(chan struct{}),
	}

	// Initialize health checker
	fm.healthChecker = NewHealthChecker(config)

	// Initialize active regions
	for code, region := range config.Regions {
		regionCopy := region
		fm.activeRegions[code] = &regionCopy
	}

	return fm, nil
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(config *RegionConfig) *HealthChecker {
	return &HealthChecker{
		config:       config,
		healthStatus: make(map[string]*HealthStatus),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Start begins health checking and failover monitoring
func (fm *FailoverManager) Start(ctx context.Context) {
	if !fm.config.FailoverEnabled {
		return
	}

	fm.wg.Add(1)
	go fm.healthCheckWorker(ctx)
}

// Stop stops the failover manager
func (fm *FailoverManager) Stop() {
	close(fm.stopCh)
	fm.wg.Wait()
}

// healthCheckWorker periodically checks region health
func (fm *FailoverManager) healthCheckWorker(ctx context.Context) {
	defer fm.wg.Done()

	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fm.stopCh:
			return
		case <-ticker.C:
			fm.performHealthChecks(ctx)
			fm.evaluateFailover(ctx)
		}
	}
}

// performHealthChecks checks health of all regions
func (fm *FailoverManager) performHealthChecks(ctx context.Context) {
	var wg sync.WaitGroup

	for code, region := range fm.activeRegions {
		wg.Add(1)
		go func(regionCode string, r *Region) {
			defer wg.Done()
			status := fm.healthChecker.CheckRegion(ctx, r)
			fm.healthChecker.UpdateStatus(regionCode, status)
		}(code, region)
	}

	wg.Wait()
}

// evaluateFailover determines if failover is needed
func (fm *FailoverManager) evaluateFailover(ctx context.Context) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Check if current region is healthy
	currentStatus := fm.healthChecker.GetStatus(fm.currentRegion.Code)
	if currentStatus == nil || currentStatus.IsHealthy {
		return
	}

	// Current region is unhealthy - find best failover target
	targetRegion := fm.selectFailoverTarget()
	if targetRegion == nil {
		// No healthy regions available
		return
	}

	// Perform failover
	fm.performFailover(ctx, targetRegion)
}

// selectFailoverTarget selects the best region for failover
func (fm *FailoverManager) selectFailoverTarget() *Region {
	candidates := make([]*Region, 0)

	// Collect healthy regions
	for code, region := range fm.activeRegions {
		if code == fm.currentRegion.Code {
			continue
		}

		status := fm.healthChecker.GetStatus(code)
		if status != nil && status.IsHealthy {
			candidates = append(candidates, region)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort by priority (lower is better)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	return candidates[0]
}

// performFailover executes the failover to a new region
func (fm *FailoverManager) performFailover(ctx context.Context, targetRegion *Region) {
	oldRegion := fm.currentRegion
	fm.currentRegion = targetRegion

	// Update config
	fm.config.CurrentRegion = targetRegion.Code

	// Call failover callback if set
	if fm.failoverCallback != nil {
		go fm.failoverCallback(oldRegion, targetRegion)
	}
}

// SetFailoverCallback sets a callback function to be called on failover
func (fm *FailoverManager) SetFailoverCallback(callback func(from, to *Region)) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.failoverCallback = callback
}

// GetCurrentRegion returns the current active region
func (fm *FailoverManager) GetCurrentRegion() *Region {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.currentRegion
}

// GetHealthStatus returns health status for all regions
func (fm *FailoverManager) GetHealthStatus() map[string]*HealthStatus {
	return fm.healthChecker.GetAllStatus()
}

// CheckRegion checks the health of a specific region
func (hc *HealthChecker) CheckRegion(ctx context.Context, region *Region) *HealthStatus {
	status := &HealthStatus{
		Region:    region.Code,
		LastCheck: time.Now(),
	}

	if region.HealthURL == "" {
		// No health URL configured - assume healthy
		status.IsHealthy = true
		status.LastSuccess = time.Now()
		return status
	}

	// Perform HTTP health check
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", region.HealthURL, nil)
	if err != nil {
		status.IsHealthy = false
		status.ErrorMessage = fmt.Sprintf("failed to create request: %v", err)
		return status
	}

	resp, err := hc.httpClient.Do(req)
	if err != nil {
		status.IsHealthy = false
		status.ErrorMessage = fmt.Sprintf("health check failed: %v", err)
		return status
	}
	defer resp.Body.Close()

	status.Latency = time.Since(start)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		status.IsHealthy = true
		status.LastSuccess = time.Now()
	} else {
		status.IsHealthy = false
		status.ErrorMessage = fmt.Sprintf("unhealthy status code: %d", resp.StatusCode)
	}

	return status
}

// UpdateStatus updates the health status for a region
func (hc *HealthChecker) UpdateStatus(regionCode string, status *HealthStatus) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	oldStatus, exists := hc.healthStatus[regionCode]
	if exists && !status.IsHealthy {
		status.FailureCount = oldStatus.FailureCount + 1
	} else if status.IsHealthy {
		status.FailureCount = 0
	}

	hc.healthStatus[regionCode] = status
}

// GetStatus returns the health status for a specific region
func (hc *HealthChecker) GetStatus(regionCode string) *HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.healthStatus[regionCode]
}

// GetAllStatus returns health status for all regions
func (hc *HealthChecker) GetAllStatus() map[string]*HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]*HealthStatus)
	for code, status := range hc.healthStatus {
		statusCopy := *status
		result[code] = &statusCopy
	}
	return result
}

// IsRegionHealthy returns true if the specified region is healthy
func (hc *HealthChecker) IsRegionHealthy(regionCode string) bool {
	status := hc.GetStatus(regionCode)
	return status != nil && status.IsHealthy
}

// GetHealthyRegions returns a list of all healthy regions
func (hc *HealthChecker) GetHealthyRegions() []string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	healthy := make([]string, 0)
	for code, status := range hc.healthStatus {
		if status.IsHealthy {
			healthy = append(healthy, code)
		}
	}
	return healthy
}
