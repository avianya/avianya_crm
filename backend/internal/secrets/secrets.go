// Package secrets encrypts the integration credentials kept in the database
// (Meta access tokens, the app secret) so a database dump or backup does not
// hand them out.
//
// AES-256-GCM, keyed by SECRETS_ENCRYPTION_KEY. GCM authenticates as well as
// encrypts: a sealed value that was tampered with, or sealed for another row,
// fails to open instead of decrypting to garbage.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// KeySize is the key length AES-256 needs, in bytes.
const KeySize = 32

// version is the first byte of every sealed value, so the format can change
// later without guessing which one an old row used.
const version byte = 1

// ErrNoKey means SECRETS_ENCRYPTION_KEY is not set, so nothing can be sealed
// or opened.
var ErrNoKey = errors.New("secrets: SECRETS_ENCRYPTION_KEY is not set")

// ErrCorrupt means a sealed value could not be opened: wrong key, a value
// sealed for another purpose, or damaged data.
var ErrCorrupt = errors.New("secrets: the stored value could not be decrypted (was SECRETS_ENCRYPTION_KEY changed?)")

// ParseKey reads a 32-byte key written as base64 (standard or URL alphabet,
// padded or not) or as 64 hex characters.
func ParseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 2*KeySize {
		if key, err := hex.DecodeString(raw); err == nil {
			return key, nil
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == KeySize {
			return key, nil
		}
	}
	return nil, fmt.Errorf("must be %d random bytes as base64 or hex; generate one with: openssl rand -base64 32", KeySize)
}

// Box seals and opens values with one key. A nil *Box has no key: every call
// returns ErrNoKey, so callers need no separate "is encryption set up" check.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for key, or nil for an empty key.
func New(key []byte) (*Box, error) {
	if len(key) == 0 {
		return nil, nil
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secrets: key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. context is authenticated but not stored: Open must
// be given the same one, which ties a sealed value to its row ("meta") so it
// cannot be copied into another.
func (b *Box) Seal(plaintext []byte, context string) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	out := append([]byte{version}, nonce...)
	return b.aead.Seal(out, nonce, plaintext, []byte(context)), nil
}

// Open decrypts what Seal produced with the same context.
func (b *Box) Open(sealed []byte, context string) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < 1+n || sealed[0] != version {
		return nil, ErrCorrupt
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(context))
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

// Mask is how a saved secret is shown: dots and, for a long enough one, its
// last four characters, so two tokens can be told apart without revealing
// either. Short secrets show no characters at all. "" stays "".
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	const dots = "••••••••"
	if utf8.RuneCountInString(secret) < 16 {
		return dots
	}
	r := []rune(secret)
	return dots + string(r[len(r)-4:])
}
