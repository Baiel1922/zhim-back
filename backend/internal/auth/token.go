// Package auth issues and verifies admin access tokens (HS256 JWT).
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	issuer   = "zhim-api"
	audience = "zhim-admin"
)

// ErrInvalidToken covers malformed, forged and expired tokens.
var ErrInvalidToken = errors.New("auth: invalid token")

// Tokens issues and verifies admin tokens.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokens creates a token service. The secret should be at least 32 bytes.
func NewTokens(secret string, ttl time.Duration) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, errors.New("auth: JWT secret must be at least 32 characters")
	}
	if ttl <= 0 {
		return nil, errors.New("auth: token TTL must be positive")
	}
	return &Tokens{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// Issue returns a signed token for the admin id and its expiry.
func (t *Tokens) Issue(adminID int64) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Audience:  jwt.ClaimStrings{audience},
		Subject:   strconv.FormatInt(adminID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Verify checks the token and returns the admin id it was issued for.
// Role and active state are read from the database on every request, so
// demoting or deactivating an admin takes effect immediately.
func (t *Tokens) Verify(token string) (int64, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: bad subject", ErrInvalidToken)
	}
	return id, nil
}
