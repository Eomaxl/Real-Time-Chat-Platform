package metrics

import (
	"context"
	"sync"
	"time"
)

// SLAMonitor tracks SLA compliance over time
type SLAMonitor struct {
	metrics     *ServiceMetrics
	serviceName string

	// Tracking windows
	messageLatencies   []time.Duration
	signalingLatencies []time.Duration
	mu                 sync.RWMutex

	// Configuration
	windowSize     int
	updateInterval time.Duration
}

// NewSLAMonitor creates a new SLA monitor
func NewSLAMonitor(metrics *ServiceMetrics, serviceName string) *SLAMonitor {
	return &SLAMonitor{
		metrics:            metrics,
		serviceName:        serviceName,
		messageLatencies:   make([]time.Duration, 0, 1000),
		signalingLatencies: make([]time.Duration, 0, 1000),
		windowSize:         1000,
		updateInterval:     time.Minute,
	}
}

// RecordMessageLatency records a message latency sample
func (s *SLAMonitor) RecordMessageLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messageLatencies = append(s.messageLatencies, latency)
	if len(s.messageLatencies) > s.windowSize {
		s.messageLatencies = s.messageLatencies[1:]
	}
}

// RecordSignalingLatency records a signaling latency sample
func (s *SLAMonitor) RecordSignalingLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.signalingLatencies = append(s.signalingLatencies, latency)
	if len(s.signalingLatencies) > s.windowSize {
		s.signalingLatencies = s.signalingLatencies[1:]
	}
}

// Start begins the SLA monitoring loop
func (s *SLAMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(s.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateSLAMetrics()
		}
	}
}

// updateSLAMetrics calculates and updates SLA compliance metrics
func (s *SLAMonitor) updateSLAMetrics() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate message latency SLA (p95 < 200ms)
	if len(s.messageLatencies) > 0 {
		p95 := s.calculateP95(s.messageLatencies)
		compliance := 100.0
		if p95 > 200*time.Millisecond {
			// Calculate percentage of requests meeting SLA
			meetingSLA := 0
			for _, lat := range s.messageLatencies {
				if lat <= 200*time.Millisecond {
					meetingSLA++
				}
			}
			compliance = float64(meetingSLA) / float64(len(s.messageLatencies)) * 100
		}
		s.metrics.UpdateSLACompliance(s.serviceName, "message_latency_p95", compliance)
	}

	// Calculate signaling latency SLA (p95 < 150ms)
	if len(s.signalingLatencies) > 0 {
		p95 := s.calculateP95(s.signalingLatencies)
		compliance := 100.0
		if p95 > 150*time.Millisecond {
			meetingSLA := 0
			for _, lat := range s.signalingLatencies {
				if lat <= 150*time.Millisecond {
					meetingSLA++
				}
			}
			compliance = float64(meetingSLA) / float64(len(s.signalingLatencies)) * 100
		}
		s.metrics.UpdateSLACompliance(s.serviceName, "signaling_latency_p95", compliance)
	}
}

// calculateP95 calculates the 95th percentile of a duration slice
func (s *SLAMonitor) calculateP95(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	// Create a copy and sort
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)

	// Simple bubble sort (good enough for small samples)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate p95 index
	index := int(float64(len(sorted)) * 0.95)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}
