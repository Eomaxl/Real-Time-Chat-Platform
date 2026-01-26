package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AnomalyDetector detects anomalous security behavior
type AnomalyDetector struct {
	db    *sql.DB
	rules []AnomalyDetectionRule
}

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector(db *sql.DB) *AnomalyDetector {
	detector := &AnomalyDetector{
		db:    db,
		rules: []AnomalyDetectionRule{},
	}

	// Load rules from database
	detector.LoadRules(context.Background())

	return detector
}

// LoadRules loads anomaly detection rules from the database
func (ad *AnomalyDetector) LoadRules(ctx context.Context) error {
	query := `
		SELECT id, name, description, rule_type, enabled, severity, conditions, actions, created_at, updated_at
		FROM anomaly_detection_rules
		WHERE enabled = true
	`

	rows, err := ad.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to load rules: %w", err)
	}
	defer rows.Close()

	rules := []AnomalyDetectionRule{}
	for rows.Next() {
		var rule AnomalyDetectionRule
		var conditionsJSON, actionsJSON []byte

		err := rows.Scan(
			&rule.ID,
			&rule.Name,
			&rule.Description,
			&rule.RuleType,
			&rule.Enabled,
			&rule.Severity,
			&conditionsJSON,
			&actionsJSON,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)

		if err != nil {
			return fmt.Errorf("failed to scan rule: %w", err)
		}

		json.Unmarshal(conditionsJSON, &rule.Conditions)
		json.Unmarshal(actionsJSON, &rule.Actions)

		rules = append(rules, rule)
	}

	ad.rules = rules
	return nil
}

// AnalyzeEvent analyzes a security event for anomalies
func (ad *AnomalyDetector) AnalyzeEvent(ctx context.Context, event *SecurityEvent) {
	// Check for brute force attacks
	if event.EventType == EventAuthFailure {
		ad.detectBruteForce(ctx, event)
	}

	// Check for unusual activity patterns
	if event.UserID != "" {
		ad.detectUnusualActivity(ctx, event)
	}

	// Check for geolocation anomalies
	if event.IPAddress != "" {
		ad.detectGeolocationAnomaly(ctx, event)
	}

	// Check for data exfiltration
	if event.Action == "download" || event.Action == "export" {
		ad.detectDataExfiltration(ctx, event)
	}

	// Apply custom rules
	for _, rule := range ad.rules {
		if ad.evaluateRule(ctx, event, &rule) {
			ad.triggerRuleActions(ctx, event, &rule)
		}
	}
}

// detectBruteForce detects brute force authentication attempts
func (ad *AnomalyDetector) detectBruteForce(ctx context.Context, event *SecurityEvent) {
	// Count failed login attempts in the last 5 minutes
	query := `
		SELECT COUNT(*) FROM security_events
		WHERE event_type = $1 
		  AND (user_id = $2 OR ip_address = $3)
		  AND timestamp > $4
	`

	var count int
	err := ad.db.QueryRowContext(ctx, query,
		EventAuthFailure,
		event.UserID,
		event.IPAddress,
		time.Now().Add(-5*time.Minute),
	).Scan(&count)

	if err != nil {
		return
	}

	// If more than 5 failed attempts, log brute force event
	if count >= 5 {
		bruteForceEvent := &SecurityEvent{
			ID:            uuid.New().String(),
			EventType:     EventAuthBruteForce,
			Severity:      SeverityHigh,
			UserID:        event.UserID,
			TenantID:      event.TenantID,
			IPAddress:     event.IPAddress,
			UserAgent:     event.UserAgent,
			Resource:      "authentication",
			Action:        "brute_force_detected",
			Result:        "blocked",
			Message:       fmt.Sprintf("Brute force attack detected: %d failed attempts", count),
			CorrelationID: event.CorrelationID,
			Timestamp:     time.Now(),
			Resolved:      false,
		}

		// Log the brute force event
		detailsJSON, _ := json.Marshal(bruteForceEvent.Details)
		ad.db.ExecContext(ctx, `
			INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
			                             user_agent, resource, action, result, message, details, 
			                             correlation_id, timestamp, resolved)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`,
			bruteForceEvent.ID,
			bruteForceEvent.EventType,
			bruteForceEvent.Severity,
			bruteForceEvent.UserID,
			bruteForceEvent.TenantID,
			bruteForceEvent.IPAddress,
			bruteForceEvent.UserAgent,
			bruteForceEvent.Resource,
			bruteForceEvent.Action,
			bruteForceEvent.Result,
			bruteForceEvent.Message,
			detailsJSON,
			bruteForceEvent.CorrelationID,
			bruteForceEvent.Timestamp,
			bruteForceEvent.Resolved,
		)
	}
}

