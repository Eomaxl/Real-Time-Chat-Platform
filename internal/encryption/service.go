package encryption

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Service provides encryption services for the chat platform
type Service struct {
	keyManager     *KeyManager
	sessionManager *SessionManager
	policy         KeyRotationPolicy
}

// NewService creates a new encryption service
func NewService(db *sql.DB, policy KeyRotationPolicy) *Service {
	keyManager := NewKeyManager(db, policy)
	sessionManager := NewSessionManager(db, keyManager, policy)

	return &Service{
		keyManager:     keyManager,
		sessionManager: sessionManager,
		policy:         policy,
	}
}

// InitializeUserKeys initializes encryption keys for a new user
func (s *Service) InitializeUserKeys(ctx context.Context, userID, deviceID string) error {
	// Generate identity key
	_, err := s.keyManager.GenerateIdentityKey(ctx, userID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to generate identity key: %w", err)
	}

	// Generate signed pre-key
	_, err = s.keyManager.GenerateSignedPreKey(ctx, userID, deviceID, 1)
	if err != nil {
		return fmt.Errorf("failed to generate signed pre-key: %w", err)
	}

	// Generate one-time pre-keys
	_, err = s.keyManager.GenerateOneTimePreKeys(ctx, userID, deviceID, 100)
	if err != nil {
		return fmt.Errorf("failed to generate one-time pre-keys: %w", err)
	}

	return nil
}

// GetUserKeyBundle retrieves a user's public key bundle for initiating encryption
func (s *Service) GetUserKeyBundle(ctx context.Context, userID, deviceID string) (*KeyBundle, error) {
	return s.keyManager.GetKeyBundle(ctx, userID, deviceID)
}

// EncryptMessage encrypts a message for a recipient
func (s *Service) EncryptMessage(ctx context.Context, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID string, plaintext string) (string, error) {
	encryptedMsg, err := s.sessionManager.EncryptMessage(ctx, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt message: %w", err)
	}

	// Serialize encrypted message to JSON
	jsonData, err := json.Marshal(encryptedMsg)
	if err != nil {
		return "", fmt.Errorf("failed to serialize encrypted message: %w", err)
	}

	return string(jsonData), nil
}

// DecryptMessage decrypts a message from a sender
func (s *Service) DecryptMessage(ctx context.Context, encryptedMessageJSON string) (string, error) {
	// Deserialize encrypted message from JSON
	var encryptedMsg EncryptedMessage
	err := json.Unmarshal([]byte(encryptedMessageJSON), &encryptedMsg)
	if err != nil {
		return "", fmt.Errorf("failed to deserialize encrypted message: %w", err)
	}

	plaintext, err := s.sessionManager.DecryptMessage(ctx, &encryptedMsg)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt message: %w", err)
	}

	return string(plaintext), nil
}

// RotateUserKeys rotates encryption keys for a user based on policy
func (s *Service) RotateUserKeys(ctx context.Context, userID, deviceID string) error {
	return s.keyManager.RotateKeys(ctx, userID, deviceID)
}

// CleanupExpiredSessions removes expired encryption sessions
func (s *Service) CleanupExpiredSessions(ctx context.Context) error {
	return s.keyManager.CleanupExpiredKeys(ctx)
}

// GetKeyRotationStatus returns the status of key rotation for a user
func (s *Service) GetKeyRotationStatus(ctx context.Context, userID, deviceID string) (*KeyRotationStatus, error) {
	identityKey, err := s.keyManager.GetIdentityKey(ctx, userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity key: %w", err)
	}

	status := &KeyRotationStatus{
		UserID:         userID,
		DeviceID:       deviceID,
		IdentityKeyAge: time.Since(identityKey.CreatedAt),
		NeedsRotation:  false,
	}

	// Check if identity key needs rotation
	daysSinceCreation := status.IdentityKeyAge.Hours() / 24
	if daysSinceCreation > float64(s.policy.IdentityKeyRotationDays) {
		status.NeedsRotation = true
		status.RotationReason = "Identity key expired"
	}

	return status, nil
}

// KeyRotationStatus represents the key rotation status for a user
type KeyRotationStatus struct {
	UserID         string        `json:"user_id"`
	DeviceID       string        `json:"device_id"`
	IdentityKeyAge time.Duration `json:"identity_key_age"`
	NeedsRotation  bool          `json:"needs_rotation"`
	RotationReason string        `json:"rotation_reason,omitempty"`
}

// VerifyKeyBundle verifies the integrity of a key bundle
func (s *Service) VerifyKeyBundle(ctx context.Context, bundle *KeyBundle) error {
	crypto := NewCryptoEngine()

	// Verify signed pre-key signature
	err := crypto.Verify(bundle.IdentityKey, bundle.SignedPreKey, bundle.SignedPreKeySig)
	if err != nil {
		return fmt.Errorf("invalid signed pre-key signature: %w", err)
	}

	return nil
}

// ExportPublicKeys exports public keys for a user (for backup or migration)
func (s *Service) ExportPublicKeys(ctx context.Context, userID, deviceID string) (*PublicKeyExport, error) {
	identityKey, err := s.keyManager.GetIdentityKey(ctx, userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity key: %w", err)
	}

	bundle, err := s.keyManager.GetKeyBundle(ctx, userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get key bundle: %w", err)
	}

	export := &PublicKeyExport{
		UserID:          userID,
		DeviceID:        deviceID,
		IdentityKey:     identityKey.PublicKey,
		SignedPreKey:    bundle.SignedPreKey,
		SignedPreKeyID:  bundle.SignedPreKeyID,
		SignedPreKeySig: bundle.SignedPreKeySig,
		ExportedAt:      time.Now(),
	}

	return export, nil
}

// PublicKeyExport represents exported public keys
type PublicKeyExport struct {
	UserID          string    `json:"user_id"`
	DeviceID        string    `json:"device_id"`
	IdentityKey     []byte    `json:"identity_key"`
	SignedPreKey    []byte    `json:"signed_prekey"`
	SignedPreKeyID  int       `json:"signed_prekey_id"`
	SignedPreKeySig []byte    `json:"signed_prekey_signature"`
	ExportedAt      time.Time `json:"exported_at"`
}

// RevokeDevice revokes all keys for a device
func (s *Service) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	// This would delete all keys associated with the device
	// For now, we'll just mark them as expired
	// In a production system, you'd want to notify all active sessions

	// Delete identity key
	query := `DELETE FROM identity_keys WHERE user_id = $1 AND device_id = $2`
	_, err := s.keyManager.db.ExecContext(ctx, query, userID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to revoke identity key: %w", err)
	}

	// Delete pre-keys
	query = `DELETE FROM signed_prekeys WHERE user_id = $1 AND device_id = $2`
	_, err = s.keyManager.db.ExecContext(ctx, query, userID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to revoke signed pre-keys: %w", err)
	}

	// Delete one-time pre-keys
	query = `DELETE FROM onetime_prekeys WHERE user_id = $1 AND device_id = $2`
	_, err = s.keyManager.db.ExecContext(ctx, query, userID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to revoke one-time pre-keys: %w", err)
	}

	// Delete sessions
	query = `DELETE FROM session_keys WHERE (sender_user_id = $1 AND sender_device_id = $2) OR (receiver_user_id = $1 AND receiver_device_id = $2)`
	_, err = s.keyManager.db.ExecContext(ctx, query, userID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to revoke sessions: %w", err)
	}

	return nil
}
