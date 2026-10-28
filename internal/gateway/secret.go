package gateway

// secret.go — password/key storage.
//
// The dashboard password and the gateway key used to sit in the `settings`
// table as plaintext. Anyone who read the database (a backup, a stray copy,
// the export endpoint before it was gated) also owned the panel and the API.
// They are now stored as PBKDF2-SHA256 verifiers:
//
//	pbkdf2-sha256$<iterations>$<salt-b64>$<key-b64>
//
// Verification is deliberately the only way back: the plaintext never has to
// be recoverable, so a database leak is a leak of the verifier, not the
// password. Verified credentials are memoised by sha256(presented) so the
// 5-second dashboard polling does not pay the KDF cost every time.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	secretScheme  = "pbkdf2-sha256"
	secretIters   = 120_000
	secretSaltLen = 16
	secretKeyLen  = 32
	cacheMax      = 64
)

// hashSecret returns a self-describing verifier string for pw.
func hashSecret(pw string) (string, error) {
	salt := make([]byte, secretSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, secretIters, secretKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", secretScheme, secretIters,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifySecret reports whether pw matches the stored verifier.
func verifySecret(verifier, pw string) bool {
	parts := strings.Split(verifier, "$")
	if len(parts) != 4 || parts[0] != secretScheme {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters <= 0 || iters > 5_000_000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) == 0 || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// secretCache memoises successful verifications by sha256(presented).
type secretCache struct {
	mu  sync.Mutex
	set map[string]struct{}
}

func newSecretCache() *secretCache { return &secretCache{set: map[string]struct{}{}} }

func (c *secretCache) key(verifier, pw string) string {
	sum := sha256.Sum256([]byte(verifier + "\x00" + pw))
	return base64.RawStdEncoding.EncodeToString(sum[:16])
}

func (c *secretCache) get(verifier, pw string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.set[c.key(verifier, pw)]
	return ok
}

func (c *secretCache) put(verifier, pw string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.set) >= cacheMax {
		c.set = map[string]struct{}{} // cheap reset: bounded memory beats precision here
	}
	c.set[c.key(verifier, pw)] = struct{}{}
}

// reset drops every memoised credential (called when a secret changes).
func (c *secretCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.set = map[string]struct{}{}
}

// checkSecret verifies pw against verifier, using and filling the cache.
func (s *Server) checkSecret(verifier, pw string) bool {
	if verifier == "" || pw == "" {
		return false
	}
	if s.secretOK.get(verifier, pw) {
		return true
	}
	if !verifySecret(verifier, pw) {
		return false
	}
	s.secretOK.put(verifier, pw)
	return true
}
