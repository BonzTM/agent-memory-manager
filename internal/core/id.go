package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateID creates a random ID with the given prefix (e.g. "evt_", "mem_").
// Panics if crypto/rand fails — an unrecoverable condition.
func GenerateID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}
