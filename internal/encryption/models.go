package encryption

import (
	"time"
)

// KeyType represents the type of encryption key
type KeyType string

const (
	KeyTypeIdentity     KeyType = "identity"
	KeyTypePreKey       KeyType = "prekey"
	KeyTypeSignedPreKey KeyType = "signed_prekey"
	KeyTypeOneTime      KeyType = "onetime"
	KeyTypeSession      KeyType = "session"
)

// KeyPair represents a public/private key pair
type KeyPair struct {
	PublicKey  []byte `json:"public_key" db:"public_key"`
	PrivateKey []byte `json:"-" db:"private_key"` // Never expose in JSON
}

// IdentityKey represents a user's long-term identity key
type IdentityKey struct {
	UserID     string    `json:"user_id" db:"user_id"`
	DeviceID   string    `json:"device_id" db:"device_id"`
	PublicKey  []byte    `json:"public_key" db:"public_key"`
	PrivateKey []byte    `json:"-" db:"private_key"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// PreKey represents a medium-term pre-key for key exchange
type PreKey struct {
	ID         string     `json:"id" db:"id"`
	UserID     string     `json:"user_id" db:"user_id"`
	DeviceID   string     `json:"device_id" db:"device_id"`
	KeyID      int        `json:"key_id" db:"key_id"`
	PublicKey  []byte     `json:"public_key" db:"public_key"`
	PrivateKey []byte     `json:"-" db:"private_key"`
	Signature  []byte     `json:"signature" db:"signature"`
	Used       bool       `json:"used" db:"used"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UsedAt     *time.Time `json:"used_at,omitempty" db:"used_at"`
}

// OneTimePreKey represents a one-time use pre-key
type OneTimePreKey struct {
	ID         string     `json:"id" db:"id"`
	UserID     string     `json:"user_id" db:"user_id"`
	DeviceID   string     `json:"device_id" db:"device_id"`
	KeyID      int        `json:"key_id" db:"key_id"`
	PublicKey  []byte     `json:"public_key" db:"public_key"`
	PrivateKey []byte     `json:"-" db:"private_key"`
	Used       bool       `json:"used" db:"used"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UsedAt     *time.Time `json:"used_at,omitempty" db:"used_at"`
}

// SessionKey represents an ephemeral session key for message encryption
type SessionKey struct {
	ID               string    `json:"id" db:"id"`
	SenderUserID     string    `json:"sender_user_id" db:"sender_user_id"`
	SenderDeviceID   string    `json:"sender_device_id" db:"sender_device_id"`
	ReceiverUserID   string    `json:"receiver_user_id" db:"receiver_user_id"`
	ReceiverDeviceID string    `json:"receiver_device_id" db:"receiver_device_id"`
	RootKey          []byte    `json:"-" db:"root_key"`
	ChainKey         []byte    `json:"-" db:"chain_key"`
	MessageNumber    int       `json:"message_number" db:"message_number"`
	PreviousCounter  int       `json:"previous_counter" db:"previous_counter"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
	ExpiresAt        time.Time `json:"expires_at" db:"expires_at"`
}

// KeyBundle represents a bundle of keys for initiating a session
type KeyBundle struct {
	UserID          string  `json:"user_id"`
	DeviceID        string  `json:"device_id"`
	IdentityKey     []byte  `json:"identity_key"`
	SignedPreKey    []byte  `json:"signed_prekey"`
	SignedPreKeyID  int     `json:"signed_prekey_id"`
	SignedPreKeySig []byte  `json:"signed_prekey_signature"`
	OneTimePreKey   *[]byte `json:"onetime_prekey,omitempty"`
	OneTimePreKeyID *int    `json:"onetime_prekey_id,omitempty"`
}

// EncryptedMessage represents an encrypted message
type EncryptedMessage struct {
	SenderUserID     string `json:"sender_user_id"`
	SenderDeviceID   string `json:"sender_device_id"`
	ReceiverUserID   string `json:"receiver_user_id"`
	ReceiverDeviceID string `json:"receiver_device_id"`
	MessageType      string `json:"message_type"` // "prekey" or "message"
	Ciphertext       []byte `json:"ciphertext"`
	EphemeralKey     []byte `json:"ephemeral_key,omitempty"`
	Counter          int    `json:"counter"`
	PreviousCounter  int    `json:"previous_counter"`
}

// KeyRotationPolicy defines when keys should be rotated
type KeyRotationPolicy struct {
	IdentityKeyRotationDays     int `json:"identity_key_rotation_days"`
	SignedPreKeyRotationDays    int `json:"signed_prekey_rotation_days"`
	OneTimePreKeyReplenishCount int `json:"onetime_prekey_replenish_count"`
	SessionKeyMaxMessages       int `json:"session_key_max_messages"`
	SessionKeyMaxAgeDays        int `json:"session_key_max_age_days"`
}

// DefaultKeyRotationPolicy returns the default key rotation policy
func DefaultKeyRotationPolicy() KeyRotationPolicy {
	return KeyRotationPolicy{
		IdentityKeyRotationDays:     365,  // Rotate identity keys yearly
		SignedPreKeyRotationDays:    30,   // Rotate signed pre-keys monthly
		OneTimePreKeyReplenishCount: 10,   // Replenish when below 10 one-time keys
		SessionKeyMaxMessages:       1000, // Rotate session after 1000 messages
		SessionKeyMaxAgeDays:        7,    // Rotate session after 7 days
	}
}
