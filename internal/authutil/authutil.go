// Package authutil provides minimal, dependency-free auth helpers.
//
// MVP DECISION (see explanation.md section 20 "Authentication"): this app
// is designed for exactly two trusted people sharing one group, not for
// public signup. We deliberately avoid pulling in golang.org/x/crypto/bcrypt
// (an external module) and instead use stdlib crypto/sha256 with a random
// salt and a high iteration count (a manual, simplified PBKDF2-like
// stretch). This is NOT bank-grade security, but it is honest, documented,
// and adequate for a private two-person tool. See explanation.md for the
// tradeoffs and what to swap in for a public-facing deployment.
package authutil

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

const stretchIterations = 100_000

// NewSalt returns a fresh random 16-byte salt, hex encoded.
func NewSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashPassword stretches password+salt through SHA-256 many times.
func HashPassword(password, salt string) string {
	data := []byte(salt + ":" + password)
	sum := sha256.Sum256(data)
	for i := 0; i < stretchIterations; i++ {
		sum = sha256.Sum256(sum[:])
	}
	return hex.EncodeToString(sum[:])
}

// VerifyPassword does a constant-time comparison of the hash.
func VerifyPassword(password, salt, wantHash string) bool {
	got := HashPassword(password, salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(wantHash)) == 1
}

// NewSessionToken returns a random 32-byte hex token for a cookie.
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var ErrInvalidCredentials = errors.New("invalid username or password")

// recoveryAlphabet excludes visually ambiguous characters (0/O, 1/I/L)
// since a person will be copying this down by hand as a backup.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewRecoveryCode returns a human-copyable code like "XKQP-7GH2-MNB4",
// used for the no-email "forgot password" flow. It is hashed with
// HashPassword/VerifyPassword exactly like a password before storage —
// the plaintext is never persisted, only shown once to the user.
func NewRecoveryCode() (string, error) {
	const groups, groupLen = 3, 4
	raw := make([]byte, groups*groupLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b []byte
	for i, v := range raw {
		if i > 0 && i%groupLen == 0 {
			b = append(b, '-')
		}
		b = append(b, recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return string(b), nil
}
