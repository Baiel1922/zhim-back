package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const secret = "0123456789abcdef0123456789abcdef"

func newTestTokens(t *testing.T, now *time.Time) *Tokens {
	t.Helper()
	tok, err := NewTokens(secret, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tok.now = func() time.Time { return *now }
	return tok
}

func TestIssueAndVerify(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	tok := newTestTokens(t, &now)

	token, exp, err := tok.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(12 * time.Hour)) {
		t.Fatalf("exp = %v", exp)
	}
	id, err := tok.Verify(token)
	if err != nil || id != 42 {
		t.Fatalf("verify = %d, %v", id, err)
	}

	now = now.Add(12*time.Hour + time.Second)
	if _, err := tok.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	tok := newTestTokens(t, &now)
	good, _, err := tok.Issue(7)
	if err != nil {
		t.Fatal(err)
	}

	other, err := NewTokens(strings.Repeat("x", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other.now = tok.now
	foreign, _, _ := other.Issue(7)

	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer: issuer, Audience: jwt.ClaimStrings{audience}, Subject: "7",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)

	wrongAudience, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: issuer, Audience: jwt.ClaimStrings{"someone-else"}, Subject: "7",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString([]byte(secret))

	noExpiry, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: issuer, Audience: jwt.ClaimStrings{audience}, Subject: "7",
	}).SignedString([]byte(secret))

	parts := strings.Split(good, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]

	for name, token := range map[string]string{
		"other secret":   foreign,
		"alg none":       none,
		"wrong audience": wrongAudience,
		"no expiry":      noExpiry,
		"tampered":       tampered,
		"garbage":        "not-a-jwt",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tok.Verify(token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

func TestNewTokensValidates(t *testing.T) {
	if _, err := NewTokens("short", time.Hour); err == nil {
		t.Error("short secret accepted")
	}
	if _, err := NewTokens(secret, 0); err == nil {
		t.Error("zero ttl accepted")
	}
}