// detectUnusualActivity detects unusual user activity patterns
func (ad *AnomalyDetector) detectUnusualActivity(ctx context.Context, event *SecurityEvent) {
	// Get user's activity profile
	var profile UserActivityProfile
	query := `
		SELECT user_id, avg_login_frequency, avg_message_rate, common_ip_addresses, 
		       common_locations, typical_active_hours, last_updated
		FROM user_activity_profiles
		WHERE user_id = $1
	`

	var ipAddressesJSON, locationsJSON, activeHoursJSON []byte
	err := ad.db.QueryRowContext(ctx, query, event.UserID).Scan(
		&profile.UserID,
		&profile.AvgLoginFrequency,
		&profile.AvgMessageRate,
		&ipAddressesJSON,
		&locationsJSON,
		&activeHoursJSON,
		&profile.LastUpdated,
	)

	if err != nil {
		// No profile exists, create one
		return
	}

	json.Unmarshal(ipAddressesJSON, &profile.CommonIPAddresses)
	json.Unmarshal(locationsJSON, &profile.CommonLocations)
	json.Unmarshal(activeHoursJSON, &profile.TypicalActiveHours)

	// Check if IP address is unusual
	isUnusualIP := true
	for _, ip := range profile.CommonIPAddresses {
		if ip == event.IPAddress {
			isUnusualIP = false
			break
		}
	}

	// Check if activity time is unusual
	currentHour := time.Now().Hour()
	isUnusualTime := true
	for _, hour := range profile.TypicalActiveHours {
		if hour == currentHour {
			isUnusualTime = false
			break
		}
	}

	// If both IP and time are unusual, flag as anomalous
	if isUnusualIP && isUnusualTime {
		anomalyEvent := &SecurityEvent{
			ID:        uuid.New().String(),
			EventType: EventAnomalousActivity,
			Severity:  SeverityMedium,
			UserID:    event.UserID,
			TenantID:  event.TenantID,
			IPAddress: event.IPAddress,
			UserAgent: event.UserAgent,
			Resource:  event.Resource,
			Action:    "unusual_activity_detected",
			Result:    "flagged",
			Message:   "Unusual activity pattern detected",
			Details: map[string]interface{}{
				"unusual_ip":   isUnusualIP,
				"unusual_time": isUnusualTime,
			},
			CorrelationID: event.CorrelationID,
			Timestamp:     time.Now(),
			Resolved:      false,
		}

		detailsJSON, _ := json.Marshal(anomalyEvent.Details)
		ad.db.ExecContext(ctx, `
			INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
			                             user_agent, resource, action, result, message, details, 
			                             correlation_id, timestamp, resolved)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`,
			anomalyEvent.ID,
			anomalyEvent.EventType,
			anomalyEvent.Severity,
			anomalyEvent.UserID,
			anomalyEvent.TenantID,
			anomalyEvent.IPAddress,
			anomalyEvent.UserAgent,
			anomalyEvent.Resource,
			anomalyEvent.Action,
			anomalyEvent.Result,
			anomalyEvent.Message,
			detailsJSON,
			anomalyEvent.CorrelationID,
			anomalyEvent.Timestamp,
			anomalyEvent.Resolved,
		)
	}
}

