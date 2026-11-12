// Package auth holds Browser Foreman's sign-in building blocks: argon2id password hashes, random secrets for tokens and
// sessions, and the sign-in rate limiter. It keeps no state of its own except the limiter's counts.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (RFC 9106's second recommended option: 64 MiB, 3 passes). They are written into every hash, so
// raising them later still verifies the old hashes.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLength is the shortest password accepted. Length does more for a password than any character rule.
const MinPasswordLength = 12

// CheckPassword refuses a password too short or too long to hash sensibly.
func CheckPassword(pw string) error {
	if n := utf8.RuneCountInString(pw); n < MinPasswordLength {
		return fmt.Errorf("a password needs at least %d characters", MinPasswordLength)
	}
	if len(pw) > 1024 {
		return errors.New("a password can be at most 1024 bytes")
	}
	return nil
}

// HashPassword returns a PHC string: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>.
func HashPassword(pw string) (string, error) {
	if err := CheckPassword(pw); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64(salt), b64(key)), nil
}

// VerifyPassword reports whether pw matches the PHC string, comparing in constant time.
func VerifyPassword(phc, pw string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 || m > 1<<22 || t > 32 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked when the user does not exist, so a missing name takes as long as a wrong password.
var dummyHash, _ = HashPassword("not-a-real-password-just-timing")

// VerifyOrWaste verifies pw against phc, or spends the same time on a dummy hash when phc is empty.
func VerifyOrWaste(phc, pw string) bool {
	if phc == "" {
		VerifyPassword(dummyHash, pw)
		return false
	}
	return VerifyPassword(phc, pw)
}

// NewSecret returns prefix + 256 random bits, hex encoded: "brofo_…" for API tokens, plain for session ids.
func NewSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// HashSecret is what the store keeps for a token or session: SHA-256, hex. The secrets are random and long, so a
// fast hash is right here (passwords are the ones that need argon2id).
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Limiter counts failed sign-ins per key (a user name, a client address) and locks a key out for a while after too
// many inside a window.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	lockout time.Duration
	fails   map[string][]time.Time
	locked  map[string]time.Time
}

func NewLimiter(max int, window, lockout time.Duration) *Limiter {
	return &Limiter{max: max, window: window, lockout: lockout, fails: map[string][]time.Time{}, locked: map[string]time.Time{}}
}

// Allowed reports whether a key may try now, and if not, when it may.
func (l *Limiter) Allowed(key string, now time.Time) (bool, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until, ok := l.locked[key]; ok {
		if now.Before(until) {
			return false, until
		}
		delete(l.locked, key)
	}
	return true, time.Time{}
}

// Fail records a failed attempt and locks the key out once it reaches the limit within the window.
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	l.fails[key] = kept
	if len(kept) >= l.max {
		l.locked[key] = now.Add(l.lockout)
		delete(l.fails, key)
	}
}

// Succeed clears a key's failures.
func (l *Limiter) Succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
