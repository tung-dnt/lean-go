package domain

import "golang.org/x/crypto/bcrypt"

// HashPassword hashes a plain-text password so that it can be stored in the database.
// This must be called before persisting any password; nothing in the database layer hashes for you.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}
