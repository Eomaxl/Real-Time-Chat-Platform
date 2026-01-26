package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository handles tenant data persistence
type Repository struct {
	db *pgxpool.Pool
}

// NewRepository creates a new tenant repository
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateTenant creates a new tenant in the database
func (r *Repository) CreateTenant(ctx context.Context, tenant *TenantContext) error {
	query := `
		INSERT INTO tenants (
			tenant_id, region, tier, limits, encryption, compliance, 
			sharding_key, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	limitsJSON, err := json.Marshal(tenant.Limits)
	if err != nil {
		return fmt.Errorf("failed to marshal limits: %w", err)
	}

	encryptionJSON, err := json.Marshal(tenant.Encryption)
	if err != nil {
		return fmt.Errorf("failed to marshal encryption: %w", err)
	}

	complianceJSON, err := json.Marshal(tenant.Compliance)
	if err != nil {
		return fmt.Errorf("failed to marshal compliance: %w", err)
	}

	_, err = r.db.Exec(ctx, query,
		tenant.TenantID,
		tenant.Region,
		tenant.Tier,
		limitsJSON,
		encryptionJSON,
		complianceJSON,
		tenant.ShardingKey,
		tenant.CreatedAt,
		tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	return nil
}

// GetTenant retrieves a tenant by ID
func (r *Repository) GetTenant(ctx context.Context, tenantID string) (*TenantContext, error) {
	query := `
		SELECT tenant_id, region, tier, limits, encryption, compliance, 
		       sharding_key, created_at, updated_at
		FROM tenants
		WHERE tenant_id = $1
	`

	var tenant TenantContext
	var limitsJSON, encryptionJSON, complianceJSON []byte

	err := r.db.QueryRow(ctx, query, tenantID).Scan(
		&tenant.TenantID,
		&tenant.Region,
		&tenant.Tier,
		&limitsJSON,
		&encryptionJSON,
		&complianceJSON,
		&tenant.ShardingKey,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("tenant not found: %s", tenantID)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	if err := json.Unmarshal(limitsJSON, &tenant.Limits); err != nil {
		return nil, fmt.Errorf("failed to unmarshal limits: %w", err)
	}

	if err := json.Unmarshal(encryptionJSON, &tenant.Encryption); err != nil {
		return nil, fmt.Errorf("failed to unmarshal encryption: %w", err)
	}

	if err := json.Unmarshal(complianceJSON, &tenant.Compliance); err != nil {
		return nil, fmt.Errorf("failed to unmarshal compliance: %w", err)
	}

	return &tenant, nil
}

// UpdateTenant updates an existing tenant
func (r *Repository) UpdateTenant(ctx context.Context, tenant *TenantContext) error {
	query := `
		UPDATE tenants
		SET region = $2, tier = $3, limits = $4, encryption = $5, 
		    compliance = $6, sharding_key = $7, updated_at = $8
		WHERE tenant_id = $1
	`

	limitsJSON, err := json.Marshal(tenant.Limits)
	if err != nil {
		return fmt.Errorf("failed to marshal limits: %w", err)
	}

	encryptionJSON, err := json.Marshal(tenant.Encryption)
	if err != nil {
		return fmt.Errorf("failed to marshal encryption: %w", err)
	}

	complianceJSON, err := json.Marshal(tenant.Compliance)
	if err != nil {
		return fmt.Errorf("failed to marshal compliance: %w", err)
	}

	tenant.UpdatedAt = time.Now()

	result, err := r.db.Exec(ctx, query,
		tenant.TenantID,
		tenant.Region,
		tenant.Tier,
		limitsJSON,
		encryptionJSON,
		complianceJSON,
		tenant.ShardingKey,
		tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found: %s", tenant.TenantID)
	}

	return nil
}

// DeleteTenant deletes a tenant
func (r *Repository) DeleteTenant(ctx context.Context, tenantID string) error {
	query := `DELETE FROM tenants WHERE tenant_id = $1`

	result, err := r.db.Exec(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	return nil
}

// ListTenants retrieves all tenants with pagination
func (r *Repository) ListTenants(ctx context.Context, limit, offset int) ([]*TenantContext, error) {
	query := `
		SELECT tenant_id, region, tier, limits, encryption, compliance, 
		       sharding_key, created_at, updated_at
		FROM tenants
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*TenantContext

	for rows.Next() {
		var tenant TenantContext
		var limitsJSON, encryptionJSON, complianceJSON []byte

		err := rows.Scan(
			&tenant.TenantID,
			&tenant.Region,
			&tenant.Tier,
			&limitsJSON,
			&encryptionJSON,
			&complianceJSON,
			&tenant.ShardingKey,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		if err := json.Unmarshal(limitsJSON, &tenant.Limits); err != nil {
			return nil, fmt.Errorf("failed to unmarshal limits: %w", err)
		}

		if err := json.Unmarshal(encryptionJSON, &tenant.Encryption); err != nil {
			return nil, fmt.Errorf("failed to unmarshal encryption: %w", err)
		}

		if err := json.Unmarshal(complianceJSON, &tenant.Compliance); err != nil {
			return nil, fmt.Errorf("failed to unmarshal compliance: %w", err)
		}

		tenants = append(tenants, &tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tenants: %w", err)
	}

	return tenants, nil
}

// GetTenantsByTier retrieves all tenants of a specific tier
func (r *Repository) GetTenantsByTier(ctx context.Context, tier string) ([]*TenantContext, error) {
	query := `
		SELECT tenant_id, region, tier, limits, encryption, compliance, 
		       sharding_key, created_at, updated_at
		FROM tenants
		WHERE tier = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, tier)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenants by tier: %w", err)
	}
	defer rows.Close()

	var tenants []*TenantContext

	for rows.Next() {
		var tenant TenantContext
		var limitsJSON, encryptionJSON, complianceJSON []byte

		err := rows.Scan(
			&tenant.TenantID,
			&tenant.Region,
			&tenant.Tier,
			&limitsJSON,
			&encryptionJSON,
			&complianceJSON,
			&tenant.ShardingKey,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		if err := json.Unmarshal(limitsJSON, &tenant.Limits); err != nil {
			return nil, fmt.Errorf("failed to unmarshal limits: %w", err)
		}

		if err := json.Unmarshal(encryptionJSON, &tenant.Encryption); err != nil {
			return nil, fmt.Errorf("failed to unmarshal encryption: %w", err)
		}

		if err := json.Unmarshal(complianceJSON, &tenant.Compliance); err != nil {
			return nil, fmt.Errorf("failed to unmarshal compliance: %w", err)
		}

		tenants = append(tenants, &tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tenants: %w", err)
	}

	return tenants, nil
}
