package httpapi

import (
	"crypto/rand"
	"encoding/hex"
)

// generateSID returns a Twilio-style identifier: a 2-letter prefix
// followed by 32 hex characters (16 random bytes hex-encoded).
//
// Examples: "SM" + 32 hex = 34-char SMS SID; "CA" + 32 hex = 34-char Call SID.
//
// Panics on rand.Read failure. crypto/rand draws from the OS RNG; a failure
// here means the system is in a state where graceful handling won't help.
func generateSID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return prefix + hex.EncodeToString(b[:])
}
