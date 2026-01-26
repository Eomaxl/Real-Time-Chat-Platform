package tenant

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IsolationManager handles tenant data isolation
type IsolationManager struct {
	db *pgxpool.Pool
}

// NewIsolationManager creates a new isolation manager
func NewIsolationManager(db *pgxpool.Pool) *IsolationManager {
	return &IsolationManager{db: db}
}

// EnableRowLevelSecurity enables RLS on all multi-tenant tables
func (m *IsolationManager) EnableRowLevelSecurity(ctx context.Context) error {
	tables := []string{
		"messages",
		"channels",
		"channel_members",
		"call_sessions",
		"call_participants",
		"users",
	}

	for _, table := range tables {
		// Enable RLS on table
		query := fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", table)
		if _, err := m.db.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to enable RLS on %s: %w", table, err)
		}

		// Create policy for tenant isolation
		policyQuery := fmt.Sprintf(`
			CREATE POLICY tenant_isolation_policy ON %s
			USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
			WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text)
		`, table)

		if _, err := m.db.Exec(ctx, policyQuery); err != nil {
			// Policy might already exist, ignore error
			continue
		}
	}

	return nil
}

// DisableRowLevelSecurity disables RLS on all multi-tenant tables (for testing)
func (m *IsolationManager) DisableRowLevelSecurity(ctx context.Context) error {
	tables := []string{
		"messages",
		"channels",
		"channel_members",
		"call_sessions",
		"call_participants",
		"users",
	}

	for _, table := range tables {
		query := fmt.Sprintf("ALTER TABLE %s DISABLE ROW LEVEL SECURITY", table)
		if _, err := m.db.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to disable RLS on %s: %w", table, err)
		}
	}

	return nil
}

// SetTenantContext sets the tenant ID in the database session
func SetTenantContext(ctx context.Context, conn *pgxpool.Conn, tenantID string) error {
	query := "SET LOCAL app.current_tenant_id = $1"
	_, err := conn.Exec(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}
	return nil
}

// WithTenantIsolation executes a function with tenant isolation enabled
func (m *IsolationManager) WithTenantIsolation(ctx context.Context, tenantID string, fn func(context.Context, pgx.Tx) error) error {
	// Get tenant context
	tenantCtx, ok := FromContext(ctx)
	if !ok && tenantID != "" {
		// If no tenant context but tenant ID provided, create minimal context
		tenantCtx = &TenantContext{TenantID: tenantID}
		ctx = WithTenantContext(ctx, tenantCtx)
	}

	if tenantCtx == nil {
		return fmt.Errorf("tenant context required for isolation")
	}

	// Begin transaction
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Set tenant context in session
	_, err = tx.Exec(ctx, "SET LOCAL app.current_tenant_id = $1", tenantCtx.TenantID)
	if err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}

	// Execute function
	if err := fn(ctx, tx); err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CreateTenantSchema creates a dedicated schema for a tenant (for schema-per-tenant isolation)
func (m *IsolationManager) CreateTenantSchema(ctx context.Context, tenantID string) error {
	schemaName := fmt.Sprintf("tenant_%s", tenantID)

	// Create schema
	query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to create tenant schema: %w", err)
	}

	// Create tables in tenant schema
	tables := []string{
		`CREATE TABLE IF NOT EXISTS %s.messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			channel_id UUID NOT NULL,
			user_id UUID NOT NULL,
			content TEXT NOT NULL,
			message_type VARCHAR(50) NOT NULL DEFAULT 'text',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			idempotency_key VARCHAR(255) UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS %s.channels (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) NOT NULL,
			type VARCHAR(50) NOT NULL,
			created_by UUID NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS %s.channel_members (
			channel_id UUID NOT NULL,
			user_id UUID NOT NULL,
			role VARCHAR(50) NOT NULL DEFAULT 'member',
			joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			PRIMARY KEY (channel_id, user_id)
		)`,
	}

	for _, tableQuery := range tables {
		query := fmt.Sprintf(tableQuery, schemaName)
		if _, err := m.db.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to create table in tenant schema: %w", err)
		}
	}

	return nil
}

// DropTenantSchema drops a tenant's dedicated schema
func (m *IsolationManager) DropTenantSchema(ctx context.Context, tenantID string) error {
	schemaName := fmt.Sprintf("tenant_%s", tenantID)
	query := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)

	if _, err := m.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to drop tenant schema: %w", err)
	}

	return nil
}

// ValidateTenantAccess validates that the current context has access to the specified tenant
func ValidateTenantAccess(ctx context.Context, targetTenantID string) error {
	tenantCtx, ok := FromContext(ctx)
	if !ok {
		return fmt.Errorf("no tenant context found")
	}

	if tenantCtx.TenantID != targetTenantID {
		return fmt.Errorf("tenant access denied: context tenant %s, target tenant %s",
			tenantCtx.TenantID, targetTenantID)
	}

	return nil
}

// AddTenantIDColumn adds tenant_id column to existing tables
func (m *IsolationManager) AddTenantIDColumn(ctx context.Context) error {
	tables := []string{
		"messages",
		"channels",
		"channel_members",
		"call_sessions",
		"call_participants",
		"users",
	}

	for _, table := range tables {
		// Add tenant_id column if it doesn't exist
		query := fmt.Sprintf(`
			ALTER TABLE %s 
			ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default'
		`, table)

		if _, err := m.db.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to add tenant_id column to %s: %w", table, err)
		}

		// Create index on tenant_id
		indexQuery := fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS idx_%s_tenant_id ON %s(tenant_id)
		`, table, table)

		if _, err := m.db.Exec(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create tenant_id index on %s: %w", table, err)
		}
	}

	return nil
}
