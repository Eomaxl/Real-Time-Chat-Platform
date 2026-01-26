package sfu

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scaler manages dynamic scaling of SFU instances
type Scaler struct {
	loadBalancer       *LoadBalancer
	minInstances       int
	maxInstances       int
	scaleUpThreshold   float64 // Utilization percentage to trigger scale up
	scaleDownThreshold float64 // Utilization percentage to trigger scale down
	cooldownPeriod     time.Duration
	lastScaleAction    time.Time
	mu                 sync.RWMutex
}

// ScalingDecision represents a scaling decision
type ScalingDecision struct {
	Action      ScalingAction
	Reason      string
	CurrentLoad float64
	Timestamp   time.Time
}

// ScalingAction represents the type of scaling action
type ScalingAction string

const (
	ScaleUp   ScalingAction = "scale-up"
	ScaleDown ScalingAction = "scale-down"
	NoAction  ScalingAction = "no-action"
)

// ScalingEvent represents a scaling event
type ScalingEvent struct {
	Action        ScalingAction
	InstanceCount int
	Utilization   float64
	Timestamp     time.Time
	Success       bool
	Error         error
}

// NewScaler creates a new scaler
func NewScaler(loadBalancer *LoadBalancer, minInstances, maxInstances int) *Scaler {
	return &Scaler{
		loadBalancer:       loadBalancer,
		minInstances:       minInstances,
		maxInstances:       maxInstances,
		scaleUpThreshold:   75.0, // Scale up at 75% utilization
		scaleDownThreshold: 25.0, // Scale down at 25% utilization
		cooldownPeriod:     5 * time.Minute,
		lastScaleAction:    time.Now(),
	}
}

// Start starts the auto-scaling loop
func (s *Scaler) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluateScaling(ctx)
		}
	}
}

// evaluateScaling evaluates whether scaling is needed
func (s *Scaler) evaluateScaling(ctx context.Context) {
	decision := s.makeScalingDecision()

	if decision.Action == NoAction {
		return
	}

	// Check cooldown period
	s.mu.RLock()
	timeSinceLastScale := time.Since(s.lastScaleAction)
	s.mu.RUnlock()

	if timeSinceLastScale < s.cooldownPeriod {
		fmt.Printf("Scaling action %s skipped due to cooldown period\n", decision.Action)
		return
	}

	// Execute scaling action
	event := s.executeScaling(ctx, decision)

	if event.Success {
		s.mu.Lock()
		s.lastScaleAction = time.Now()
		s.mu.Unlock()

		fmt.Printf("Scaling action %s completed successfully\n", decision.Action)
	} else {
		fmt.Printf("Scaling action %s failed: %v\n", decision.Action, event.Error)
	}
}

// makeScalingDecision determines if scaling is needed
func (s *Scaler) makeScalingDecision() *ScalingDecision {
	stats := s.loadBalancer.GetLoadBalancingStats()

	utilizationRate := stats["utilization_rate"].(float64)
	healthyInstances := stats["healthy_instances"].(int)

	decision := &ScalingDecision{
		Action:      NoAction,
		CurrentLoad: utilizationRate,
		Timestamp:   time.Now(),
	}

	// Check if we need to scale up
	if utilizationRate > s.scaleUpThreshold && healthyInstances < s.maxInstances {
		decision.Action = ScaleUp
		decision.Reason = fmt.Sprintf("Utilization %.2f%% exceeds threshold %.2f%%", utilizationRate, s.scaleUpThreshold)
		return decision
	}

	// Check if we can scale down
	if utilizationRate < s.scaleDownThreshold && healthyInstances > s.minInstances {
		decision.Action = ScaleDown
		decision.Reason = fmt.Sprintf("Utilization %.2f%% below threshold %.2f%%", utilizationRate, s.scaleDownThreshold)
		return decision
	}

	decision.Reason = fmt.Sprintf("Utilization %.2f%% within acceptable range", utilizationRate)
	return decision
}

