package db

import (
	"crypto/rand"
	crypto_sha256 "crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	// Argon2 parameters (OWASP recommendations)
	argon2Time    = 3            // 3 passes
	argon2Memory  = 64 * 1024   // 64 MB
	argon2Threads = 4
	argon2KeyLen  = 32
)

// tokenPrefix generates a deterministic lookup prefix from a token.
// Uses SHA-256 truncated to 8 base64 chars. This is NOT for authentication —
// only for fast prefix-based lookup in the DB. The actual auth uses Argon2id.
// Deterministic so that the same token always produces the same prefix,
// regardless of which random salt was used for the Argon2id hash.
func tokenPrefix(token string) string {
	h := crypto_sha256.Sum256([]byte(token))
	// Take first 6 bytes (48 bits) → base64 = 8 chars
	return base64.StdEncoding.EncodeToString(h[:6])[:8]
}

// hashToken creates an Argon2id hash of the token with a random salt.
// Returns base64-encoded hash string in format: base64(salt|hash).
// The prefix is derived deterministically from the token (NOT from the random hash)
// so that lookups by token always find the correct row.
func hashToken(token string) (hashStr string, prefix string, err error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(token), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// Encode as base64: salt + hash concatenated
	result := make([]byte, len(salt)+len(key))
	copy(result[:len(salt)], salt)
	copy(result[len(salt):], key)

	fullHash := base64.StdEncoding.EncodeToString(result)
	prefix = tokenPrefix(token)
	return fullHash, prefix, nil
}

// verifyToken checks if a token matches the stored hash.
// Uses constant-time comparison to prevent timing attacks.
func verifyToken(token, storedHash string) (bool, error) {
	decoded, err := base64.StdEncoding.DecodeString(storedHash)
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}

	if len(decoded) < 16+32 { // salt + min hash
		return false, fmt.Errorf("invalid hash length")
	}

	salt := decoded[:16]
	hash := decoded[16:]

	expected := argon2.IDKey([]byte(token), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	if len(expected) != len(hash) {
		return false, fmt.Errorf("hash mismatch")
	}

	// Constant-time comparison to prevent timing attacks
	match := subtle.ConstantTimeCompare(expected, hash) == 1

	return match, nil
}

// StoreTokenHash stores a token as its Argon2id hash.
func StoreTokenHash(repo *AgentRepo, agentName, token string) error {
	hash, prefix, err := hashToken(token)
	if err != nil {
		return err
	}
	_, err = repo.db.Exec(
		`UPDATE agents SET token = $1, token_prefix = $2 WHERE name = $3`, hash, prefix, agentName,
	)
	return err
}

// CheckToken verifies a token against the stored hash.
func CheckToken(repo *AgentRepo, agentName, token string) (bool, error) {
	var storedHash string
	err := repo.db.QueryRow(`SELECT token FROM agents WHERE name = $1`, agentName).Scan(&storedHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return verifyToken(token, storedHash)
}