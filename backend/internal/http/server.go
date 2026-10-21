// Package http wires the HTTP transport: router, middleware and generated handlers.
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// maxBodyBytes caps JSON request bodies.
const maxBodyBytes = 64 << 10

// Deps are the router's dependencies.
type Deps struct {
	Log      *slog.Logger
	Catalog  *service.Catalog
	Checkout *service.Checkout
	Images   *imgproxy.Signer
	// Browser origins allowed by CORS.
	CORSOrigins []string
	// Reverse proxy CIDRs whose X-Forwarded-For is trusted; empty = use the TCP peer.
	TrustedProxyCIDRs []string
	// Per-IP requests per minute; 0 disables.
	CheckoutRateLimit int
	LookupRateLimit   int
}

// NewRouter builds the root HTTP handler.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(d.TrustedProxyCIDRs) > 0 {
		r.Use(middleware.ClientIPFromXFF(d.TrustedProxyCIDRs...))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}
	r.Use(requestLogger(d.Log))
	r.Use(middleware.Recoverer)
	r.Use(cors(d.CORSOrigins))
	r.Use(middleware.RequestSize(maxBodyBytes))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, oapi.Error{Code: oapi.ErrorCodeNotFound, Message: "route not found"})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, oapi.Error{Code: oapi.ErrorCodeMethodNotAllowed, Message: "method not allowed"})
	})

	// Keyed by the Go operation name that the generated strict handler passes to middleware.
	checkoutLimiter := newRateLimiter(d.CheckoutRateLimit, time.Minute)
	limiters := map[string]*rateLimiter{
		"CreateOrder":   checkoutLimiter,
		"ValidatePromo": checkoutLimiter,
		"GetOrder":      newRateLimiter(d.LookupRateLimit, time.Minute),
	}

	strict := oapi.NewStrictHandlerWithOptions(
		NewHandlers(d.Catalog, d.Checkout, d.Images),
		[]oapi.StrictMiddlewareFunc{rateLimit(limiters)},
		oapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  requestErrorHandler,
			ResponseErrorHandlerFunc: responseErrorHandler(d.Log),
		},
	)
	return oapi.HandlerWithOptions(strict, oapi.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: requestErrorHandler,
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
					slog.String("client_ip", middleware.GetClientIP(r.Context())),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}
