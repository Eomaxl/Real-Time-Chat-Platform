package encryption

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// KeyManager manages encryption keys for users
type KeyManager struct {
	db     *sql.DB
	crypto *CryptoEngine
	policy KeyRotationPolicy
}

// NewKeyManager creates a new key manager
func NewKeyManager(db *sql.DB, policy KeyRotationPolicy) *KeyManager {
	return &KeyManager{
		db:     db,
		crypto: NewCryptoEngine(),
		policy: policy,
	}
}

// GenerateIdentityKey generates and stores a new identity key for a user
func (km *KeyManager) GenerateIdentityKey(ctx context.Context, userID, deviceID string) (*IdentityKey, error) {
	keyPair, err := km.crypto.GenerateIdentityKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate identity key: %w", err)
	}

	identityKey := &IdentityKey{
		UserID:     userID,
		DeviceID:   deviceID,
		PublicKey:  keyPair.PublicKey,
		PrivateKey: keyPair.PrivateKey,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	query := `
		INSERT INTO identity_keys (user_id, device_id, public_key, private_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, device_id) DO UPDATE
		SET public_key = EXCLUDED.public_key,
		    private_key = EXCLUDED.private_key,
		    updated_at = EXCLUDED.updated_at
	`

	_, err = km.db.ExecContext(ctx, query,
		identityKey.UserID,
		identityKey.DeviceID,
		identityKey.PublicKey,
		identityKey.PrivateKey,
		identityKey.CreatedAt,
		identityKey.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to store identity key: %w", err)
	}

	return identityKey, nil
}

// GetIdentityKey retrieves an identity key for a user
func (km *KeyManager) GetIdentityKey(ctx context.Context, userID, deviceID string) (*IdentityKey, error) {
	query := `
		SELECT user_id, device_id, public_key, private_key, created_at, updated_at
		FROM identity_keys
		WHERE user_id = $1 AND device_id = $2
	`

	var key IdentityKey
	err := km.db.QueryRowContext(ctx, query, userID, deviceID).Scan(
		&key.UserID,
		&key.DeviceID,
		&key.PublicKey,
		&key.PrivateKey,
		&key.CreatedAt,
		&key.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get identity key: %w", err)
	}

	return &key, nil
}

