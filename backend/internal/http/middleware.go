package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
)

// cors allows browser calls from the given origins only (exact match).
// Requests without an Origin header (server-side rendering, curl) pass through.
func cors(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

			if origin == "" || !allowed[origin] {
				if preflight {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "Retry-After")
			if preflight {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateLimiter is a fixed-window request counter per key (client IP).
// The process is a single instance, so in-memory state is enough.
type rateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	windows   map[string]*rateWindow
	lastSweep time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: time.Now, windows: map[string]*rateWindow{}}
}

// allow counts a request for key. When the limit is exhausted it returns false
// and how long until the window resets.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= l.window {
		for k, w := range l.windows {
			if now.Sub(w.start) >= l.window {
				delete(l.windows, k)
			}
		}
		l.lastSweep = now
	}

	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.windows[key] = &rateWindow{start: now, count: 1}
		return true, 0
	}
	if w.count >= l.limit {
		return false, w.start.Add(l.window).Sub(now)
	}
	w.count++
	return true, 0
}

// rateLimit applies limiters to operations by name, keyed by client IP.
func rateLimit(limiters map[string]*rateLimiter) oapi.StrictMiddlewareFunc {
	return func(next oapi.StrictHandlerFunc, operationID string) oapi.StrictHandlerFunc {
		l := limiters[operationID]
		if l == nil || l.limit <= 0 {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			key := middleware.GetClientIP(ctx)
			if key == "" {
				key = "unknown"
			}
			if ok, retryAfter := l.allow(key); !ok {
				return nil, errRateLimited{retryAfter: retryAfter}
			}
			return next(ctx, w, r, request)
		}
	}
}

type userAgentKey struct{}

// withUserAgent keeps the User-Agent in the context: strict handlers do not
// see the request, and the purchase event for ad platforms needs it.
func withUserAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userAgentKey{}, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userAgent(ctx context.Context) string {
	ua, _ := ctx.Value(userAgentKey{}).(string)
	return ua
}
