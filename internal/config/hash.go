package config

import (
	"crypto/sha256"
	"encoding/hex"
)

// ComputeSimpleHash computes a simple hash from a byte slice.
func ComputeSimpleHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