// GenerateSignedPreKey generates and stores a signed pre-key
func (km *KeyManager) GenerateSignedPreKey(ctx context.Context, userID, deviceID string, keyID int) (*PreKey, error) {
	// Generate DH key pair
	keyPair, err := km.crypto.GenerateDHKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate pre-key: %w", err)
	}

	// Get identity key to sign the pre-key
	identityKey, err := km.GetIdentityKey(ctx, userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity key: %w", err)
	}

	// Sign the public key
	signature, err := km.crypto.Sign(identityKey.PrivateKey, keyPair.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign pre-key: %w", err)
	}

	preKey := &PreKey{
		ID:         uuid.New().String(),
		UserID:     userID,
		DeviceID:   deviceID,
		KeyID:      keyID,
		PublicKey:  keyPair.PublicKey,
		PrivateKey: keyPair.PrivateKey,
		Signature:  signature,
		Used:       false,
		CreatedAt:  time.Now(),
	}

	query := `
		INSERT INTO signed_prekeys (id, user_id, device_id, key_id, public_key, private_key, signature, used, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = km.db.ExecContext(ctx, query,
		preKey.ID,
		preKey.UserID,
		preKey.DeviceID,
		preKey.KeyID,
		preKey.PublicKey,
		preKey.PrivateKey,
		preKey.Signature,
		preKey.Used,
		preKey.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to store signed pre-key: %w", err)
	}

	return preKey, nil
}

// GenerateOneTimePreKeys generates and stores multiple one-time pre-keys
func (km *KeyManager) GenerateOneTimePreKeys(ctx context.Context, userID, deviceID string, count int) ([]*OneTimePreKey, error) {
	keys := make([]*OneTimePreKey, 0, count)

	tx, err := km.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO onetime_prekeys (id, user_id, device_id, key_id, public_key, private_key, used, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	for i := 0; i < count; i++ {
		keyPair, err := km.crypto.GenerateDHKeyPair()
		if err != nil {
			return nil, fmt.Errorf("failed to generate one-time pre-key: %w", err)
		}

		key := &OneTimePreKey{
			ID:         uuid.New().String(),
			UserID:     userID,
			DeviceID:   deviceID,
			KeyID:      i,
			PublicKey:  keyPair.PublicKey,
			PrivateKey: keyPair.PrivateKey,
			Used:       false,
			CreatedAt:  time.Now(),
		}

		_, err = tx.ExecContext(ctx, query,
			key.ID,
			key.UserID,
			key.DeviceID,
			key.KeyID,
			key.PublicKey,
			key.PrivateKey,
			key.Used,
			key.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to store one-time pre-key: %w", err)
		}

		keys = append(keys, key)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return keys, nil
}

// GetKeyBundle retrieves a key bundle for initiating a session
func (km *KeyManager) GetKeyBundle(ctx context.Context, userID, deviceID string) (*KeyBundle, error) {
	// Get identity key
	identityKey, err := km.GetIdentityKey(ctx, userID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity key: %w", err)
	}

	// Get signed pre-key
	signedPreKeyQuery := `
		SELECT key_id, public_key, signature
		FROM signed_prekeys
		WHERE user_id = $1 AND device_id = $2 AND used = false
		ORDER BY created_at DESC
		LIMIT 1
	`

	var signedPreKeyID int
	var signedPreKey, signedPreKeySig []byte
	err = km.db.QueryRowContext(ctx, signedPreKeyQuery, userID, deviceID).Scan(
		&signedPreKeyID,
		&signedPreKey,
		&signedPreKeySig,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get signed pre-key: %w", err)
	}

	bundle := &KeyBundle{
		UserID:          userID,
		DeviceID:        deviceID,
		IdentityKey:     identityKey.PublicKey,
		SignedPreKey:    signedPreKey,
		SignedPreKeyID:  signedPreKeyID,
		SignedPreKeySig: signedPreKeySig,
	}

	// Try to get a one-time pre-key
	oneTimeKeyQuery := `
		SELECT id, key_id, public_key
		FROM onetime_prekeys
		WHERE user_id = $1 AND device_id = $2 AND used = false
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`

	var oneTimeKeyID string
	var oneTimeKeyKeyID int
	var oneTimeKey []byte
	err = km.db.QueryRowContext(ctx, oneTimeKeyQuery, userID, deviceID).Scan(
		&oneTimeKeyID,
		&oneTimeKeyKeyID,
		&oneTimeKey,
	)

	if err == nil {
		// Mark the one-time key as used
		updateQuery := `UPDATE onetime_prekeys SET used = true, used_at = $1 WHERE id = $2`
		_, err = km.db.ExecContext(ctx, updateQuery, time.Now(), oneTimeKeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to mark one-time key as used: %w", err)
		}

		bundle.OneTimePreKey = &oneTimeKey
		bundle.OneTimePreKeyID = &oneTimeKeyKeyID
	}

	return bundle, nil
}

// RotateKeys checks and rotates keys based on the rotation policy
func (km *KeyManager) RotateKeys(ctx context.Context, userID, deviceID string) error {
	// Check identity key age
	identityKey, err := km.GetIdentityKey(ctx, userID, deviceID)
	if err == nil {
		daysSinceCreation := time.Since(identityKey.CreatedAt).Hours() / 24
		if daysSinceCreation > float64(km.policy.IdentityKeyRotationDays) {
			_, err = km.GenerateIdentityKey(ctx, userID, deviceID)
			if err != nil {
				return fmt.Errorf("failed to rotate identity key: %w", err)
			}
		}
	}

	// Check signed pre-key age
	signedPreKeyQuery := `
		SELECT created_at FROM signed_prekeys
		WHERE user_id = $1 AND device_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	var signedPreKeyCreatedAt time.Time
	err = km.db.QueryRowContext(ctx, signedPreKeyQuery, userID, deviceID).Scan(&signedPreKeyCreatedAt)
	if err == nil {
		daysSinceCreation := time.Since(signedPreKeyCreatedAt).Hours() / 24
		if daysSinceCreation > float64(km.policy.SignedPreKeyRotationDays) {
			// Get next key ID
			var maxKeyID int
			km.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(key_id), 0) FROM signed_prekeys WHERE user_id = $1 AND device_id = $2`, userID, deviceID).Scan(&maxKeyID)

			_, err = km.GenerateSignedPreKey(ctx, userID, deviceID, maxKeyID+1)
			if err != nil {
				return fmt.Errorf("failed to rotate signed pre-key: %w", err)
			}
		}
	}

	// Check one-time pre-key count
	countQuery := `SELECT COUNT(*) FROM onetime_prekeys WHERE user_id = $1 AND device_id = $2 AND used = false`
	var count int
	err = km.db.QueryRowContext(ctx, countQuery, userID, deviceID).Scan(&count)
	if err == nil && count < km.policy.OneTimePreKeyReplenishCount {
		// Replenish one-time pre-keys
		_, err = km.GenerateOneTimePreKeys(ctx, userID, deviceID, 100)
		if err != nil {
			return fmt.Errorf("failed to replenish one-time pre-keys: %w", err)
		}
	}

	return nil
}

// CleanupExpiredKeys removes expired session keys
func (km *KeyManager) CleanupExpiredKeys(ctx context.Context) error {
	query := `DELETE FROM session_keys WHERE expires_at < $1`
	_, err := km.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup expired keys: %w", err)
	}

	return nil
}
