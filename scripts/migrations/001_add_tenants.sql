-- Migration: Add multi-tenancy support
-- Description: Creates tenant tables and adds tenant_id columns to existing tables

-- Create tenants table
CREATE TABLE IF NOT EXISTS tenants (
    tenant_id VARCHAR(255) PRIMARY KEY,
    region VARCHAR(100) NOT NULL,
    tier VARCHAR(50) NOT NULL CHECK (tier IN ('free', 'pro', 'enterprise', 'platform')),
    limits JSONB NOT NULL,
    encryption JSONB NOT NULL,
    compliance JSONB NOT NULL,
    sharding_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create index on tier for filtering
CREATE INDEX IF NOT EXISTS idx_tenants_tier ON tenants(tier);

-- Create index on region for geo-routing
CREATE INDEX IF NOT EXISTS idx_tenants_region ON tenants(region);

-- Add tenant_id column to existing tables
ALTER TABLE messages 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

ALTER TABLE channels 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

ALTER TABLE channel_members 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

ALTER TABLE call_sessions 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

ALTER TABLE call_participants 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

ALTER TABLE users 
ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255) NOT NULL DEFAULT 'default';

-- Create indexes on tenant_id for all tables
CREATE INDEX IF NOT EXISTS idx_messages_tenant_id ON messages(tenant_id);
CREATE INDEX IF NOT EXISTS idx_channels_tenant_id ON channels(tenant_id);
CREATE INDEX IF NOT EXISTS idx_channel_members_tenant_id ON channel_members(tenant_id);
CREATE INDEX IF NOT EXISTS idx_call_sessions_tenant_id ON call_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_call_participants_tenant_id ON call_participants(tenant_id);
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);

-- Create composite indexes for common queries
CREATE INDEX IF NOT EXISTS idx_messages_tenant_channel ON messages(tenant_id, channel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channels_tenant_type ON channels(tenant_id, type);
CREATE INDEX IF NOT EXISTS idx_call_sessions_tenant_channel ON call_sessions(tenant_id, channel_id);

-- Enable Row Level Security on all multi-tenant tables
ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE channel_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE call_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE call_participants ENABLE ROW LEVEL SECURITY;
ALTER TABLE users ENABLE ROW LEVEL SECURITY;

-- Create RLS policies for tenant isolation
CREATE POLICY tenant_isolation_policy ON messages
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

CREATE POLICY tenant_isolation_policy ON channels
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

CREATE POLICY tenant_isolation_policy ON channel_members
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

CREATE POLICY tenant_isolation_policy ON call_sessions
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

CREATE POLICY tenant_isolation_policy ON call_participants
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

CREATE POLICY tenant_isolation_policy ON users
    USING (tenant_id = current_setting('app.current_tenant_id', true)::text)
    WITH CHECK (tenant_id = current_setting('app.current_tenant_id', true)::text);

-- Create function to automatically set tenant_id from context
CREATE OR REPLACE FUNCTION set_tenant_id()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.tenant_id IS NULL OR NEW.tenant_id = 'default' THEN
        NEW.tenant_id := current_setting('app.current_tenant_id', true)::text;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers to automatically set tenant_id
CREATE TRIGGER set_tenant_id_messages
    BEFORE INSERT ON messages
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

CREATE TRIGGER set_tenant_id_channels
    BEFORE INSERT ON channels
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

CREATE TRIGGER set_tenant_id_channel_members
    BEFORE INSERT ON channel_members
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

CREATE TRIGGER set_tenant_id_call_sessions
    BEFORE INSERT ON call_sessions
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

CREATE TRIGGER set_tenant_id_call_participants
    BEFORE INSERT ON call_participants
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

CREATE TRIGGER set_tenant_id_users
    BEFORE INSERT ON users
    FOR EACH ROW
    EXECUTE FUNCTION set_tenant_id();

-- Create tenant usage tracking table
CREATE TABLE IF NOT EXISTS tenant_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    resource_type VARCHAR(100) NOT NULL,
    resource_count BIGINT NOT NULL DEFAULT 0,
    storage_bytes BIGINT NOT NULL DEFAULT 0,
    api_requests BIGINT NOT NULL DEFAULT 0,
    websocket_connections INT NOT NULL DEFAULT 0,
    active_calls INT NOT NULL DEFAULT 0,
    period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    period_end TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for usage tracking
CREATE INDEX IF NOT EXISTS idx_tenant_usage_tenant_period ON tenant_usage(tenant_id, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_tenant_usage_resource ON tenant_usage(resource_type, period_start);

-- Create tenant audit log table
CREATE TABLE IF NOT EXISTS tenant_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    user_id VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(100) NOT NULL,
    resource_id VARCHAR(255),
    details JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for audit log
CREATE INDEX IF NOT EXISTS idx_tenant_audit_log_tenant ON tenant_audit_log(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_log_user ON tenant_audit_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_log_action ON tenant_audit_log(action, created_at DESC);

-- Insert default tenant for existing data
INSERT INTO tenants (tenant_id, region, tier, limits, encryption, compliance, sharding_key)
VALUES (
    'default',
    'us-east-1',
    'platform',
    '{"MaxChannels": -1, "MaxUsersPerChannel": -1, "MessageRateLimit": -1, "StorageQuota": -1, "CallDurationLimit": 0, "APIRateLimit": -1, "ConcurrentCalls": -1, "RetentionPeriod": 0, "MaxWebSocketConns": -1, "MaxFileSize": -1}'::jsonb,
    '{"Required": false, "Algorithm": "AES-256-GCM", "KeyRotation": 2592000000000000, "E2EEnabled": false}'::jsonb,
    '{"DataResidency": [], "EncryptionRequired": false, "AuditLogRetention": 2592000000000000, "PIIHandling": "restrict", "GDPRCompliant": false, "HIPAACompliant": false, "SOC2Compliant": false}'::jsonb,
    'default'
)
ON CONFLICT (tenant_id) DO NOTHING;
