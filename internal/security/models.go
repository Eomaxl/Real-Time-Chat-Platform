package security

import (
	"time"
)

// EventType represents the type of security event
type EventType string

const (
	// Authentication events
	EventAuthSuccess    EventType = "auth_success"
	EventAuthFailure    EventType = "auth_failure"
	EventAuthBruteForce EventType = "auth_brute_force"
	EventTokenExpired   EventType = "token_expired"
	EventTokenInvalid   EventType = "token_invalid"

	// Authorization events
	EventAuthzDenied         EventType = "authz_denied"
	EventAuthzUnauthorized   EventType = "authz_unauthorized"
	EventPrivilegeEscalation EventType = "privilege_escalation"

	// Data access events
	EventDataAccessDenied    EventType = "data_access_denied"
	EventDataExfiltration    EventType = "data_exfiltration"
	EventSensitiveDataAccess EventType = "sensitive_data_access"

	// Rate limiting events
	EventRateLimitExceeded EventType = "rate_limit_exceeded"
	EventAbuseDetected     EventType = "abuse_detected"

	// Encryption events
	EventEncryptionFailure EventType = "encryption_failure"
	EventDecryptionFailure EventType = "decryption_failure"
	EventKeyRotation       EventType = "key_rotation"
	EventKeyCompromise     EventType = "key_compromise"

	// Anomaly events
	EventAnomalousActivity  EventType = "anomalous_activity"
	EventSuspiciousPattern  EventType = "suspicious_pattern"
	EventGeolocationAnomaly EventType = "geolocation_anomaly"

	// Compliance events
	EventComplianceViolation EventType = "compliance_violation"
	EventAuditLogAccess      EventType = "audit_log_access"
	EventDataRetention       EventType = "data_retention"
)

// Severity represents the severity level of a security event
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// SecurityEvent represents a security event
type SecurityEvent struct {
	ID            string                 `json:"id" db:"id"`
	EventType     EventType              `json:"event_type" db:"event_type"`
	Severity      Severity               `json:"severity" db:"severity"`
	UserID        string                 `json:"user_id,omitempty" db:"user_id"`
	TenantID      string                 `json:"tenant_id,omitempty" db:"tenant_id"`
	IPAddress     string                 `json:"ip_address,omitempty" db:"ip_address"`
	UserAgent     string                 `json:"user_agent,omitempty" db:"user_agent"`
	Resource      string                 `json:"resource,omitempty" db:"resource"`
	Action        string                 `json:"action,omitempty" db:"action"`
	Result        string                 `json:"result" db:"result"` // "success", "failure", "blocked"
	Message       string                 `json:"message" db:"message"`
	Details       map[string]interface{} `json:"details,omitempty" db:"details"`
	CorrelationID string                 `json:"correlation_id,omitempty" db:"correlation_id"`
	Timestamp     time.Time              `json:"timestamp" db:"timestamp"`
	Resolved      bool                   `json:"resolved" db:"resolved"`
	ResolvedAt    *time.Time             `json:"resolved_at,omitempty" db:"resolved_at"`
	ResolvedBy    string                 `json:"resolved_by,omitempty" db:"resolved_by"`
}

// AnomalyDetectionRule represents a rule for detecting anomalies
type AnomalyDetectionRule struct {
	ID          string                 `json:"id" db:"id"`
	Name        string                 `json:"name" db:"name"`
	Description string                 `json:"description" db:"description"`
	RuleType    string                 `json:"rule_type" db:"rule_type"` // "threshold", "pattern", "ml"
	Enabled     bool                   `json:"enabled" db:"enabled"`
	Severity    Severity               `json:"severity" db:"severity"`
	Conditions  map[string]interface{} `json:"conditions" db:"conditions"`
	Actions     []string               `json:"actions" db:"actions"` // "alert", "block", "log"
	CreatedAt   time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at" db:"updated_at"`
}

