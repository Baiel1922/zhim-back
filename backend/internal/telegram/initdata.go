// Package telegram validates Telegram Mini App launch data.
package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Validation errors.
var (
	ErrMalformed = errors.New("telegram: malformed init data")
	ErrSignature = errors.New("telegram: init data signature mismatch")
	ErrExpired   = errors.New("telegram: init data expired")
)

// maxClockSkew tolerates auth_date slightly in the future.
const maxClockSkew = time.Minute

// User is the Telegram user who opened the Mini App.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// InitData is validated launch data.
type InitData struct {
	User     User
	AuthDate time.Time
}

// ValidateInitData checks the raw initData string as documented in
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app:
// hash = hex(HMAC_SHA256(key = HMAC_SHA256(key = "WebAppData", msg = botToken), msg = data_check_string)),
// where data_check_string is every received field except hash, sorted by key,
// as key=value lines. auth_date must not be older than maxAge.
func ValidateInitData(raw, botToken string, maxAge time.Duration, now time.Time) (InitData, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return InitData{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	gotHash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(gotHash) != sha256.Size {
		return InitData{}, fmt.Errorf("%w: missing or invalid hash", ErrMalformed)
	}

	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	ok := hmac.Equal(gotHash, hmacSHA256(secret, []byte(dataCheckString(values, "hash"))))
	if !ok && values.Has("signature") {
		// The docs say "all received fields"; tolerate clients that sign without
		// the newer Ed25519 "signature" field. Both forms need the bot token.
		ok = hmac.Equal(gotHash, hmacSHA256(secret, []byte(dataCheckString(values, "hash", "signature"))))
	}
	if !ok {
		return InitData{}, ErrSignature
	}

	unix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return InitData{}, fmt.Errorf("%w: invalid auth_date", ErrMalformed)
	}
	authDate := time.Unix(unix, 0)
	if now.Sub(authDate) > maxAge || authDate.Sub(now) > maxClockSkew {
		return InitData{}, ErrExpired
	}

	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return InitData{}, fmt.Errorf("%w: missing user", ErrMalformed)
	}
	return InitData{User: user, AuthDate: authDate}, nil
}

func dataCheckString(values url.Values, exclude ...string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		skip := false
		for _, e := range exclude {
			skip = skip || k == e
		}
		if !skip {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}
	return strings.Join(lines, "\n")
}

func hmacSHA256(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}
