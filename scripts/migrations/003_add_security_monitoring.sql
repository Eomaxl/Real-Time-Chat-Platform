-- Migration: Add security monitoring tables
-- This migration adds tables for security event logging, anomaly detection, and compliance

-- Security events table
CREATE TABLE IF NOT EXISTS security_events (
    id VARCHAR(255) PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    severity VARCHAR(50) NOT NULL,
    user_id VARCHAR(255),
    tenant_id VARCHAR(255),
    ip_address VARCHAR(100),
    user_agent TEXT,
    resource VARCHAR(255),
    action VARCHAR(100),
    result VARCHAR(50) NOT NULL,
    message TEXT NOT NULL,
    details JSONB,
    correlation_id VARCHAR(255),
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    resolved BOOLEAN DEFAULT FALSE,
    resolved_at TIMESTAMP WITH TIME ZONE,
    resolved_by VARCHAR(255)
);

CREATE INDEX idx_security_events_user ON security_events(user_id);
CREATE INDEX idx_security_events_type ON security_events(event_type);
CREATE INDEX idx_security_events_severity ON security_events(severity);
CREATE INDEX idx_security_events_timestamp ON security_events(timestamp DESC);
CREATE INDEX idx_security_events_unresolved ON security_events(resolved) WHERE resolved = FALSE;
CREATE INDEX idx_security_events_correlation ON security_events(correlation_id);

-- Audit trails table
CREATE TABLE IF NOT EXISTS audit_trails (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    resource_id VARCHAR(255),
    old_value TEXT,
    new_value TEXT,
    ip_address VARCHAR(100),
    user_agent TEXT,
    correlation_id VARCHAR(255),
    metadata JSONB,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_audit_trails_user ON audit_trails(user_id);
CREATE INDEX idx_audit_trails_resource ON audit_trails(resource);
CREATE INDEX idx_audit_trails_timestamp ON audit_trails(timestamp DESC);
CREATE INDEX idx_audit_trails_correlation ON audit_trails(correlation_id);

-- Anomaly detection rules table
CREATE TABLE IF NOT EXISTS anomaly_detection_rules (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    rule_type VARCHAR(50) NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    severity VARCHAR(50) NOT NULL,
    conditions JSONB NOT NULL,
    actions JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_anomaly_rules_enabled ON anomaly_detection_rules(enabled) WHERE enabled = TRUE;

-- Incident responses table
CREATE TABLE IF NOT EXISTS incident_responses (
    id VARCHAR(255) PRIMARY KEY,
    event_id VARCHAR(255) NOT NULL REFERENCES security_events(id),
    action VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL,
    executed_at TIMESTAMP WITH TIME ZONE,
    result TEXT,
    details JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_incident_responses_event ON incident_responses(event_id);
CREATE INDEX idx_incident_responses_status ON incident_responses(status);

-- Compliance reports table
CREATE TABLE IF NOT EXISTS compliance_reports (
    id VARCHAR(255) PRIMARY KEY,
    report_type VARCHAR(50) NOT NULL,
    start_date TIMESTAMP WITH TIME ZONE NOT NULL,
    end_date TIMESTAMP WITH TIME ZONE NOT NULL,
    total_events INTEGER DEFAULT 0,
    security_events INTEGER DEFAULT 0,
    violations INTEGER DEFAULT 0,
    resolved_issues INTEGER DEFAULT 0,
    pending_issues INTEGER DEFAULT 0,
    summary JSONB,
    generated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    generated_by VARCHAR(255)
);

CREATE INDEX idx_compliance_reports_type ON compliance_reports(report_type);
CREATE INDEX idx_compliance_reports_generated ON compliance_reports(generated_at DESC);

-- User activity profiles table (for anomaly detection)
CREATE TABLE IF NOT EXISTS user_activity_profiles (
    user_id VARCHAR(255) PRIMARY KEY,
    avg_login_frequency FLOAT DEFAULT 0,
    avg_message_rate FLOAT DEFAULT 0,
    common_ip_addresses JSONB,
    common_locations JSONB,
    typical_active_hours JSONB,
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Threat intelligence table
CREATE TABLE IF NOT EXISTS threat_intelligence (
    id VARCHAR(255) PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    value VARCHAR(255) NOT NULL,
    threat_level VARCHAR(50) NOT NULL,
    source VARCHAR(255),
    description TEXT,
    first_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    active BOOLEAN DEFAULT TRUE
);

CREATE INDEX idx_threat_intel_type_value ON threat_intelligence(type, value);
CREATE INDEX idx_threat_intel_active ON threat_intelligence(active) WHERE active = TRUE;

-- Blocked IPs table
CREATE TABLE IF NOT EXISTS blocked_ips (
    ip_address VARCHAR(100) PRIMARY KEY,
    reason TEXT,
    blocked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_blocked_ips_expires ON blocked_ips(expires_at);

-- Revoked tokens table
CREATE TABLE IF NOT EXISTS revoked_tokens (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    reason TEXT
);

CREATE INDEX idx_revoked_tokens_user ON revoked_tokens(user_id);

-- Admin alerts table
CREATE TABLE IF NOT EXISTS admin_alerts (
    id VARCHAR(255) PRIMARY KEY,
    event_id VARCHAR(255) REFERENCES security_events(id),
    severity VARCHAR(50) NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledged_at TIMESTAMP WITH TIME ZONE,
    acknowledged_by VARCHAR(255)
);

CREATE INDEX idx_admin_alerts_unacknowledged ON admin_alerts(acknowledged) WHERE acknowledged = FALSE;
CREATE INDEX idx_admin_alerts_created ON admin_alerts(created_at DESC);

-- Audit tasks table
CREATE TABLE IF NOT EXISTS audit_tasks (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255),
    task_type VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    completed_by VARCHAR(255),
    results JSONB
);

CREATE INDEX idx_audit_tasks_status ON audit_tasks(status);
CREATE INDEX idx_audit_tasks_user ON audit_tasks(user_id);

-- Add fields to users table for security features
ALTER TABLE users ADD COLUMN IF NOT EXISTS status VARCHAR(50) DEFAULT 'active';
ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS blocked_reason TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_required BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_required_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS requires_reauth BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_required_at TIMESTAMP WITH TIME ZONE;

-- Comments for documentation
COMMENT ON TABLE security_events IS 'Security events and incidents';
COMMENT ON TABLE audit_trails IS 'Audit trail for all user actions';
COMMENT ON TABLE anomaly_detection_rules IS 'Rules for detecting anomalous behavior';
COMMENT ON TABLE incident_responses IS 'Automated incident response actions';
COMMENT ON TABLE compliance_reports IS 'Compliance audit reports';
COMMENT ON TABLE user_activity_profiles IS 'User activity profiles for anomaly detection';
COMMENT ON TABLE threat_intelligence IS 'Threat intelligence data';
COMMENT ON TABLE blocked_ips IS 'Blocked IP addresses';
COMMENT ON TABLE revoked_tokens IS 'Revoked authentication tokens';
COMMENT ON TABLE admin_alerts IS 'Alerts for administrators';
COMMENT ON TABLE audit_tasks IS 'Audit tasks for compliance';
