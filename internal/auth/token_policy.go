package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomToken generates a random token string of a given length
func RandomToken(length int) (string, error) {
	b := make([]byte, (length/2)+1)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b)[:length], nil
}