// executeScaling executes the scaling action
func (s *Scaler) executeScaling(ctx context.Context, decision *ScalingDecision) *ScalingEvent {
	event := &ScalingEvent{
		Action:      decision.Action,
		Utilization: decision.CurrentLoad,
		Timestamp:   time.Now(),
	}

	instances := s.loadBalancer.GetHealthyInstances()
	event.InstanceCount = len(instances)

	switch decision.Action {
	case ScaleUp:
		err := s.scaleUp(ctx)
		event.Success = err == nil
		event.Error = err
		if err == nil {
			event.InstanceCount++
		}
	case ScaleDown:
		err := s.scaleDown(ctx)
		event.Success = err == nil
		event.Error = err
		if err == nil {
			event.InstanceCount--
		}
	default:
		event.Success = true
	}

	return event
}

// scaleUp adds a new SFU instance
func (s *Scaler) scaleUp(ctx context.Context) error {
	// In a real implementation, this would:
	// 1. Request a new instance from the orchestrator (Kubernetes, ECS, etc.)
	// 2. Wait for the instance to be ready
	// 3. Register it with the load balancer

	fmt.Println("Scaling up: Requesting new SFU instance")

	// Simulate instance creation
	newInstance := &SFUInstance{
		ID:              fmt.Sprintf("sfu-%d", time.Now().Unix()),
		Address:         "localhost",
		Port:            "8084",
		Region:          "us-east",
		ActiveSessions:  0,
		MaxSessions:     50,
		CPUUsage:        0,
		MemoryUsage:     0,
		BandwidthUsage:  0,
		LastHealthCheck: time.Now(),
		Healthy:         true,
	}

	s.loadBalancer.RegisterInstance(newInstance)

	return nil
}

// scaleDown removes an SFU instance
func (s *Scaler) scaleDown(ctx context.Context) error {
	// In a real implementation, this would:
	// 1. Select an instance to remove (least loaded)
	// 2. Drain existing sessions (migrate to other instances)
	// 3. Deregister from load balancer
	// 4. Terminate the instance

	instances := s.loadBalancer.GetHealthyInstances()
	if len(instances) <= s.minInstances {
		return fmt.Errorf("cannot scale down: at minimum instance count")
	}

	// Find the least loaded instance
	var leastLoadedInstance *SFUInstance
	var lowestLoad float64 = 1.0

	for _, instance := range instances {
		instance.mu.RLock()
		load := float64(instance.ActiveSessions) / float64(instance.MaxSessions)
		instance.mu.RUnlock()

		if leastLoadedInstance == nil || load < lowestLoad {
			leastLoadedInstance = instance
			lowestLoad = load
		}
	}

	if leastLoadedInstance == nil {
		return fmt.Errorf("no instance found to scale down")
	}

	fmt.Printf("Scaling down: Removing instance %s\n", leastLoadedInstance.ID)

	// Drain sessions (in production, migrate to other instances)
	// For now, just deregister
	s.loadBalancer.DeregisterInstance(leastLoadedInstance.ID)

	return nil
}

// GetScalingMetrics returns current scaling metrics
func (s *Scaler) GetScalingMetrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.loadBalancer.GetLoadBalancingStats()

	return map[string]interface{}{
		"min_instances":        s.minInstances,
		"max_instances":        s.maxInstances,
		"current_instances":    stats["healthy_instances"],
		"utilization":          stats["utilization_rate"],
		"scale_up_threshold":   s.scaleUpThreshold,
		"scale_down_threshold": s.scaleDownThreshold,
		"last_scale_action":    s.lastScaleAction,
		"cooldown_period":      s.cooldownPeriod.String(),
	}
}

// SetThresholds updates the scaling thresholds
func (s *Scaler) SetThresholds(scaleUpThreshold, scaleDownThreshold float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scaleUpThreshold = scaleUpThreshold
	s.scaleDownThreshold = scaleDownThreshold
}

// SetCooldownPeriod updates the cooldown period
func (s *Scaler) SetCooldownPeriod(period time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cooldownPeriod = period
}

// ManualScale manually triggers a scaling action
func (s *Scaler) ManualScale(ctx context.Context, action ScalingAction) error {
	decision := &ScalingDecision{
		Action:      action,
		Reason:      "Manual scaling triggered",
		CurrentLoad: 0,
		Timestamp:   time.Now(),
	}

	event := s.executeScaling(ctx, decision)

	if event.Success {
		s.mu.Lock()
		s.lastScaleAction = time.Now()
		s.mu.Unlock()
		return nil
	}

	return event.Error
}
