-- Migration: Add encryption tables for end-to-end encryption
-- This migration adds tables for managing encryption keys and sessions

-- Identity keys table (long-term identity keys)
CREATE TABLE IF NOT EXISTS identity_keys (
    user_id VARCHAR(255) NOT NULL,
    device_id VARCHAR(255) NOT NULL,
    public_key BYTEA NOT NULL,
    private_key BYTEA NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (user_id, device_id)
);

CREATE INDEX idx_identity_keys_user ON identity_keys(user_id);

-- Signed pre-keys table (medium-term keys signed by identity key)
CREATE TABLE IF NOT EXISTS signed_prekeys (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    device_id VARCHAR(255) NOT NULL,
    key_id INTEGER NOT NULL,
    public_key BYTEA NOT NULL,
    private_key BYTEA NOT NULL,
    signature BYTEA NOT NULL,
    used BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    used_at TIMESTAMP WITH TIME ZONE,
    UNIQUE (user_id, device_id, key_id)
);

CREATE INDEX idx_signed_prekeys_user_device ON signed_prekeys(user_id, device_id);
CREATE INDEX idx_signed_prekeys_unused ON signed_prekeys(user_id, device_id, used) WHERE used = FALSE;

-- One-time pre-keys table (single-use keys for perfect forward secrecy)
CREATE TABLE IF NOT EXISTS onetime_prekeys (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    device_id VARCHAR(255) NOT NULL,
    key_id INTEGER NOT NULL,
    public_key BYTEA NOT NULL,
    private_key BYTEA NOT NULL,
    used BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    used_at TIMESTAMP WITH TIME ZONE,
    UNIQUE (user_id, device_id, key_id)
);

CREATE INDEX idx_onetime_prekeys_user_device ON onetime_prekeys(user_id, device_id);
CREATE INDEX idx_onetime_prekeys_unused ON onetime_prekeys(user_id, device_id, used) WHERE used = FALSE;

-- Session keys table (ephemeral session keys for message encryption)
CREATE TABLE IF NOT EXISTS session_keys (
    id VARCHAR(255) PRIMARY KEY,
    sender_user_id VARCHAR(255) NOT NULL,
    sender_device_id VARCHAR(255) NOT NULL,
    receiver_user_id VARCHAR(255) NOT NULL,
    receiver_device_id VARCHAR(255) NOT NULL,
    root_key BYTEA NOT NULL,
    chain_key BYTEA NOT NULL,
    message_number INTEGER DEFAULT 0,
    previous_counter INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    UNIQUE (sender_user_id, sender_device_id, receiver_user_id, receiver_device_id)
);

CREATE INDEX idx_session_keys_sender ON session_keys(sender_user_id, sender_device_id);
CREATE INDEX idx_session_keys_receiver ON session_keys(receiver_user_id, receiver_device_id);
CREATE INDEX idx_session_keys_expires ON session_keys(expires_at);

-- Add encryption fields to messages table
ALTER TABLE messages ADD COLUMN IF NOT EXISTS encrypted BOOLEAN DEFAULT FALSE;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sender_device_id VARCHAR(255);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS receiver_device_id VARCHAR(255);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS encryption_version VARCHAR(50);

-- Create index for encrypted messages
CREATE INDEX IF NOT EXISTS idx_messages_encrypted ON messages(encrypted) WHERE encrypted = TRUE;

-- Comments for documentation
COMMENT ON TABLE identity_keys IS 'Long-term identity keys for users (Ed25519)';
COMMENT ON TABLE signed_prekeys IS 'Medium-term pre-keys signed by identity key (Curve25519)';
COMMENT ON TABLE onetime_prekeys IS 'Single-use pre-keys for perfect forward secrecy (Curve25519)';
COMMENT ON TABLE session_keys IS 'Ephemeral session keys for message encryption (Double Ratchet)';
