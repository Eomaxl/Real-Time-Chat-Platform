package security

import (
	"context"
	"database/sql"
	"time"
)

// Service provides comprehensive security services
type Service struct {
	monitor           *Monitor
	anomalyDetector   *AnomalyDetector
	incidentHandler   *IncidentHandler
	complianceManager *ComplianceManager
}

// NewService creates a new security service
func NewService(db *sql.DB) *Service {
	return &Service{
		monitor:           NewMonitor(db),
		anomalyDetector:   NewAnomalyDetector(db),
		incidentHandler:   NewIncidentHandler(db),
		complianceManager: NewComplianceManager(db),
	}
}

// LogAuthenticationSuccess logs a successful authentication event
func (s *Service) LogAuthenticationSuccess(ctx context.Context, userID, tenantID, ipAddress, userAgent, correlationID string) error {
	event := &SecurityEvent{
		EventType:     EventAuthSuccess,
		Severity:      SeverityLow,
		UserID:        userID,
		TenantID:      tenantID,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		Resource:      "authentication",
		Action:        "login",
		Result:        "success",
		Message:       "User authenticated successfully",
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// LogAuthenticationFailure logs a failed authentication event
func (s *Service) LogAuthenticationFailure(ctx context.Context, userID, tenantID, ipAddress, userAgent, correlationID, reason string) error {
	event := &SecurityEvent{
		EventType: EventAuthFailure,
		Severity:  SeverityMedium,
		UserID:    userID,
		TenantID:  tenantID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Resource:  "authentication",
		Action:    "login",
		Result:    "failure",
		Message:   "Authentication failed: " + reason,
		Details: map[string]interface{}{
			"reason": reason,
		},
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// LogAuthorizationDenied logs an authorization denial event
func (s *Service) LogAuthorizationDenied(ctx context.Context, userID, tenantID, resource, action, ipAddress, correlationID string) error {
	event := &SecurityEvent{
		EventType:     EventAuthzDenied,
		Severity:      SeverityMedium,
		UserID:        userID,
		TenantID:      tenantID,
		IPAddress:     ipAddress,
		Resource:      resource,
		Action:        action,
		Result:        "denied",
		Message:       "Authorization denied for resource access",
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// LogDataAccess logs a data access event
func (s *Service) LogDataAccess(ctx context.Context, userID, tenantID, resource, resourceID, action, ipAddress, correlationID string) error {
	audit := &AuditTrail{
		UserID:        userID,
		TenantID:      tenantID,
		Action:        action,
		Resource:      resource,
		ResourceID:    resourceID,
		IPAddress:     ipAddress,
		CorrelationID: correlationID,
	}

	return s.monitor.LogAuditTrail(ctx, audit)
}

// LogDataModification logs a data modification event
func (s *Service) LogDataModification(ctx context.Context, userID, tenantID, resource, resourceID, action, oldValue, newValue, ipAddress, correlationID string) error {
	audit := &AuditTrail{
		UserID:        userID,
		TenantID:      tenantID,
		Action:        action,
		Resource:      resource,
		ResourceID:    resourceID,
		OldValue:      oldValue,
		NewValue:      newValue,
		IPAddress:     ipAddress,
		CorrelationID: correlationID,
	}

	return s.monitor.LogAuditTrail(ctx, audit)
}

// LogRateLimitExceeded logs a rate limit exceeded event
func (s *Service) LogRateLimitExceeded(ctx context.Context, userID, tenantID, ipAddress, resource, correlationID string) error {
	event := &SecurityEvent{
		EventType:     EventRateLimitExceeded,
		Severity:      SeverityMedium,
		UserID:        userID,
		TenantID:      tenantID,
		IPAddress:     ipAddress,
		Resource:      resource,
		Action:        "rate_limit_check",
		Result:        "blocked",
		Message:       "Rate limit exceeded",
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// LogEncryptionFailure logs an encryption failure event
func (s *Service) LogEncryptionFailure(ctx context.Context, userID, tenantID, resource, correlationID, reason string) error {
	event := &SecurityEvent{
		EventType: EventEncryptionFailure,
		Severity:  SeverityHigh,
		UserID:    userID,
		TenantID:  tenantID,
		Resource:  resource,
		Action:    "encrypt",
		Result:    "failure",
		Message:   "Encryption operation failed: " + reason,
		Details: map[string]interface{}{
			"reason": reason,
		},
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// LogKeyRotation logs a key rotation event
func (s *Service) LogKeyRotation(ctx context.Context, userID, tenantID, keyType, correlationID string) error {
	event := &SecurityEvent{
		EventType: EventKeyRotation,
		Severity:  SeverityLow,
		UserID:    userID,
		TenantID:  tenantID,
		Resource:  "encryption_keys",
		Action:    "rotate",
		Result:    "success",
		Message:   "Encryption key rotated",
		Details: map[string]interface{}{
			"key_type": keyType,
		},
		CorrelationID: correlationID,
	}

	return s.monitor.LogSecurityEvent(ctx, event)
}

// GetSecurityEvents retrieves security events with filtering
func (s *Service) GetSecurityEvents(ctx context.Context, filter SecurityEventFilter) ([]SecurityEvent, error) {
	return s.monitor.GetSecurityEvents(ctx, filter)
}

// GetAuditTrails retrieves audit trails with filtering
func (s *Service) GetAuditTrails(ctx context.Context, filter AuditTrailFilter) ([]AuditTrail, error) {
	return s.monitor.GetAuditTrails(ctx, filter)
}

// GetSecurityMetrics retrieves security metrics for a time period
func (s *Service) GetSecurityMetrics(ctx context.Context, startTime, endTime time.Time) (*SecurityMetrics, error) {
	return s.monitor.GetSecurityMetrics(ctx, startTime, endTime)
}

// ResolveSecurityEvent marks a security event as resolved
func (s *Service) ResolveSecurityEvent(ctx context.Context, eventID, resolvedBy string) error {
	return s.monitor.ResolveSecurityEvent(ctx, eventID, resolvedBy)
}

// GenerateComplianceReport generates a compliance report
func (s *Service) GenerateComplianceReport(ctx context.Context, reportType string, startDate, endDate time.Time, generatedBy string) (*ComplianceReport, error) {
	return s.complianceManager.GenerateComplianceReport(ctx, reportType, startDate, endDate, generatedBy)
}

// GetComplianceReport retrieves a compliance report by ID
func (s *Service) GetComplianceReport(ctx context.Context, reportID string) (*ComplianceReport, error) {
	return s.complianceManager.GetComplianceReport(ctx, reportID)
}

// ListComplianceReports lists compliance reports
func (s *Service) ListComplianceReports(ctx context.Context, reportType string, limit int) ([]ComplianceReport, error) {
	return s.complianceManager.ListComplianceReports(ctx, reportType, limit)
}

// ExportComplianceReport exports a compliance report
func (s *Service) ExportComplianceReport(ctx context.Context, reportID string, format string) ([]byte, error) {
	return s.complianceManager.ExportComplianceReport(ctx, reportID, format)
}

// GetIncidentResponses retrieves incident responses for an event
func (s *Service) GetIncidentResponses(ctx context.Context, eventID string) ([]IncidentResponse, error) {
	return s.incidentHandler.GetIncidentResponses(ctx, eventID)
}

// CheckIPBlocked checks if an IP address is blocked
func (s *Service) CheckIPBlocked(ctx context.Context, ipAddress string, db *sql.DB) (bool, error) {
	query := `
		SELECT COUNT(*) FROM blocked_ips
		WHERE ip_address = $1 
		  AND (expires_at IS NULL OR expires_at > $2)
	`

	var count int
	err := db.QueryRowContext(ctx, query, ipAddress, time.Now()).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// CheckUserBlocked checks if a user is blocked
func (s *Service) CheckUserBlocked(ctx context.Context, userID string, db *sql.DB) (bool, error) {
	query := `
		SELECT status FROM users WHERE id = $1
	`

	var status string
	err := db.QueryRowContext(ctx, query, userID).Scan(&status)
	if err != nil {
		return false, err
	}

	return status == "blocked", nil
}

// CheckTokenRevoked checks if a user's tokens have been revoked
func (s *Service) CheckTokenRevoked(ctx context.Context, userID string, tokenIssuedAt time.Time, db *sql.DB) (bool, error) {
	query := `
		SELECT COUNT(*) FROM revoked_tokens
		WHERE user_id = $1 AND revoked_at > $2
	`

	var count int
	err := db.QueryRowContext(ctx, query, userID, tokenIssuedAt).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
