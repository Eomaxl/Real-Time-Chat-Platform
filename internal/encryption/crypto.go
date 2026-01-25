package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

var (
	ErrInvalidKeySize    = errors.New("invalid key size")
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrInvalidMAC        = errors.New("invalid MAC")
)

const (
	// Key sizes
	IdentityKeySize = 32
	PreKeySize      = 32
	ChainKeySize    = 32
	MessageKeySize  = 32
	MacKeySize      = 32
	IVSize          = 16

	// HKDF info strings
	InfoRootKey    = "WhisperRootKey"
	InfoChainKey   = "WhisperChainKey"
	InfoMessageKey = "WhisperMessageKeys"
)

// CryptoEngine handles all cryptographic operations
type CryptoEngine struct{}

// NewCryptoEngine creates a new crypto engine
func NewCryptoEngine() *CryptoEngine {
	return &CryptoEngine{}
}

// GenerateIdentityKeyPair generates a new identity key pair using Ed25519
func (c *CryptoEngine) GenerateIdentityKeyPair() (*KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate identity key: %w", err)
	}

	return &KeyPair{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}, nil
}

// GenerateDHKeyPair generates a Diffie-Hellman key pair using Curve25519
func (c *CryptoEngine) GenerateDHKeyPair() (*KeyPair, error) {
	var privateKey [32]byte
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	var publicKey [32]byte
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	return &KeyPair{
		PublicKey:  publicKey[:],
		PrivateKey: privateKey[:],
	}, nil
}

// PerformDH performs Diffie-Hellman key agreement
func (c *CryptoEngine) PerformDH(privateKey, publicKey []byte) ([]byte, error) {
	if len(privateKey) != 32 || len(publicKey) != 32 {
		return nil, ErrInvalidKeySize
	}

	var sharedSecret [32]byte
	var priv, pub [32]byte
	copy(priv[:], privateKey)
	copy(pub[:], publicKey)

	curve25519.ScalarMult(&sharedSecret, &priv, &pub)

	return sharedSecret[:], nil
}

// Sign signs data using Ed25519
func (c *CryptoEngine) Sign(privateKey, data []byte) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, ErrInvalidKeySize
	}

	signature := ed25519.Sign(privateKey, data)
	return signature, nil
}

// Verify verifies an Ed25519 signature
func (c *CryptoEngine) Verify(publicKey, data, signature []byte) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidKeySize
	}

	if !ed25519.Verify(publicKey, data, signature) {
		return ErrInvalidSignature
	}

	return nil
}

// DeriveKeys derives multiple keys from a shared secret using HKDF
func (c *CryptoEngine) DeriveKeys(sharedSecret, salt []byte, info string, outputLength int) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, sharedSecret, salt, []byte(info))

	output := make([]byte, outputLength)
	if _, err := io.ReadFull(hkdfReader, output); err != nil {
		return nil, fmt.Errorf("failed to derive keys: %w", err)
	}

	return output, nil
}

// DeriveRootKey derives a new root key and chain key from the current root key
func (c *CryptoEngine) DeriveRootKey(rootKey, dhOutput []byte) (newRootKey, chainKey []byte, err error) {
	// Use HKDF to derive 64 bytes (32 for root key, 32 for chain key)
	derived, err := c.DeriveKeys(dhOutput, rootKey, InfoRootKey, 64)
	if err != nil {
		return nil, nil, err
	}

	return derived[:32], derived[32:], nil
}

// DeriveChainKey derives the next chain key from the current chain key
func (c *CryptoEngine) DeriveChainKey(chainKey []byte) ([]byte, error) {
	if len(chainKey) != ChainKeySize {
		return nil, ErrInvalidKeySize
	}

	// Use HMAC-SHA256 with constant input
	mac := hmac.New(sha256.New, chainKey)
	mac.Write([]byte{0x02})

	return mac.Sum(nil), nil
}

// DeriveMessageKey derives a message key from a chain key
func (c *CryptoEngine) DeriveMessageKey(chainKey []byte) ([]byte, error) {
	if len(chainKey) != ChainKeySize {
		return nil, ErrInvalidKeySize
	}

	// Use HMAC-SHA256 with constant input
	mac := hmac.New(sha256.New, chainKey)
	mac.Write([]byte{0x01})

	return mac.Sum(nil), nil
}

// Encrypt encrypts plaintext using AES-256-CBC with HMAC-SHA256
func (c *CryptoEngine) Encrypt(messageKey, plaintext []byte) (ciphertext, iv, mac []byte, err error) {
	if len(messageKey) != MessageKeySize {
		return nil, nil, nil, ErrInvalidKeySize
	}

	// Derive encryption and MAC keys from message key
	encKey := messageKey[:16]
	macKey := messageKey[16:]

	// Generate random IV
	iv = make([]byte, IVSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Pad plaintext to AES block size
	paddedPlaintext := c.pkcs7Pad(plaintext, aes.BlockSize)

	// Create AES cipher
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Encrypt using CBC mode
	ciphertext = make([]byte, len(paddedPlaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, paddedPlaintext)

	// Compute HMAC
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	mac = h.Sum(nil)

	return ciphertext, iv, mac, nil
}

// Decrypt decrypts ciphertext using AES-256-CBC with HMAC-SHA256 verification
func (c *CryptoEngine) Decrypt(messageKey, ciphertext, iv, mac []byte) ([]byte, error) {
	if len(messageKey) != MessageKeySize {
		return nil, ErrInvalidKeySize
	}

	if len(iv) != IVSize {
		return nil, ErrInvalidCiphertext
	}

	// Derive encryption and MAC keys from message key
	encKey := messageKey[:16]
	macKey := messageKey[16:]

	// Verify HMAC
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	expectedMAC := h.Sum(nil)

	if !hmac.Equal(mac, expectedMAC) {
		return nil, ErrInvalidMAC
	}

	// Create AES cipher
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Decrypt using CBC mode
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove padding
	unpaddedPlaintext, err := c.pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("failed to unpad plaintext: %w", err)
	}

	return unpaddedPlaintext, nil
}

// pkcs7Pad adds PKCS#7 padding to data
func (c *CryptoEngine) pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := make([]byte, padding)
	for i := range padText {
		padText[i] = byte(padding)
	}
	return append(data, padText...)
}

// pkcs7Unpad removes PKCS#7 padding from data
func (c *CryptoEngine) pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	padding := int(data[len(data)-1])
	if padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid padding")
	}

	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}

	return data[:len(data)-padding], nil
}

// GenerateRandomBytes generates cryptographically secure random bytes
func (c *CryptoEngine) GenerateRandomBytes(n int) ([]byte, error) {
	bytes := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return bytes, nil
}

// Hash computes SHA-512 hash of data
func (c *CryptoEngine) Hash(data []byte) []byte {
	hash := sha512.Sum512(data)
	return hash[:]
}

// IntToBytes converts an integer to bytes
func IntToBytes(n int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(n))
	return b
}
