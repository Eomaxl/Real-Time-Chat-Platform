package tenant

import (
	"context"
	"time"
)

// ContextKey represents keys used in tenant context
type ContextKey string

const (
	TenantContextKey ContextKey = "tenant_context"
)

// TenantContext holds tenant-specific information and configuration
type TenantContext struct {
	TenantID    string
	Region      string
	Tier        string // "free", "pro", "enterprise", "platform"
	Limits      TenantLimits
	Encryption  EncryptionConfig
	Compliance  ComplianceConfig
	ShardingKey string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TenantLimits defines resource limits for a tenant
type TenantLimits struct {
	MaxChannels        int
	MaxUsersPerChannel int
	MessageRateLimit   int   // per minute
	StorageQuota       int64 // in bytes
	CallDurationLimit  time.Duration
	APIRateLimit       int // requests per minute
	ConcurrentCalls    int
	RetentionPeriod    time.Duration
	MaxWebSocketConns  int
	MaxFileSize        int64 // in bytes
}

// EncryptionConfig defines encryption requirements for a tenant
type EncryptionConfig struct {
	Required    bool
	Algorithm   string // "AES-256-GCM", "ChaCha20-Poly1305"
	KeyRotation time.Duration
	E2EEnabled  bool // End-to-end encryption
}

// ComplianceConfig defines compliance requirements for a tenant
type ComplianceConfig struct {
	DataResidency      []string // allowed regions
	EncryptionRequired bool
	AuditLogRetention  time.Duration
	PIIHandling        string // "encrypt", "tokenize", "restrict"
	GDPRCompliant      bool
	HIPAACompliant     bool
	SOC2Compliant      bool
}

// WithTenantContext adds tenant context to the given context
func WithTenantContext(ctx context.Context, tenantCtx *TenantContext) context.Context {
	return context.WithValue(ctx, TenantContextKey, tenantCtx)
}

// FromContext extracts tenant context from the given context
func FromContext(ctx context.Context) (*TenantContext, bool) {
	tenantCtx, ok := ctx.Value(TenantContextKey).(*TenantContext)
	return tenantCtx, ok
}

// MustFromContext extracts tenant context or panics if not found
func MustFromContext(ctx context.Context) *TenantContext {
	tenantCtx, ok := FromContext(ctx)
	if !ok {
		panic("tenant context not found in context")
	}
	return tenantCtx
}

// GetTenantID safely extracts tenant ID from context
func GetTenantID(ctx context.Context) string {
	tenantCtx, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return tenantCtx.TenantID
}

// GetDefaultLimits returns default limits for a given tier
func GetDefaultLimits(tier string) TenantLimits {
	switch tier {
	case "free":
		return TenantLimits{
			MaxChannels:        10,
			MaxUsersPerChannel: 50,
			MessageRateLimit:   100,
			StorageQuota:       1 * 1024 * 1024 * 1024, // 1GB
			CallDurationLimit:  30 * time.Minute,
			APIRateLimit:       1000,
			ConcurrentCalls:    5,
			RetentionPeriod:    30 * 24 * time.Hour, // 30 days
			MaxWebSocketConns:  10,
			MaxFileSize:        10 * 1024 * 1024, // 10MB
		}
	case "pro":
		return TenantLimits{
			MaxChannels:        100,
			MaxUsersPerChannel: 500,
			MessageRateLimit:   1000,
			StorageQuota:       100 * 1024 * 1024 * 1024, // 100GB
			CallDurationLimit:  4 * time.Hour,
			APIRateLimit:       10000,
			ConcurrentCalls:    50,
			RetentionPeriod:    90 * 24 * time.Hour, // 90 days
			MaxWebSocketConns:  100,
			MaxFileSize:        100 * 1024 * 1024, // 100MB
		}
	case "enterprise":
		return TenantLimits{
			MaxChannels:        1000,
			MaxUsersPerChannel: 10000,
			MessageRateLimit:   10000,
			StorageQuota:       1024 * 1024 * 1024 * 1024, // 1TB
			CallDurationLimit:  24 * time.Hour,
			APIRateLimit:       100000,
			ConcurrentCalls:    500,
			RetentionPeriod:    365 * 24 * time.Hour, // 1 year
			MaxWebSocketConns:  1000,
			MaxFileSize:        1024 * 1024 * 1024, // 1GB
		}
	case "platform":
		return TenantLimits{
			MaxChannels:        -1, // unlimited
			MaxUsersPerChannel: -1, // unlimited
			MessageRateLimit:   -1, // unlimited
			StorageQuota:       -1, // unlimited
			CallDurationLimit:  0,  // unlimited
			APIRateLimit:       -1, // unlimited
			ConcurrentCalls:    -1, // unlimited
			RetentionPeriod:    0,  // unlimited
			MaxWebSocketConns:  -1, // unlimited
			MaxFileSize:        -1, // unlimited
		}
	default:
		return GetDefaultLimits("free")
	}
}

// GetDefaultEncryption returns default encryption config for a given tier
func GetDefaultEncryption(tier string) EncryptionConfig {
	switch tier {
	case "free", "pro":
		return EncryptionConfig{
			Required:    false,
			Algorithm:   "AES-256-GCM",
			KeyRotation: 90 * 24 * time.Hour, // 90 days
			E2EEnabled:  false,
		}
	case "enterprise", "platform":
		return EncryptionConfig{
			Required:    true,
			Algorithm:   "AES-256-GCM",
			KeyRotation: 30 * 24 * time.Hour, // 30 days
			E2EEnabled:  true,
		}
	default:
		return GetDefaultEncryption("free")
	}
}

// GetDefaultCompliance returns default compliance config for a given tier
func GetDefaultCompliance(tier string) ComplianceConfig {
	switch tier {
	case "free", "pro":
		return ComplianceConfig{
			DataResidency:      []string{}, // no restrictions
			EncryptionRequired: false,
			AuditLogRetention:  30 * 24 * time.Hour, // 30 days
			PIIHandling:        "restrict",
			GDPRCompliant:      false,
			HIPAACompliant:     false,
			SOC2Compliant:      false,
		}
	case "enterprise", "platform":
		return ComplianceConfig{
			DataResidency:      []string{}, // configurable
			EncryptionRequired: true,
			AuditLogRetention:  7 * 365 * 24 * time.Hour, // 7 years
			PIIHandling:        "encrypt",
			GDPRCompliant:      true,
			HIPAACompliant:     true,
			SOC2Compliant:      true,
		}
	default:
		return GetDefaultCompliance("free")
	}
}

// NewTenantContext creates a new tenant context with default values
func NewTenantContext(tenantID, tier, region string) *TenantContext {
	now := time.Now()
	return &TenantContext{
		TenantID:    tenantID,
		Region:      region,
		Tier:        tier,
		Limits:      GetDefaultLimits(tier),
		Encryption:  GetDefaultEncryption(tier),
		Compliance:  GetDefaultCompliance(tier),
		ShardingKey: tenantID, // Use tenant ID as sharding key by default
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// IsUnlimited checks if a limit value represents unlimited
func IsUnlimited(limit int) bool {
	return limit < 0
}

// IsUnlimitedInt64 checks if a limit value represents unlimited
func IsUnlimitedInt64(limit int64) bool {
	return limit < 0
}

// IsUnlimitedDuration checks if a duration represents unlimited
func IsUnlimitedDuration(duration time.Duration) bool {
	return duration == 0
}

// CheckLimit checks if a value is within the allowed limit
func CheckLimit(current, limit int) bool {
	if IsUnlimited(limit) {
		return true
	}
	return current < limit
}

// CheckLimitInt64 checks if a value is within the allowed limit
func CheckLimitInt64(current, limit int64) bool {
	if IsUnlimitedInt64(limit) {
		return true
	}
	return current < limit
}
