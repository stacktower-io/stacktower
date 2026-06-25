package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// hashKey generates a cache key by hashing the components.
// The key format is: prefix:hash(parts...)
func hashKey(prefix string, parts ...interface{}) string {
	data, err := json.Marshal(parts)
	if err != nil {
		// Marshal failures are practically impossible for the plain structs
		// and strings used as key parts, but if one happens, make sure
		// different failures can't collide on the hash of empty input.
		data = []byte("marshal-error:" + err.Error())
	}
	hash := sha256.Sum256(data)
	// Use full SHA-256 hash (64 hex chars / 256 bits) to prevent collisions
	return fmt.Sprintf("%s:%s", prefix, hex.EncodeToString(hash[:]))
}

// Hash computes a SHA-256 hash of the input data.
// Returns the full 64-character hex string.
func Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
