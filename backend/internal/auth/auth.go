// Package auth holds the small pieces of login that are not about HTTP or the
// database: hashing passwords and making session tokens.
//
// Passwords are hashed with bcrypt, which is slow on purpose and salts every
// hash. Session tokens are 32 random bytes; the cookie carries the token and
// the database only its SHA-256, so a leaked sessions table cannot be used to
// log in.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor: about a quarter of a second per check on a
// typical server, which is slow for an attacker and fine for a login.
const bcryptCost = 12

// HashPassword returns the bcrypt hash of a password.
func HashPassword(password string) (string, error) {
	// bcrypt only uses the first 72 bytes; refuse rather than silently ignore
	// the rest (model.ValidatePassword caps length well below this in runes,
	// but multi-byte characters could still pass it).
	if len(password) > 72 {
		return "", errors.New("auth: password longer than 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// dummyHash is checked against when no account matches, so a login for an
// unknown email takes as long as one for a known email with a wrong password
// and the timing does not tell which emails have accounts.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password-just-timing"), bcryptCost)

// CheckPassword reports whether password matches hash. An empty hash (no
// account, or no password set) never matches, but still takes as long.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken returns a fresh session token for the cookie, and the hash to
// store for it.
func NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is what is stored for a session token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
