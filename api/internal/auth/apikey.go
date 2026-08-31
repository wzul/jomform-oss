// API key generation & hashing for MCP/REST access.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// APIKeyPrefix brands raw keys so leaked keys are identifiable.
const APIKeyPrefix = "jfk"

// GenerateAPIKey returns (rawKey, sha256Hex) — store only the hash.
func GenerateAPIKey() (raw string, hash string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = APIKeyPrefix + "_" + hex.EncodeToString(b)
	return raw, HashAPIKey(raw), nil
}

// HashAPIKey hashes a raw key for storage/lookup.
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
