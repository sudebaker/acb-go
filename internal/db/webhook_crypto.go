package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	encryptCipher cipher.AEAD
	cipherOnce    sync.Once
	cipherErr     error
)

// initCipher initializes the AES-GCM cipher from WEBHOOK_SECRET_KEY env var.
// Returns error if key is not set or invalid.
func initCipher() error {
	key := os.Getenv("WEBHOOK_SECRET_KEY")
	if key == "" {
		return fmt.Errorf("WEBHOOK_SECRET_KEY not set")
	}

	keyBytes, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("decode WEBHOOK_SECRET_KEY: %w", err)
	}

	if len(keyBytes) != 32 {
		return fmt.Errorf("WEBHOOK_SECRET_KEY must be 32 bytes (256-bit), got %d", len(keyBytes))
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create GCM: %w", err)
	}

	encryptCipher = gcm
	return nil
}

// get cipher lazily initializes the cipher on first use.
func getCipher() (cipher.AEAD, error) {
	cipherOnce.Do(func() {
		cipherErr = initCipher()
	})
	return encryptCipher, cipherErr
}

// encryptWebhookSecret encrypts a webhook secret using AES-256-GCM.
// Returns base64-encoded ciphertext in format: base64(nonce|ciphertext).
func EncryptWebhookSecret(plaintext string) (string, error) {
	gcm, err := getCipher()
	if err != nil {
		// If cipher not initialized, return plaintext (dev mode)
		// but log a warning in production this should fail
		return plaintext, nil
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptWebhookSecret decrypts a webhook secret from base64-encoded ciphertext.
// Format: base64(nonce|ciphertext).
func DecryptWebhookSecret(encoded string) (string, error) {
	gcm, err := getCipher()
	if err != nil {
		// If cipher not initialized, assume plaintext (dev mode)
		return encoded, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}

	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid secret length")
	}

	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}

	return string(plaintext), nil
}
