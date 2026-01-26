package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// IncidentHandler handles automated incident response
type IncidentHandler struct {
	db *sql.DB
}

// NewIncidentHandler creates a new incident handler
func NewIncidentHandler(db *sql.DB) *IncidentHandler {
	return &IncidentHandler{
		db: db,
	}
}

// HandleEvent handles a security event and triggers appropriate responses
func (ih *IncidentHandler) HandleEvent(ctx context.Context, event *SecurityEvent) {
	// Determine appropriate response based on event type and severity
	var actions []string

	switch event.EventType {
	case EventAuthBruteForce:
		actions = []string{"block_ip", "alert_admin"}
	case EventDataExfiltration:
		actions = []string{"block_user", "alert_admin", "revoke_tokens"}
	case EventPrivilegeEscalation:
		actions = []string{"block_user", "alert_admin", "audit_access"}
	case EventKeyCompromise:
		actions = []string{"revoke_keys", "alert_admin", "force_reauth"}
	case EventAnomalousActivity:
		if event.Severity == SeverityCritical {
			actions = []string{"alert_admin", "require_mfa"}
		} else {
			actions = []string{"log_event"}
		}
	default:
		if event.Severity == SeverityCritical {
			actions = []string{"alert_admin"}
		}
	}

	// Execute each action
	for _, action := range actions {
		ih.executeAction(ctx, event, action)
	}
}

