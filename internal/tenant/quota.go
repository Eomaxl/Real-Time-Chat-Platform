package tenant

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QuotaManager handles tenant resource quotas
type QuotaManager struct {
	db *pgxpool.Pool
}

// NewQuotaManager creates a new quota manager
func NewQuotaManager(db *pgxpool.Pool) *QuotaManager {
	return &QuotaManager{db: db}
}

// ResourceUsage represents current resource usage for a tenant
type ResourceUsage struct {
	TenantID       string
	ChannelCount   int
	MessageCount   int64
	StorageBytes   int64
	ActiveCalls    int
	WebSocketConns int
	TotalUsers     int
}

// CheckChannelQuota checks if a tenant can create a new channel
func (q *QuotaManager) CheckChannelQuota(ctx context.Context, tenantID string, limits TenantLimits) (bool, error) {
	if IsUnlimited(limits.MaxChannels) {
		return true, nil
	}

	query := `SELECT COUNT(*) FROM channels WHERE tenant_id = $1`

	var count int
	err := q.db.QueryRow(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count channels: %w", err)
	}

	return count < limits.MaxChannels, nil
}

// CheckStorageQuota checks if a tenant has exceeded their storage quota
func (q *QuotaManager) CheckStorageQuota(ctx context.Context, tenantID string, additionalBytes int64, limits TenantLimits) (bool, error) {
	if IsUnlimitedInt64(limits.StorageQuota) {
		return true, nil
	}

	usage, err := q.GetResourceUsage(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get resource usage: %w", err)
	}

	return usage.StorageBytes+additionalBytes <= limits.StorageQuota, nil
}

// CheckConcurrentCallsQuota checks if a tenant can start a new call
func (q *QuotaManager) CheckConcurrentCallsQuota(ctx context.Context, tenantID string, limits TenantLimits) (bool, error) {
	if IsUnlimited(limits.ConcurrentCalls) {
		return true, nil
	}

	query := `
		SELECT COUNT(*) 
		FROM call_sessions 
		WHERE tenant_id = $1 AND status = 'active'
	`

	var count int
	err := q.db.QueryRow(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count active calls: %w", err)
	}

	return count < limits.ConcurrentCalls, nil
}

// CheckWebSocketQuota checks if a tenant can open a new WebSocket connection
func (q *QuotaManager) CheckWebSocketQuota(ctx context.Context, tenantID string, currentConns int, limits TenantLimits) bool {
	if IsUnlimited(limits.MaxWebSocketConns) {
		return true
	}

	return currentConns < limits.MaxWebSocketConns
}

// CheckChannelMemberQuota checks if a channel can add more members
func (q *QuotaManager) CheckChannelMemberQuota(ctx context.Context, channelID string, limits TenantLimits) (bool, error) {
	if IsUnlimited(limits.MaxUsersPerChannel) {
		return true, nil
	}

	query := `SELECT COUNT(*) FROM channel_members WHERE channel_id = $1`

	var count int
	err := q.db.QueryRow(ctx, query, channelID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count channel members: %w", err)
	}

	return count < limits.MaxUsersPerChannel, nil
}

// GetResourceUsage retrieves current resource usage for a tenant
func (q *QuotaManager) GetResourceUsage(ctx context.Context, tenantID string) (*ResourceUsage, error) {
	usage := &ResourceUsage{
		TenantID: tenantID,
	}

	// Count channels
	err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE tenant_id = $1`, tenantID).Scan(&usage.ChannelCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count channels: %w", err)
	}

	// Count messages
	err = q.db.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE tenant_id = $1`, tenantID).Scan(&usage.MessageCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count messages: %w", err)
	}

	// Calculate storage (approximate based on message content length)
	err = q.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(LENGTH(content)), 0) 
		FROM messages 
		WHERE tenant_id = $1
	`, tenantID).Scan(&usage.StorageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate storage: %w", err)
	}

	// Count active calls
	err = q.db.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM call_sessions 
		WHERE tenant_id = $1 AND status = 'active'
	`, tenantID).Scan(&usage.ActiveCalls)
	if err != nil {
		return nil, fmt.Errorf("failed to count active calls: %w", err)
	}

	// Count users
	err = q.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantID).Scan(&usage.TotalUsers)
	if err != nil {
		return nil, fmt.Errorf("failed to count users: %w", err)
	}

	return usage, nil
}

// RecordUsage records resource usage for a tenant
func (q *QuotaManager) RecordUsage(ctx context.Context, usage *ResourceUsage) error {
	query := `
		INSERT INTO tenant_usage (
			tenant_id, resource_type, resource_count, storage_bytes, 
			websocket_connections, active_calls, period_start, period_end
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW() + INTERVAL '1 hour')
		ON CONFLICT (tenant_id, period_start) 
		DO UPDATE SET 
			resource_count = EXCLUDED.resource_count,
			storage_bytes = EXCLUDED.storage_bytes,
			websocket_connections = EXCLUDED.websocket_connections,
			active_calls = EXCLUDED.active_calls,
			updated_at = NOW()
	`

	_, err := q.db.Exec(ctx, query,
		usage.TenantID,
		"aggregate",
		usage.MessageCount,
		usage.StorageBytes,
		usage.WebSocketConns,
		usage.ActiveCalls,
	)

	if err != nil {
		return fmt.Errorf("failed to record usage: %w", err)
	}

	return nil
}

// GetUsagePercentage calculates the percentage of quota used
func GetUsagePercentage(current, limit int) float64 {
	if IsUnlimited(limit) {
		return 0.0
	}
	if limit == 0 {
		return 100.0
	}
	return (float64(current) / float64(limit)) * 100.0
}

// GetUsagePercentageInt64 calculates the percentage of quota used for int64 values
func GetUsagePercentageInt64(current, limit int64) float64 {
	if IsUnlimitedInt64(limit) {
		return 0.0
	}
	if limit == 0 {
		return 100.0
	}
	return (float64(current) / float64(limit)) * 100.0
}

// IsNearLimit checks if usage is near the limit (>80%)
func IsNearLimit(current, limit int) bool {
	if IsUnlimited(limit) {
		return false
	}
	return GetUsagePercentage(current, limit) > 80.0
}

// IsNearLimitInt64 checks if usage is near the limit (>80%) for int64 values
func IsNearLimitInt64(current, limit int64) bool {
	if IsUnlimitedInt64(limit) {
		return false
	}
	return GetUsagePercentageInt64(current, limit) > 80.0
}