// detectGeolocationAnomaly detects geolocation anomalies
func (ad *AnomalyDetector) detectGeolocationAnomaly(ctx context.Context, event *SecurityEvent) {
	// Check for impossible travel (login from two distant locations in short time)
	query := `
		SELECT ip_address, timestamp FROM security_events
		WHERE user_id = $1 
		  AND event_type = $2
		  AND timestamp > $3
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var lastIP string
	var lastTime time.Time
	err := ad.db.QueryRowContext(ctx, query,
		event.UserID,
		EventAuthSuccess,
		time.Now().Add(-1*time.Hour),
	).Scan(&lastIP, &lastTime)

	if err != nil {
		return
	}

	// If IP changed within an hour, flag as potential anomaly
	if lastIP != event.IPAddress {
		anomalyEvent := &SecurityEvent{
			ID:        uuid.New().String(),
			EventType: EventGeolocationAnomaly,
			Severity:  SeverityMedium,
			UserID:    event.UserID,
			TenantID:  event.TenantID,
			IPAddress: event.IPAddress,
			UserAgent: event.UserAgent,
			Resource:  "authentication",
			Action:    "geolocation_anomaly_detected",
			Result:    "flagged",
			Message:   "Possible impossible travel detected",
			Details: map[string]interface{}{
				"previous_ip":   lastIP,
				"current_ip":    event.IPAddress,
				"time_diff_min": time.Since(lastTime).Minutes(),
			},
			CorrelationID: event.CorrelationID,
			Timestamp:     time.Now(),
			Resolved:      false,
		}

		detailsJSON, _ := json.Marshal(anomalyEvent.Details)
		ad.db.ExecContext(ctx, `
			INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
			                             user_agent, resource, action, result, message, details, 
			                             correlation_id, timestamp, resolved)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`,
			anomalyEvent.ID,
			anomalyEvent.EventType,
			anomalyEvent.Severity,
			anomalyEvent.UserID,
			anomalyEvent.TenantID,
			anomalyEvent.IPAddress,
			anomalyEvent.UserAgent,
			anomalyEvent.Resource,
			anomalyEvent.Action,
			anomalyEvent.Result,
			anomalyEvent.Message,
			detailsJSON,
			anomalyEvent.CorrelationID,
			anomalyEvent.Timestamp,
			anomalyEvent.Resolved,
		)
	}
}

// detectDataExfiltration detects potential data exfiltration
func (ad *AnomalyDetector) detectDataExfiltration(ctx context.Context, event *SecurityEvent) {
	// Count data access events in the last hour
	query := `
		SELECT COUNT(*) FROM security_events
		WHERE user_id = $1 
		  AND (action = 'download' OR action = 'export')
		  AND timestamp > $2
	`

	var count int
	err := ad.db.QueryRowContext(ctx, query,
		event.UserID,
		time.Now().Add(-1*time.Hour),
	).Scan(&count)

	if err != nil {
		return
	}

	// If more than 10 downloads/exports in an hour, flag as potential exfiltration
	if count >= 10 {
		exfiltrationEvent := &SecurityEvent{
			ID:            uuid.New().String(),
			EventType:     EventDataExfiltration,
			Severity:      SeverityHigh,
			UserID:        event.UserID,
			TenantID:      event.TenantID,
			IPAddress:     event.IPAddress,
			UserAgent:     event.UserAgent,
			Resource:      event.Resource,
			Action:        "data_exfiltration_detected",
			Result:        "flagged",
			Message:       fmt.Sprintf("Potential data exfiltration: %d downloads/exports in 1 hour", count),
			CorrelationID: event.CorrelationID,
			Timestamp:     time.Now(),
			Resolved:      false,
		}

		detailsJSON, _ := json.Marshal(exfiltrationEvent.Details)
		ad.db.ExecContext(ctx, `
			INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
			                             user_agent, resource, action, result, message, details, 
			                             correlation_id, timestamp, resolved)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`,
			exfiltrationEvent.ID,
			exfiltrationEvent.EventType,
			exfiltrationEvent.Severity,
			exfiltrationEvent.UserID,
			exfiltrationEvent.TenantID,
			exfiltrationEvent.IPAddress,
			exfiltrationEvent.UserAgent,
			exfiltrationEvent.Resource,
			exfiltrationEvent.Action,
			exfiltrationEvent.Result,
			exfiltrationEvent.Message,
			detailsJSON,
			exfiltrationEvent.CorrelationID,
			exfiltrationEvent.Timestamp,
			exfiltrationEvent.Resolved,
		)
	}
}

// evaluateRule evaluates a custom anomaly detection rule
func (ad *AnomalyDetector) evaluateRule(ctx context.Context, event *SecurityEvent, rule *AnomalyDetectionRule) bool {
	// Simple threshold-based rule evaluation
	if rule.RuleType == "threshold" {
		if threshold, ok := rule.Conditions["threshold"].(float64); ok {
			if eventType, ok := rule.Conditions["event_type"].(string); ok {
				if string(event.EventType) == eventType {
					// Count events in time window
					timeWindow := 5 * time.Minute
					if tw, ok := rule.Conditions["time_window_minutes"].(float64); ok {
						timeWindow = time.Duration(tw) * time.Minute
					}

					query := `
						SELECT COUNT(*) FROM security_events
						WHERE event_type = $1 AND timestamp > $2
					`

					var count int
					ad.db.QueryRowContext(ctx, query, eventType, time.Now().Add(-timeWindow)).Scan(&count)

					return float64(count) >= threshold
				}
			}
		}
	}

	return false
}

// triggerRuleActions triggers actions for a matched rule
func (ad *AnomalyDetector) triggerRuleActions(ctx context.Context, event *SecurityEvent, rule *AnomalyDetectionRule) {
	for _, action := range rule.Actions {
		switch action {
		case "alert":
			// Log high-severity alert
			alertEvent := &SecurityEvent{
				ID:        uuid.New().String(),
				EventType: EventAnomalousActivity,
				Severity:  rule.Severity,
				UserID:    event.UserID,
				TenantID:  event.TenantID,
				IPAddress: event.IPAddress,
				Resource:  event.Resource,
				Action:    "rule_triggered",
				Result:    "alert",
				Message:   fmt.Sprintf("Anomaly detection rule triggered: %s", rule.Name),
				Details: map[string]interface{}{
					"rule_id":   rule.ID,
					"rule_name": rule.Name,
				},
				Timestamp: time.Now(),
			}

			detailsJSON, _ := json.Marshal(alertEvent.Details)
			ad.db.ExecContext(ctx, `
				INSERT INTO security_events (id, event_type, severity, user_id, tenant_id, ip_address, 
				                             user_agent, resource, action, result, message, details, 
				                             correlation_id, timestamp, resolved)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			`,
				alertEvent.ID,
				alertEvent.EventType,
				alertEvent.Severity,
				alertEvent.UserID,
				alertEvent.TenantID,
				alertEvent.IPAddress,
				alertEvent.UserAgent,
				alertEvent.Resource,
				alertEvent.Action,
				alertEvent.Result,
				alertEvent.Message,
				detailsJSON,
				alertEvent.CorrelationID,
				alertEvent.Timestamp,
				alertEvent.Resolved,
			)

		case "block":
			// This would integrate with rate limiting or access control
			// For now, just log the block action

		case "log":
			// Already logged by default
		}
	}
}
