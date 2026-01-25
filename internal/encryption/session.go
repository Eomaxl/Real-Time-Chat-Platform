package encryption

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionManager manages encryption sessions between users
type SessionManager struct {
	db         *sql.DB
	crypto     *CryptoEngine
	keyManager *KeyManager
	policy     KeyRotationPolicy
}

// NewSessionManager creates a new session manager
func NewSessionManager(db *sql.DB, keyManager *KeyManager, policy KeyRotationPolicy) *SessionManager {
	return &SessionManager{
		db:         db,
		crypto:     NewCryptoEngine(),
		keyManager: keyManager,
		policy:     policy,
	}
}

// InitiateSession initiates a new encryption session with a recipient
func (sm *SessionManager) InitiateSession(ctx context.Context, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID string) (*SessionKey, error) {
	// Get sender's identity key
	senderIdentity, err := sm.keyManager.GetIdentityKey(ctx, senderUserID, senderDeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sender identity key: %w", err)
	}

	// Get receiver's key bundle
	receiverBundle, err := sm.keyManager.GetKeyBundle(ctx, receiverUserID, receiverDeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get receiver key bundle: %w", err)
	}

	// Generate ephemeral key pair
	ephemeralKeyPair, err := sm.crypto.GenerateDHKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Perform X3DH key agreement
	// DH1 = DH(IK_A, SPK_B)
	dh1, err := sm.crypto.PerformDH(senderIdentity.PrivateKey, receiverBundle.SignedPreKey)
	if err != nil {
		return nil, fmt.Errorf("failed to perform DH1: %w", err)
	}

	// DH2 = DH(EK_A, IK_B)
	dh2, err := sm.crypto.PerformDH(ephemeralKeyPair.PrivateKey, receiverBundle.IdentityKey)
	if err != nil {
		return nil, fmt.Errorf("failed to perform DH2: %w", err)
	}

	// DH3 = DH(EK_A, SPK_B)
	dh3, err := sm.crypto.PerformDH(ephemeralKeyPair.PrivateKey, receiverBundle.SignedPreKey)
	if err != nil {
		return nil, fmt.Errorf("failed to perform DH3: %w", err)
	}

	// Concatenate DH outputs
	var dhOutput []byte
	dhOutput = append(dhOutput, dh1...)
	dhOutput = append(dhOutput, dh2...)
	dhOutput = append(dhOutput, dh3...)

	// If one-time pre-key is available, perform DH4
	if receiverBundle.OneTimePreKey != nil {
		dh4, err := sm.crypto.PerformDH(ephemeralKeyPair.PrivateKey, *receiverBundle.OneTimePreKey)
		if err != nil {
			return nil, fmt.Errorf("failed to perform DH4: %w", err)
		}
		dhOutput = append(dhOutput, dh4...)
	}

	// Derive root key and chain key
	sharedSecret := sm.crypto.Hash(dhOutput)
	rootKey, chainKey, err := sm.crypto.DeriveRootKey(sharedSecret[:32], sharedSecret[32:])
	if err != nil {
		return nil, fmt.Errorf("failed to derive root key: %w", err)
	}

	// Create session key
	sessionKey := &SessionKey{
		ID:               uuid.New().String(),
		SenderUserID:     senderUserID,
		SenderDeviceID:   senderDeviceID,
		ReceiverUserID:   receiverUserID,
		ReceiverDeviceID: receiverDeviceID,
		RootKey:          rootKey,
		ChainKey:         chainKey,
		MessageNumber:    0,
		PreviousCounter:  0,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		ExpiresAt:        time.Now().AddDate(0, 0, sm.policy.SessionKeyMaxAgeDays),
	}

	// Store session key
	err = sm.StoreSessionKey(ctx, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to store session key: %w", err)
	}

	return sessionKey, nil
}

// StoreSessionKey stores a session key in the database
func (sm *SessionManager) StoreSessionKey(ctx context.Context, sessionKey *SessionKey) error {
	query := `
		INSERT INTO session_keys (id, sender_user_id, sender_device_id, receiver_user_id, receiver_device_id, 
		                          root_key, chain_key, message_number, previous_counter, created_at, updated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (sender_user_id, sender_device_id, receiver_user_id, receiver_device_id)
		DO UPDATE SET
			root_key = EXCLUDED.root_key,
			chain_key = EXCLUDED.chain_key,
			message_number = EXCLUDED.message_number,
			previous_counter = EXCLUDED.previous_counter,
			updated_at = EXCLUDED.updated_at,
			expires_at = EXCLUDED.expires_at
	`

	_, err := sm.db.ExecContext(ctx, query,
		sessionKey.ID,
		sessionKey.SenderUserID,
		sessionKey.SenderDeviceID,
		sessionKey.ReceiverUserID,
		sessionKey.ReceiverDeviceID,
		sessionKey.RootKey,
		sessionKey.ChainKey,
		sessionKey.MessageNumber,
		sessionKey.PreviousCounter,
		sessionKey.CreatedAt,
		sessionKey.UpdatedAt,
		sessionKey.ExpiresAt,
	)

	return err
}

