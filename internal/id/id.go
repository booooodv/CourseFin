package id

import (
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

// New returns a new canonical 32-character lowercase hex UUIDv4.
func New() string {
	u := uuid.New()
	return hex.EncodeToString(u[:])
}

// Normalize accepts a 32-character hex ID or a standard 36-character dashed UUID
// and returns the canonical 32-character lowercase hex representation.
func Normalize(s string) (string, bool) {
	switch len(s) {
	case 32:
		if !isHex(s) {
			return "", false
		}
		return strings.ToLower(s), true
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return "", false
		}
		compact := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
		if !isHex(compact) {
			return "", false
		}
		return strings.ToLower(compact), true
	default:
		return "", false
	}
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		isUpper := c >= 'A' && c <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}
