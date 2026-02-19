package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	SaltLen   = 16
	KeyLen    = 32
	NonceLen  = 12
	VerifyStr = "essh-verify"
)

// GenerateSalt returns a cryptographically random salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	return salt, nil
}

// DeriveKey uses Argon2id to derive a 32-byte AES key from a password and salt.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, KeyLen)
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
// Returns the hex-encoded nonce+ciphertext.
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	nonce := make([]byte, NonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts hex-encoded nonce+ciphertext using AES-256-GCM.
func Decrypt(key []byte, encoded string) (string, error) {
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decoding hex: %w", err)
	}
	if len(data) < NonceLen {
		return "", fmt.Errorf("ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	nonce := data[:NonceLen]
	ciphertext := data[NonceLen:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}
	return string(plaintext), nil
}
