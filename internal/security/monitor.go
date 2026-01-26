package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Monitor provides security monitoring and event tracking
type Monitor struct {
	db              *sql.DB
	anomalyDetector *AnomalyDetector
	incidentHandler *IncidentHandler
}

// NewMonitor creates a new security monitor
func NewMonitor(db *sql.DB) *Monitor {
	return &Monitor{
		db:              db,
		anomalyDetector: NewAnomalyDetector(db),
		incidentHandler: NewIncidentHandler(db),
	}
}

// LogSecurityEvent logs a security event
func (m *Monitor) LogSecurityEvent(ctx context.Context, event *SecurityEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Serialize details to JSON
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	query := `
		INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
		                             user_agent, resource, action, result, message, details, 
		                             correlation_id, timestamp, resolved)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err = m.db.ExecContext(ctx, query,
		event.ID,
		event.EventType,
		event.Severity,
		event.UserID,
		event.TenantID,
		event.IPAddress,
		event.UserAgent,
		event.Resource,
		event.Action,
		event.Result,
		event.Message,
		detailsJSON,
		event.CorrelationID,
		event.Timestamp,
		event.Resolved,
	)

	if err != nil {
		return fmt.Errorf("failed to log security event: %w", err)
	}

	// Check for anomalies
	go m.anomalyDetector.AnalyzeEvent(ctx, event)

	// Trigger incident response if needed
	if event.Severity == SeverityCritical || event.Severity == SeverityHigh {
		go m.incidentHandler.HandleEvent(ctx, event)
	}

	return nil
}

// GetSecurityEvents retrieves security events with filtering
func (m *Monitor) GetSecurityEvents(ctx context.Context, filter SecurityEventFilter) ([]SecurityEvent, error) {
	query := `
		SELECT id, event_type, severity, user_id, tenant_id, ip_address, user_agent, 
		       resource, action, result, message, details, correlation_id, timestamp, 
		       resolved, resolved_at, resolved_by
		FROM security_events
		WHERE 1=1
	`

	args := []interface{}{}
	argCount := 1

	if filter.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argCount)
		args = append(args, filter.UserID)
		argCount++
	}

	if filter.EventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", argCount)
		args = append(args, filter.EventType)
		argCount++
	}

	if filter.Severity != "" {
		query += fmt.Sprintf(" AND severity = $%d", argCount)
		args = append(args, filter.Severity)
		argCount++
	}

	if !filter.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argCount)
		args = append(args, filter.StartTime)
		argCount++
	}

	if !filter.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argCount)
		args = append(args, filter.EndTime)
		argCount++
	}

	if filter.Resolved != nil {
		query += fmt.Sprintf(" AND resolved = $%d", argCount)
		args = append(args, *filter.Resolved)
		argCount++
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, filter.Limit)
	}

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query security events: %w", err)
	}
	defer rows.Close()

	events := []SecurityEvent{}
	for rows.Next() {
		var event SecurityEvent
		var detailsJSON []byte

		err := rows.Scan(
			&event.ID,
			&event.EventType,
			&event.Severity,
			&event.UserID,
			&event.TenantID,
			&event.IPAddress,
			&event.UserAgent,
			&event.Resource,
			&event.Action,
			&event.Result,
			&event.Message,
			&detailsJSON,
			&event.CorrelationID,
			&event.Timestamp,
			&event.Resolved,
			&event.ResolvedAt,
			&event.ResolvedBy,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan security event: %w", err)
		}

		// Unmarshal details
		if len(detailsJSON) > 0 {
			json.Unmarshal(detailsJSON, &event.Details)
		}

		events = append(events, event)
	}

	return events, nil
}

// ResolveSecurityEvent marks a security event as resolved
func (m *Monitor) ResolveSecurityEvent(ctx context.Context, eventID, resolvedBy string) error {
	query := `
		UPDATE security_events
		SET resolved = true, resolved_at = $1, resolved_by = $2
		WHERE id = $3
	`

	_, err := m.db.ExecContext(ctx, query, time.Now(), resolvedBy, eventID)
	if err != nil {
		return fmt.Errorf("failed to resolve security event: %w", err)
	}

	return nil
}

// LogAuditTrail logs an audit trail entry
func (m *Monitor) LogAuditTrail(ctx context.Context, audit *AuditTrail) error {
	if audit.ID == "" {
		audit.ID = uuid.New().String()
	}

	if audit.Timestamp.IsZero() {
		audit.Timestamp = time.Now()
	}

	// Serialize metadata to JSON
	metadataJSON, err := json.Marshal(audit.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO audit_trails (id, user_id, tenant_id, action, resource, resource_id, 
		                          old_value, new_value, ip_address, user_agent, correlation_id, 
		                          metadata, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = m.db.ExecContext(ctx, query,
		audit.ID,
		audit.UserID,
		audit.TenantID,
		audit.Action,
		audit.Resource,
		audit.ResourceID,
		audit.OldValue,
		audit.NewValue,
		audit.IPAddress,
		audit.UserAgent,
		audit.CorrelationID,
		metadataJSON,
		audit.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to log audit trail: %w", err)
	}

	return nil
}

// GetAuditTrails retrieves audit trail entries
func (m *Monitor) GetAuditTrails(ctx context.Context, filter AuditTrailFilter) ([]AuditTrail, error) {
	query := `
		SELECT id, user_id, tenant_id, action, resource, resource_id, old_value, new_value, 
		       ip_address, user_agent, correlation_id, metadata, timestamp
		FROM audit_trails
		WHERE 1=1
	`

	args := []interface{}{}
	argCount := 1

	if filter.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argCount)
		args = append(args, filter.UserID)
		argCount++
	}

	if filter.Resource != "" {
		query += fmt.Sprintf(" AND resource = $%d", argCount)
		args = append(args, filter.Resource)
		argCount++
	}

	if !filter.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argCount)
		args = append(args, filter.StartTime)
		argCount++
	}

	if !filter.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argCount)
		args = append(args, filter.EndTime)
		argCount++
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, filter.Limit)
	}

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit trails: %w", err)
	}
	defer rows.Close()

	trails := []AuditTrail{}
	for rows.Next() {
		var trail AuditTrail
		var metadataJSON []byte

		err := rows.Scan(
			&trail.ID,
			&trail.UserID,
			&trail.TenantID,
			&trail.Action,
			&trail.Resource,
			&trail.ResourceID,
			&trail.OldValue,
			&trail.NewValue,
			&trail.IPAddress,
			&trail.UserAgent,
			&trail.CorrelationID,
			&metadataJSON,
			&trail.Timestamp,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan audit trail: %w", err)
		}

		// Unmarshal metadata
		if len(metadataJSON) > 0 {
			json.Unmarshal(metadataJSON, &trail.Metadata)
		}

		trails = append(trails, trail)
	}

	return trails, nil
}

// SecurityEventFilter represents filters for querying security events
type SecurityEventFilter struct {
	UserID    string
	EventType EventType
	Severity  Severity
	StartTime time.Time
	EndTime   time.Time
	Resolved  *bool
	Limit     int
}

// AuditTrailFilter represents filters for querying audit trails
type AuditTrailFilter struct {
	UserID    string
	Resource  string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
}

// GetSecurityMetrics retrieves security metrics for a time period
func (m *Monitor) GetSecurityMetrics(ctx context.Context, startTime, endTime time.Time) (*SecurityMetrics, error) {
	metrics := &SecurityMetrics{
		StartTime: startTime,
		EndTime:   endTime,
	}

	// Count total events
	err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM security_events 
		WHERE timestamp >= $1 AND timestamp <= $2
	`, startTime, endTime).Scan(&metrics.TotalEvents)

	if err != nil {
		return nil, fmt.Errorf("failed to count total events: %w", err)
	}

	// Count by severity
	rows, err := m.db.QueryContext(ctx, `
		SELECT severity, COUNT(*) FROM security_events 
		WHERE timestamp >= $1 AND timestamp <= $2
		GROUP BY severity
	`, startTime, endTime)

	if err != nil {
		return nil, fmt.Errorf("failed to count by severity: %w", err)
	}
	defer rows.Close()

	metrics.BySeverity = make(map[Severity]int)
	for rows.Next() {
		var severity Severity
		var count int
		rows.Scan(&severity, &count)
		metrics.BySeverity[severity] = count
	}

	// Count by event type
	rows, err = m.db.QueryContext(ctx, `
		SELECT event_type, COUNT(*) FROM security_events 
		WHERE timestamp >= $1 AND timestamp <= $2
		GROUP BY event_type
		ORDER BY COUNT(*) DESC
		LIMIT 10
	`, startTime, endTime)

	if err != nil {
		return nil, fmt.Errorf("failed to count by event type: %w", err)
	}
	defer rows.Close()

	metrics.ByEventType = make(map[EventType]int)
	for rows.Next() {
		var eventType EventType
		var count int
		rows.Scan(&eventType, &count)
		metrics.ByEventType[eventType] = count
	}

	return metrics, nil
}

// SecurityMetrics represents security metrics
type SecurityMetrics struct {
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	TotalEvents int               `json:"total_events"`
	BySeverity  map[Severity]int  `json:"by_severity"`
	ByEventType map[EventType]int `json:"by_event_type"`
}