// GetSessionKey retrieves a session key
func (sm *SessionManager) GetSessionKey(ctx context.Context, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID string) (*SessionKey, error) {
	query := `
		SELECT id, sender_user_id, sender_device_id, receiver_user_id, receiver_device_id,
		       root_key, chain_key, message_number, previous_counter, created_at, updated_at, expires_at
		FROM session_keys
		WHERE sender_user_id = $1 AND sender_device_id = $2 
		  AND receiver_user_id = $3 AND receiver_device_id = $4
	`

	var key SessionKey
	err := sm.db.QueryRowContext(ctx, query, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID).Scan(
		&key.ID,
		&key.SenderUserID,
		&key.SenderDeviceID,
		&key.ReceiverUserID,
		&key.ReceiverDeviceID,
		&key.RootKey,
		&key.ChainKey,
		&key.MessageNumber,
		&key.PreviousCounter,
		&key.CreatedAt,
		&key.UpdatedAt,
		&key.ExpiresAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get session key: %w", err)
	}

	return &key, nil
}

// EncryptMessage encrypts a message for a recipient
func (sm *SessionManager) EncryptMessage(ctx context.Context, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID string, plaintext []byte) (*EncryptedMessage, error) {
	// Get or create session
	sessionKey, err := sm.GetSessionKey(ctx, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID)
	if err != nil {
		// Session doesn't exist, initiate new session
		sessionKey, err = sm.InitiateSession(ctx, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID)
		if err != nil {
			return nil, fmt.Errorf("failed to initiate session: %w", err)
		}
	}

	// Check if session needs rotation
	if sessionKey.MessageNumber >= sm.policy.SessionKeyMaxMessages || time.Now().After(sessionKey.ExpiresAt) {
		// Rotate session
		sessionKey, err = sm.InitiateSession(ctx, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID)
		if err != nil {
			return nil, fmt.Errorf("failed to rotate session: %w", err)
		}
	}

	// Derive message key from chain key
	messageKey, err := sm.crypto.DeriveMessageKey(sessionKey.ChainKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive message key: %w", err)
	}

	// Encrypt the message
	ciphertext, iv, mac, err := sm.crypto.Encrypt(messageKey, plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt message: %w", err)
	}

	// Combine ciphertext, IV, and MAC
	encryptedData := append(iv, mac...)
	encryptedData = append(encryptedData, ciphertext...)

	encryptedMsg := &EncryptedMessage{
		SenderUserID:     senderUserID,
		SenderDeviceID:   senderDeviceID,
		ReceiverUserID:   receiverUserID,
		ReceiverDeviceID: receiverDeviceID,
		MessageType:      "message",
		Ciphertext:       encryptedData,
		Counter:          sessionKey.MessageNumber,
		PreviousCounter:  sessionKey.PreviousCounter,
	}

	// Update chain key for next message (forward secrecy)
	newChainKey, err := sm.crypto.DeriveChainKey(sessionKey.ChainKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive new chain key: %w", err)
	}

	sessionKey.ChainKey = newChainKey
	sessionKey.MessageNumber++
	sessionKey.UpdatedAt = time.Now()

	// Store updated session key
	err = sm.StoreSessionKey(ctx, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to update session key: %w", err)
	}

	return encryptedMsg, nil
}

// DecryptMessage decrypts a message from a sender
func (sm *SessionManager) DecryptMessage(ctx context.Context, encryptedMsg *EncryptedMessage) ([]byte, error) {
	// Get session key (receiver's perspective)
	sessionKey, err := sm.GetSessionKey(ctx, encryptedMsg.ReceiverUserID, encryptedMsg.ReceiverDeviceID, encryptedMsg.SenderUserID, encryptedMsg.SenderDeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session key: %w", err)
	}

	// Extract IV, MAC, and ciphertext
	if len(encryptedMsg.Ciphertext) < IVSize+32 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}

	iv := encryptedMsg.Ciphertext[:IVSize]
	mac := encryptedMsg.Ciphertext[IVSize : IVSize+32]
	ciphertext := encryptedMsg.Ciphertext[IVSize+32:]

	// Derive message key from chain key
	messageKey, err := sm.crypto.DeriveMessageKey(sessionKey.ChainKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive message key: %w", err)
	}

	// Decrypt the message
	plaintext, err := sm.crypto.Decrypt(messageKey, ciphertext, iv, mac)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt message: %w", err)
	}

	// Update chain key for next message (forward secrecy)
	newChainKey, err := sm.crypto.DeriveChainKey(sessionKey.ChainKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive new chain key: %w", err)
	}

	sessionKey.ChainKey = newChainKey
	sessionKey.MessageNumber++
	sessionKey.UpdatedAt = time.Now()

	// Store updated session key
	err = sm.StoreSessionKey(ctx, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to update session key: %w", err)
	}

	return plaintext, nil
}

// DeleteSession deletes a session between two users
func (sm *SessionManager) DeleteSession(ctx context.Context, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID string) error {
	query := `
		DELETE FROM session_keys
		WHERE sender_user_id = $1 AND sender_device_id = $2 
		  AND receiver_user_id = $3 AND receiver_device_id = $4
	`

	_, err := sm.db.ExecContext(ctx, query, senderUserID, senderDeviceID, receiverUserID, receiverDeviceID)
	return err
}
