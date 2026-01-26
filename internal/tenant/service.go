package tenant

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Service provides tenant management operations
type Service struct {
	repo             *Repository
	isolationManager *IsolationManager
	quotaManager     *QuotaManager
	rateLimiter      *RateLimiter
	metrics          *Metrics
	auditLogger      *AuditLogger
}

// NewService creates a new tenant service
func NewService(db *pgxpool.Pool, redisClient *redis.Client) *Service {
	repo := NewRepository(db)

	return &Service{
		repo:             repo,
		isolationManager: NewIsolationManager(db),
		quotaManager:     NewQuotaManager(db),
		rateLimiter:      NewRateLimiter(redisClient),
		metrics:          NewMetrics(),
		auditLogger:      NewAuditLogger(repo),
	}
}

// CreateTenant creates a new tenant with default configuration
func (s *Service) CreateTenant(ctx context.Context, tenantID, tier, region string) (*TenantContext, error) {
	// Create tenant context with defaults
	tenantCtx := NewTenantContext(tenantID, tier, region)

	// Save to database
	if err := s.repo.CreateTenant(ctx, tenantCtx); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	// Log audit event
	s.auditLogger.LogAction(ctx, tenantID, "system", "create_tenant", "tenant", tenantID, nil)

	return tenantCtx, nil
}

// GetTenant retrieves a tenant by ID
func (s *Service) GetTenant(ctx context.Context, tenantID string) (*TenantContext, error) {
	return s.repo.GetTenant(ctx, tenantID)
}

// UpdateTenant updates tenant configuration
func (s *Service) UpdateTenant(ctx context.Context, tenant *TenantContext) error {
	if err := s.repo.UpdateTenant(ctx, tenant); err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	// Log audit event
	s.auditLogger.LogAction(ctx, tenant.TenantID, "system", "update_tenant", "tenant", tenant.TenantID, nil)

	return nil
}

// DeleteTenant deletes a tenant and all associated data
func (s *Service) DeleteTenant(ctx context.Context, tenantID string) error {
	// Delete tenant data
	if err := s.repo.DeleteTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	// Reset rate limits
	if err := s.rateLimiter.ResetRateLimit(ctx, tenantID); err != nil {
		// Log but don't fail
	}

	// Log audit event
	s.auditLogger.LogAction(ctx, tenantID, "system", "delete_tenant", "tenant", tenantID, nil)

	return nil
}

// CheckAPIRateLimit checks if a tenant can make an API request
func (s *Service) CheckAPIRateLimit(ctx context.Context, tenantID string) (bool, error) {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	allowed, err := s.rateLimiter.CheckRateLimit(ctx, tenantID, tenant.Limits)
	if err != nil {
		return false, err
	}

	if !allowed {
		s.metrics.RecordRateLimitHit(tenantID, tenant.Tier)
	}

	return allowed, nil
}

// CheckMessageRateLimit checks if a tenant can send a message
func (s *Service) CheckMessageRateLimit(ctx context.Context, tenantID string) (bool, error) {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	allowed, err := s.rateLimiter.CheckMessageRateLimit(ctx, tenantID, tenant.Limits)
	if err != nil {
		return false, err
	}

	if !allowed {
		s.metrics.RecordMessageRateLimitHit(tenantID, tenant.Tier)
	}

	return allowed, nil
}

// CheckChannelQuota checks if a tenant can create a new channel
func (s *Service) CheckChannelQuota(ctx context.Context, tenantID string) (bool, error) {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	allowed, err := s.quotaManager.CheckChannelQuota(ctx, tenantID, tenant.Limits)
	if err != nil {
		return false, err
	}

	if !allowed {
		s.metrics.RecordQuotaExceeded(tenantID, tenant.Tier, "channels")
	}

	return allowed, nil
}

// CheckStorageQuota checks if a tenant has storage available
func (s *Service) CheckStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) (bool, error) {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	allowed, err := s.quotaManager.CheckStorageQuota(ctx, tenantID, additionalBytes, tenant.Limits)
	if err != nil {
		return false, err
	}

	if !allowed {
		s.metrics.RecordQuotaExceeded(tenantID, tenant.Tier, "storage")
	}

	return allowed, nil
}

// CheckCallQuota checks if a tenant can start a new call
func (s *Service) CheckCallQuota(ctx context.Context, tenantID string) (bool, error) {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	allowed, err := s.quotaManager.CheckConcurrentCallsQuota(ctx, tenantID, tenant.Limits)
	if err != nil {
		return false, err
	}

	if !allowed {
		s.metrics.RecordQuotaExceeded(tenantID, tenant.Tier, "calls")
	}

	return allowed, nil
}

// GetResourceUsage retrieves current resource usage for a tenant
func (s *Service) GetResourceUsage(ctx context.Context, tenantID string) (*ResourceUsage, error) {
	usage, err := s.quotaManager.GetResourceUsage(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource usage: %w", err)
	}

	// Update metrics
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err == nil {
		s.metrics.UpdateResourceUsage(tenantID, tenant.Tier, usage)
		s.metrics.UpdateQuotaUsage(tenantID, tenant.Tier, tenant.Limits, usage)
	}

	return usage, nil
}

// UpgradeTier upgrades a tenant to a higher tier
func (s *Service) UpgradeTier(ctx context.Context, tenantID, newTier string) error {
	tenant, err := s.repo.GetTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	// Update tier and limits
	tenant.Tier = newTier
	tenant.Limits = GetDefaultLimits(newTier)
	tenant.Encryption = GetDefaultEncryption(newTier)
	tenant.Compliance = GetDefaultCompliance(newTier)

	if err := s.repo.UpdateTenant(ctx, tenant); err != nil {
		return fmt.Errorf("failed to upgrade tier: %w", err)
	}

	// Log audit event
	s.auditLogger.LogAction(ctx, tenantID, "system", "upgrade_tier", "tenant", tenantID, map[string]interface{}{
		"new_tier": newTier,
	})

	return nil
}

// GetMetrics returns the metrics instance
func (s *Service) GetMetrics() *Metrics {
	return s.metrics
}

// GetIsolationManager returns the isolation manager
func (s *Service) GetIsolationManager() *IsolationManager {
	return s.isolationManager
}

// GetQuotaManager returns the quota manager
func (s *Service) GetQuotaManager() *QuotaManager {
	return s.quotaManager
}

// GetRateLimiter returns the rate limiter
func (s *Service) GetRateLimiter() *RateLimiter {
	return s.rateLimiter
}