// IncidentResponse represents an automated incident response action
type IncidentResponse struct {
	ID         string                 `json:"id" db:"id"`
	EventID    string                 `json:"event_id" db:"event_id"`
	Action     string                 `json:"action" db:"action"` // "block_user", "revoke_token", "alert_admin"
	Status     string                 `json:"status" db:"status"` // "pending", "executed", "failed"
	ExecutedAt *time.Time             `json:"executed_at,omitempty" db:"executed_at"`
	Result     string                 `json:"result,omitempty" db:"result"`
	Details    map[string]interface{} `json:"details,omitempty" db:"details"`
	CreatedAt  time.Time              `json:"created_at" db:"created_at"`
}

// ComplianceReport represents a compliance audit report
type ComplianceReport struct {
	ID             string                 `json:"id" db:"id"`
	ReportType     string                 `json:"report_type" db:"report_type"` // "gdpr", "hipaa", "soc2"
	StartDate      time.Time              `json:"start_date" db:"start_date"`
	EndDate        time.Time              `json:"end_date" db:"end_date"`
	TotalEvents    int                    `json:"total_events" db:"total_events"`
	SecurityEvents int                    `json:"security_events" db:"security_events"`
	Violations     int                    `json:"violations" db:"violations"`
	ResolvedIssues int                    `json:"resolved_issues" db:"resolved_issues"`
	PendingIssues  int                    `json:"pending_issues" db:"pending_issues"`
	Summary        map[string]interface{} `json:"summary" db:"summary"`
	GeneratedAt    time.Time              `json:"generated_at" db:"generated_at"`
	GeneratedBy    string                 `json:"generated_by" db:"generated_by"`
}

// UserActivityProfile represents a user's activity profile for anomaly detection
type UserActivityProfile struct {
	UserID             string    `json:"user_id" db:"user_id"`
	AvgLoginFrequency  float64   `json:"avg_login_frequency" db:"avg_login_frequency"`
	AvgMessageRate     float64   `json:"avg_message_rate" db:"avg_message_rate"`
	CommonIPAddresses  []string  `json:"common_ip_addresses" db:"common_ip_addresses"`
	CommonLocations    []string  `json:"common_locations" db:"common_locations"`
	TypicalActiveHours []int     `json:"typical_active_hours" db:"typical_active_hours"`
	LastUpdated        time.Time `json:"last_updated" db:"last_updated"`
}

// ThreatIntelligence represents threat intelligence data
type ThreatIntelligence struct {
	ID          string    `json:"id" db:"id"`
	Type        string    `json:"type" db:"type"` // "ip", "domain", "hash"
	Value       string    `json:"value" db:"value"`
	ThreatLevel string    `json:"threat_level" db:"threat_level"` // "low", "medium", "high", "critical"
	Source      string    `json:"source" db:"source"`
	Description string    `json:"description" db:"description"`
	FirstSeen   time.Time `json:"first_seen" db:"first_seen"`
	LastSeen    time.Time `json:"last_seen" db:"last_seen"`
	Active      bool      `json:"active" db:"active"`
}

// AuditTrail represents an audit trail entry
type AuditTrail struct {
	ID            string                 `json:"id" db:"id"`
	UserID        string                 `json:"user_id" db:"user_id"`
	TenantID      string                 `json:"tenant_id,omitempty" db:"tenant_id"`
	Action        string                 `json:"action" db:"action"`
	Resource      string                 `json:"resource" db:"resource"`
	ResourceID    string                 `json:"resource_id,omitempty" db:"resource_id"`
	OldValue      string                 `json:"old_value,omitempty" db:"old_value"`
	NewValue      string                 `json:"new_value,omitempty" db:"new_value"`
	IPAddress     string                 `json:"ip_address" db:"ip_address"`
	UserAgent     string                 `json:"user_agent,omitempty" db:"user_agent"`
	CorrelationID string                 `json:"correlation_id,omitempty" db:"correlation_id"`
	Metadata      map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
	Timestamp     time.Time              `json:"timestamp" db:"timestamp"`
}
