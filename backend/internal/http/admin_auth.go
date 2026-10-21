package http

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// Admin operations open to managers (orders and stock). Every other admin
// operation requires the owner role, so a new endpoint is owner-only until it
// is added here.
var managerOperations = map[string]bool{
	"AdminGetMe":           true,
	"AdminListProducts":    true,
	"AdminGetProduct":      true,
	"AdminSetVariantStock": true,
	"AdminListOrders":      true,
	"AdminGetOrder":        true,
	"AdminSetOrderStatus":  true,
	"AdminSetOrderPayment": true,
	"AdminSetOrderComment": true,
}

// publicAdminOperations need no token.
var publicAdminOperations = map[string]bool{
	"AdminAuthTelegram": true,
}

type adminKey struct{}

// currentAdmin returns the authenticated admin of an admin request.
func currentAdmin(ctx context.Context) db.Admin {
	a, _ := ctx.Value(adminKey{}).(db.Admin)
	return a
}

// requireAdmin authenticates Admin* operations by bearer token and enforces roles.
func requireAdmin(authn *service.AdminAuth) oapi.StrictMiddlewareFunc {
	return func(next oapi.StrictHandlerFunc, operation string) oapi.StrictHandlerFunc {
		if !strings.HasPrefix(operation, "Admin") || publicAdminOperations[operation] {
			return next
		}
		managerAllowed := managerOperations[operation]
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				return nil, &service.Error{Code: service.CodeUnauthorized, Message: "missing bearer token"}
			}
			admin, err := authn.Authenticate(ctx, token)
			if err != nil {
				return nil, err
			}
			if admin.Role != db.AdminRoleOwner && !managerAllowed {
				return nil, &service.Error{Code: service.CodeForbidden, Message: "owner role required"}
			}
			return next(context.WithValue(ctx, adminKey{}, admin), w, r, request)
		}
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// bodyLimit caps request bodies: photo uploads get room for one image, JSON gets 64 KB.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxBodyBytes)
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && mt == "multipart/form-data" {
			limit = service.MaxImageBytes + 1<<20 // file plus form overhead
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func isTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}