// executeAction executes a specific incident response action
func (ih *IncidentHandler) executeAction(ctx context.Context, event *SecurityEvent, action string) {
	response := &IncidentResponse{
		ID:        uuid.New().String(),
		EventID:   event.ID,
		Action:    action,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	// Execute the action
	var err error
	switch action {
	case "block_ip":
		err = ih.blockIP(ctx, event.IPAddress)
	case "block_user":
		err = ih.blockUser(ctx, event.UserID)
	case "revoke_tokens":
		err = ih.revokeTokens(ctx, event.UserID)
	case "revoke_keys":
		err = ih.revokeKeys(ctx, event.UserID)
	case "alert_admin":
		err = ih.alertAdmin(ctx, event)
	case "require_mfa":
		err = ih.requireMFA(ctx, event.UserID)
	case "force_reauth":
		err = ih.forceReauth(ctx, event.UserID)
	case "audit_access":
		err = ih.auditAccess(ctx, event.UserID)
	case "log_event":
		// Already logged
		err = nil
	default:
		err = fmt.Errorf("unknown action: %s", action)
	}

	// Update response status
	if err != nil {
		response.Status = "failed"
		response.Result = err.Error()
	} else {
		response.Status = "executed"
		response.Result = "success"
	}

	executedAt := time.Now()
	response.ExecutedAt = &executedAt

	// Store incident response
	ih.storeIncidentResponse(ctx, response)
}

// blockIP blocks an IP address
func (ih *IncidentHandler) blockIP(ctx context.Context, ipAddress string) error {
	// In a real implementation, this would integrate with firewall/WAF
	// For now, we'll store it in a blocked IPs table
	query := `
		INSERT INTO blocked_ips (ip_address, reason, blocked_at, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (ip_address) DO UPDATE SET blocked_at = EXCLUDED.blocked_at
	`

	_, err := ih.db.ExecContext(ctx, query,
		ipAddress,
		"Automated block due to security event",
		time.Now(),
		time.Now().Add(24*time.Hour), // Block for 24 hours
	)

	return err
}

// blockUser blocks a user account
func (ih *IncidentHandler) blockUser(ctx context.Context, userID string) error {
	// In a real implementation, this would update the user's status
	query := `
		UPDATE users SET status = 'blocked', blocked_at = $1, blocked_reason = $2
		WHERE id = $3
	`

	_, err := ih.db.ExecContext(ctx, query,
		time.Now(),
		"Automated block due to security event",
		userID,
	)

	return err
}

// revokeTokens revokes all tokens for a user
func (ih *IncidentHandler) revokeTokens(ctx context.Context, userID string) error {
	// In a real implementation, this would invalidate all JWT tokens
	// This could be done by incrementing a token version number or blacklisting tokens
	query := `
		INSERT INTO revoked_tokens (user_id, revoked_at, reason)
		VALUES ($1, $2, $3)
	`

	_, err := ih.db.ExecContext(ctx, query,
		userID,
		time.Now(),
		"Automated revocation due to security event",
	)

	return err
}

// revokeKeys revokes encryption keys for a user
func (ih *IncidentHandler) revokeKeys(ctx context.Context, userID string) error {
	// Delete all encryption keys for the user
	queries := []string{
		`DELETE FROM identity_keys WHERE user_id = $1`,
		`DELETE FROM signed_prekeys WHERE user_id = $1`,
		`DELETE FROM onetime_prekeys WHERE user_id = $1`,
		`DELETE FROM session_keys WHERE sender_user_id = $1 OR receiver_user_id = $1`,
	}

	for _, query := range queries {
		_, err := ih.db.ExecContext(ctx, query, userID)
		if err != nil {
			return fmt.Errorf("failed to revoke keys: %w", err)
		}
	}

	return nil
}

// alertAdmin sends an alert to administrators
func (ih *IncidentHandler) alertAdmin(ctx context.Context, event *SecurityEvent) error {
	// In a real implementation, this would send notifications via email, Slack, PagerDuty, etc.
	// For now, we'll just log it as a high-priority alert
	query := `
		INSERT INTO admin_alerts (id, event_id, severity, message, created_at, acknowledged)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := ih.db.ExecContext(ctx, query,
		uuid.New().String(),
		event.ID,
		event.Severity,
		fmt.Sprintf("Security event requires attention: %s", event.Message),
		time.Now(),
		false,
	)

	return err
}

// requireMFA requires multi-factor authentication for a user
func (ih *IncidentHandler) requireMFA(ctx context.Context, userID string) error {
	// Update user settings to require MFA
	query := `
		UPDATE users SET mfa_required = true, mfa_required_at = $1
		WHERE id = $2
	`

	_, err := ih.db.ExecContext(ctx, query, time.Now(), userID)
	return err
}

// forceReauth forces a user to re-authenticate
func (ih *IncidentHandler) forceReauth(ctx context.Context, userID string) error {
	// Revoke tokens and set flag requiring re-authentication
	err := ih.revokeTokens(ctx, userID)
	if err != nil {
		return err
	}

	query := `
		UPDATE users SET requires_reauth = true, reauth_required_at = $1
		WHERE id = $2
	`

	_, err = ih.db.ExecContext(ctx, query, time.Now(), userID)
	return err
}

// auditAccess performs an access audit for a user
func (ih *IncidentHandler) auditAccess(ctx context.Context, userID string) error {
	// Create an audit task
	query := `
		INSERT INTO audit_tasks (id, user_id, task_type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := ih.db.ExecContext(ctx, query,
		uuid.New().String(),
		userID,
		"access_audit",
		"pending",
		time.Now(),
	)

	return err
}

// storeIncidentResponse stores an incident response record
func (ih *IncidentHandler) storeIncidentResponse(ctx context.Context, response *IncidentResponse) error {
	detailsJSON, err := json.Marshal(response.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	query := `
		INSERT INTO incident_responses (id, event_id, action, status, executed_at, result, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err = ih.db.ExecContext(ctx, query,
		response.ID,
		response.EventID,
		response.Action,
		response.Status,
		response.ExecutedAt,
		response.Result,
		detailsJSON,
		response.CreatedAt,
	)

	return err
}

// GetIncidentResponses retrieves incident responses for an event
func (ih *IncidentHandler) GetIncidentResponses(ctx context.Context, eventID string) ([]IncidentResponse, error) {
	query := `
		SELECT id, event_id, action, status, executed_at, result, details, created_at
		FROM incident_responses
		WHERE event_id = $1
		ORDER BY created_at DESC
	`

	rows, err := ih.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to query incident responses: %w", err)
	}
	defer rows.Close()

	responses := []IncidentResponse{}
	for rows.Next() {
		var response IncidentResponse
		var detailsJSON []byte

		err := rows.Scan(
			&response.ID,
			&response.EventID,
			&response.Action,
			&response.Status,
			&response.ExecutedAt,
			&response.Result,
			&detailsJSON,
			&response.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan incident response: %w", err)
		}

		if len(detailsJSON) > 0 {
			json.Unmarshal(detailsJSON, &response.Details)
		}

		responses = append(responses, response)
	}

	return responses, nil
}
