package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// Check represents a health check function
type Check func(ctx context.Context) error

// CheckResult represents the result of a health check
type CheckResult struct {
	Name      string        `json:"name"`
	Status    Status        `json:"status"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// HealthResponse represents the overall health response
type HealthResponse struct {
	Status    Status                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
	Checks    map[string]CheckResult `json:"checks"`
	Version   string                 `json:"version,omitempty"`
}

// Checker manages health checks for the service
type Checker struct {
	checks  map[string]Check
	mutex   sync.RWMutex
	timeout time.Duration
	version string
}

// NewChecker creates a new health checker
func NewChecker() *Checker {
	return &Checker{
		checks:  make(map[string]Check),
		timeout: 5 * time.Second,
		version: "1.0.0",
	}
}

// AddCheck adds a health check
func (c *Checker) AddCheck(name string, check Check) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.checks[name] = check
}

// RemoveCheck removes a health check
func (c *Checker) RemoveCheck(name string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.checks, name)
}

// SetTimeout sets the timeout for health checks
func (c *Checker) SetTimeout(timeout time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.timeout = timeout
}

// SetVersion sets the service version
func (c *Checker) SetVersion(version string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.version = version
}

// Check performs all health checks and returns the overall status
func (c *Checker) Check(ctx context.Context) *HealthResponse {
	start := time.Now()

	c.mutex.RLock()
	checks := make(map[string]Check)
	for name, check := range c.checks {
		checks[name] = check
	}
	timeout := c.timeout
	version := c.version
	c.mutex.RUnlock()

	results := make(map[string]CheckResult)
	overallStatus := StatusHealthy

	// Run all checks concurrently
	var wg sync.WaitGroup
	var resultsMutex sync.Mutex

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check Check) {
			defer wg.Done()

			checkStart := time.Now()
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result := CheckResult{
				Name:      name,
				Status:    StatusHealthy,
				Timestamp: checkStart,
			}

			if err := check(checkCtx); err != nil {
				result.Status = StatusUnhealthy
				result.Error = err.Error()
			}

			result.Duration = time.Since(checkStart)

			resultsMutex.Lock()
			results[name] = result
			if result.Status != StatusHealthy && overallStatus == StatusHealthy {
				overallStatus = StatusUnhealthy
			}
			resultsMutex.Unlock()
		}(name, check)
	}

	wg.Wait()

	return &HealthResponse{
		Status:    overallStatus,
		Timestamp: start,
		Duration:  time.Since(start),
		Checks:    results,
		Version:   version,
	}
}

// Handler returns an HTTP handler for health checks
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		response := c.Check(ctx)

		w.Header().Set("Content-Type", "application/json")

		// Set HTTP status based on health
		if response.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode health response", http.StatusInternalServerError)
		}
	}
}

// ReadinessHandler returns a simple readiness check handler
func (c *Checker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		response := c.Check(ctx)

		if response.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service Unavailable"))
		}
	}
}

// LivenessHandler returns a simple liveness check handler
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

// Common health check functions

// DatabaseHealthCheck creates a health check for database connectivity
func DatabaseHealthCheck(db interface{ Health(context.Context) error }) Check {
	return func(ctx context.Context) error {
		return db.Health(ctx)
	}
}

// RedisHealthCheck creates a health check for Redis connectivity
func RedisHealthCheck(redis interface{ Health(context.Context) error }) Check

// ServiceDiscoveryHealthCheck creates a health check for service discovery
func ServiceDiscoveryHealthCheck(discovery interface{ Health() error }) Check {
	return func(ctx context.Context) error {
		return discovery.Health()
	}
}

// HTTPHealthCheck creates a health check for HTTP endpoints
func HTTPHealthCheck(url string, timeout time.Duration) Check {
	return func(ctx context.Context) error {
		client := &http.Client{Timeout: timeout}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
		}

		return nil
	}
}
