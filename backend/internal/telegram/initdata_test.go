package telegram_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/telegram"
)

const botToken = "123456:TEST-token-for-unit-tests"

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// sign builds initData the way Telegram does (independent of the code under test).
func sign(t *testing.T, fields map[string]string, token string) string {
	t.Helper()
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))

	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}

func fields(authDate time.Time) map[string]string {
	return map[string]string{
		"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
		"user":      `{"id":111222333,"first_name":"Тест","last_name":"","username":"tester","language_code":"ru"}`,
		"auth_date": strconv.FormatInt(authDate.Unix(), 10),
	}
}

func TestValidInitData(t *testing.T) {
	raw := sign(t, fields(now.Add(-time.Hour)), botToken)
	got, err := telegram.ValidateInitData(raw, botToken, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.User.ID != 111222333 || got.User.FirstName != "Тест" || !got.AuthDate.Equal(now.Add(-time.Hour)) {
		t.Fatalf("got %+v", got)
	}
}

func TestSignatureFieldIsPartOfTheHash(t *testing.T) {
	f := fields(now)
	f["signature"] = "ed25519-signature-base64url"
	if _, err := telegram.ValidateInitData(sign(t, f, botToken), botToken, 24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
}

func TestRejects(t *testing.T) {
	valid := sign(t, fields(now.Add(-time.Hour)), botToken)
	tampered := strings.Replace(valid, "111222333", "1", 1)
	forged := withHash(valid, strings.Repeat("ab", 32))

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"other bot token", sign(t, fields(now), "999:other"), telegram.ErrSignature},
		{"forged hash", forged, telegram.ErrSignature},
		{"tampered user", tampered, telegram.ErrSignature},
		{"expired auth_date", sign(t, fields(now.Add(-24*time.Hour-time.Second)), botToken), telegram.ErrExpired},
		{"auth_date in the future", sign(t, fields(now.Add(10*time.Minute)), botToken), telegram.ErrExpired},
		{"missing hash", "auth_date=1&user=%7B%7D", telegram.ErrMalformed},
		{"garbage", "%%%", telegram.ErrMalformed},
		{"no user", sign(t, map[string]string{"auth_date": strconv.FormatInt(now.Unix(), 10)}, botToken), telegram.ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := telegram.ValidateInitData(tt.raw, botToken, 24*time.Hour, now)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func withHash(raw, newHash string) string {
	v, _ := url.ParseQuery(raw)
	v.Set("hash", newHash)
	return v.Encode()
}
