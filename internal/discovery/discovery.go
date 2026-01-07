package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"real-time-chat-system/internal/config"
)

// ServiceInstance represents a service instance
type ServiceInstance struct {
	ID      string
	Name    string
	Address string
	Port    string
	Health  string
	Tags    []string
}

// Discovery interface defines service discovery operations
type Discovery interface {
	Register(serviceName, port string) error
	Deregister(serviceName string) error
	Discover(serviceName string) ([]*ServiceInstance, error)
	Health() error
}

// MemoryDiscovery is an in-memory implementation for development
type MemoryDiscovery struct {
	services map[string][]*ServiceInstance
	mutex    sync.RWMutex
	config   *config.ServiceDiscoveryConfig
}

// NewMemoryDiscovery creates a new in-memory service discovery
func NewMemoryDiscovery(cfg *config.ServiceDiscoveryConfig) *MemoryDiscovery {
	return &MemoryDiscovery{
		services: make(map[string][]*ServiceInstance),
		config:   cfg,
	}
}

// Register registers a service instance
func (d *MemoryDiscovery) Register(serviceName, port string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	instance := &ServiceInstance{
		ID:      fmt.Sprintf("%s-%d", serviceName, time.Now().Unix()),
		Name:    serviceName,
		Address: "localhost",
		Port:    port,
		Health:  "healthy",
		Tags:    []string{},
	}

	d.services[serviceName] = append(d.services[serviceName], instance)
	return nil
}

// Deregister removes a service instance
func (d *MemoryDiscovery) Deregister(serviceName string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	delete(d.services, serviceName)
	return nil
}

// Discover returns all instances of a service
func (d *MemoryDiscovery) Discover(serviceName string) ([]*ServiceInstance, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	instances, exists := d.services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}

	// Return a copy to avoid race conditions
	result := make([]*ServiceInstance, len(instances))
	copy(result, instances)
	return result, nil
}

// Health checks the health of the discovery service
func (d *MemoryDiscovery) Health() error {
	return nil // Memory discovery is always healthy
}

// ConsulDiscovery would be implemented for production use
type ConsulDiscovery struct {
	// consul client would be here
	config *config.ServiceDiscoveryConfig
}

// NewConsulDiscovery creates a new Consul-based service discovery
func NewConsulDiscovery(cfg *config.ServiceDiscoveryConfig) (*ConsulDiscovery, error) {
	// In a real implementation, this would initialize a Consul client
	return &ConsulDiscovery{
		config: cfg,
	}, nil
}

// Register registers a service with Consul
func (d *ConsulDiscovery) Register(serviceName, port string) error {
	// Implementation would register with Consul
	return fmt.Errorf("consul discovery not implemented yet")
}

// Deregister removes a service from Consul
func (d *ConsulDiscovery) Deregister(serviceName string) error {
	// Implementation would deregister from Consul
	return fmt.Errorf("consul discovery not implemented yet")
}

// Discover finds services in Consul
func (d *ConsulDiscovery) Discover(serviceName string) ([]*ServiceInstance, error) {
	// Implementation would query Consul
	return nil, fmt.Errorf("consul discovery not implemented yet")
}

// Health checks Consul connectivity
func (d *ConsulDiscovery) Health() error {
	// Implementation would check Consul health
	return fmt.Errorf("consul discovery not implemented yet")
}

// New creates a new service discovery instance based on configuration
func New(cfg *config.ServiceDiscoveryConfig) (Discovery, error) {
	switch cfg.Type {
	case "memory":
		return NewMemoryDiscovery(cfg), nil
	case "consul":
		return NewConsulDiscovery(cfg)
	default:
		return nil, fmt.Errorf("unsupported service discovery type: %s", cfg.Type)
	}
}

// LoadBalancer provides load balancing for discovered services
type LoadBalancer struct {
	discovery Discovery
	counter   uint64
	mutex     sync.Mutex
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(discovery Discovery) *LoadBalancer {
	return &LoadBalancer{
		discovery: discovery,
	}
}

// GetInstance returns a service instance using round-robin load balancing
func (lb *LoadBalancer) GetInstance(serviceName string) (*ServiceInstance, error) {
	instances, err := lb.discovery.Discover(serviceName)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("no healthy instances found for service %s", serviceName)
	}

	lb.mutex.Lock()
	index := lb.counter % uint64(len(instances))
	lb.counter++
	lb.mutex.Unlock()

	return instances[index], nil
}

// HealthChecker periodically checks service health
type HealthChecker struct {
	discovery Discovery
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(discovery Discovery, interval time.Duration) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	return &HealthChecker{
		discovery: discovery,
		interval:  interval,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start begins health checking
func (hc *HealthChecker) Start() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			if err := hc.discovery.Health(); err != nil {
				// Log health check failure
				// In production, this would trigger alerts
			}
		}
	}
}

// Stop stops health checking
func (hc *HealthChecker) Stop() {
	hc.cancel()
}
