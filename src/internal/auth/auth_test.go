package auth

import (
	"strings"
	"testing"
	"time"
)

// Passwords are made at run time, never written in the source.
func password(t *testing.T) string {
	t.Helper()
	s, err := NewSecret("")
	if err != nil {
		t.Fatal(err)
	}
	return s[:20]
}

func TestPasswordHashRoundTrip(t *testing.T) {
	pw := password(t)
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("PHC string %q", h)
	}
	if !VerifyPassword(h, pw) {
		t.Fatal("the right password was refused")
	}
	if VerifyPassword(h, pw+"x") {
		t.Fatal("a wrong password was accepted")
	}
	h2, _ := HashPassword(pw)
	if h2 == h {
		t.Fatal("two hashes of one password must differ (random salt)")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=0,t=3,p=2$AAAA$AAAA", "$bcrypt$x", "$argon2id$v=18$m=65536,t=3,p=2$AAAA$AAAA"} {
		if VerifyPassword(bad, pw) {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
}

func TestShortPasswordsAreRefused(t *testing.T) {
	if _, err := HashPassword(password(t)[:MinPasswordLength-1]); err == nil {
		t.Fatal("a short password was accepted")
	}
}

func TestMissingUserTakesAsLongAsAWrongPassword(t *testing.T) {
	if VerifyOrWaste("", password(t)) {
		t.Fatal("no hash must never verify")
	}
	start := time.Now()
	VerifyOrWaste("", password(t))
	if time.Since(start) < time.Millisecond {
		t.Fatal("a missing user must still spend the time of a hash")
	}
}

func TestSecrets(t *testing.T) {
	a, _ := NewSecret("brofo_")
	b, _ := NewSecret("brofo_")
	if a == b || !strings.HasPrefix(a, "brofo_") || len(a) != len("brofo_")+64 {
		t.Fatalf("%q %q", a, b)
	}
	if HashSecret(a) == HashSecret(b) || len(HashSecret(a)) != 64 || strings.Contains(HashSecret(a), a) {
		t.Fatal("hash")
	}
}

func TestLimiterLocksOutAndRecovers(t *testing.T) {
	l := NewLimiter(3, time.Minute, 15*time.Minute)
	now := time.Unix(0, 0)
	for i := 0; i < 2; i++ {
		l.Fail("ada", now)
	}
	if ok, _ := l.Allowed("ada", now); !ok {
		t.Fatal("locked out too early")
	}
	l.Fail("ada", now)
	if ok, until := l.Allowed("ada", now); ok || !until.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("not locked out: %v %v", ok, until)
	}
	if ok, _ := l.Allowed("bob", now); !ok {
		t.Fatal("one key locked another")
	}
	if ok, _ := l.Allowed("ada", now.Add(16*time.Minute)); !ok {
		t.Fatal("still locked after the lockout")
	}
	l.Fail("cy", now)
	l.Fail("cy", now.Add(2*time.Minute)) // the first fell out of the window
	l.Fail("cy", now.Add(2*time.Minute))
	if ok, _ := l.Allowed("cy", now.Add(2*time.Minute)); !ok {
		t.Fatal("failures outside the window counted")
	}
}
