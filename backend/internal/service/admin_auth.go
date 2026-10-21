package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/auth"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/telegram"
)

// initDataMaxAge bounds how old Telegram launch data may be.
const initDataMaxAge = 24 * time.Hour

// AdminAuth signs admins in with Telegram Mini App initData.
type AdminAuth struct {
	store    *repo.Store
	tokens   *auth.Tokens
	botToken string
	now      func() time.Time
}

// NewAdminAuth creates the admin sign-in service.
func NewAdminAuth(store *repo.Store, tokens *auth.Tokens, botToken string) *AdminAuth {
	return &AdminAuth{store: store, tokens: tokens, botToken: botToken, now: time.Now}
}

// Session is an issued admin token.
type Session struct {
	Token     string
	ExpiresAt time.Time
	Admin     db.Admin
}

// Login validates initData and issues a token for an active admin.
func (a *AdminAuth) Login(ctx context.Context, initData string) (*Session, error) {
	data, err := telegram.ValidateInitData(initData, a.botToken, initDataMaxAge, a.now())
	switch {
	case errors.Is(err, telegram.ErrExpired):
		return nil, newError(CodeInitDataExpired, "init data is older than 24 hours")
	case err != nil:
		return nil, newError(CodeInitDataInvalid, "init data is invalid")
	}

	admin, err := a.store.GetActiveAdminByTelegramID(ctx, data.User.ID)
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, newError(CodeNotAdmin, "telegram user %d is not an admin", data.User.ID)
		}
		return nil, fmt.Errorf("get admin: %w", err)
	}
	token, exp, err := a.tokens.Issue(admin.ID)
	if err != nil {
		return nil, err
	}
	return &Session{Token: token, ExpiresAt: exp, Admin: admin}, nil
}

// Authenticate resolves a bearer token to an active admin. The role comes from
// the database, so changes apply to tokens already issued.
func (a *AdminAuth) Authenticate(ctx context.Context, token string) (db.Admin, error) {
	id, err := a.tokens.Verify(token)
	if err != nil {
		return db.Admin{}, newError(CodeUnauthorized, "invalid or expired token")
	}
	admin, err := a.store.GetAdmin(ctx, id)
	if err != nil {
		if repo.IsNotFound(err) {
			return db.Admin{}, newError(CodeUnauthorized, "admin no longer exists")
		}
		return db.Admin{}, fmt.Errorf("get admin: %w", err)
	}
	if !admin.IsActive {
		return db.Admin{}, newError(CodeUnauthorized, "admin is deactivated")
	}
	return admin, nil
}
