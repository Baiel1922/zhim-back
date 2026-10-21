package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	clock := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(2, time.Minute)
	l.now = func() time.Time { return clock }

	for i := range 2 {
		if ok, _ := l.allow("1.1.1.1"); !ok {
			t.Fatalf("request %d rejected", i+1)
		}
	}
	ok, retry := l.allow("1.1.1.1")
	if ok || retry != time.Minute {
		t.Fatalf("third request: ok=%v retry=%v", ok, retry)
	}
	if ok, _ := l.allow("2.2.2.2"); !ok {
		t.Fatal("other IPs have their own window")
	}

	clock = clock.Add(40 * time.Second)
	if _, retry := l.allow("1.1.1.1"); retry != 20*time.Second {
		t.Fatalf("retry after = %v, want 20s", retry)
	}

	clock = clock.Add(21 * time.Second)
	if ok, _ := l.allow("1.1.1.1"); !ok {
		t.Fatal("window did not reset")
	}
	if len(l.windows) != 1 {
		t.Fatalf("expired windows were not swept: %d left", len(l.windows))
	}
}

func TestCORS(t *testing.T) {
	handler := cors([]string{"https://zhim.kg"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	do := func(method, origin string, preflight bool) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "/v1/orders", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if preflight {
			req.Header.Set("Access-Control-Request-Method", "POST")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("allowed origin", func(t *testing.T) {
		rec := do(http.MethodPost, "https://zhim.kg", false)
		if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "https://zhim.kg" {
			t.Fatalf("status=%d acao=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("vary = %q", rec.Header().Get("Vary"))
		}
	})
	t.Run("allowed preflight", func(t *testing.T) {
		rec := do(http.MethodOptions, "https://zhim.kg", true)
		h := rec.Header()
		if rec.Code != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != "https://zhim.kg" ||
			h.Get("Access-Control-Allow-Headers") != "Content-Type, Authorization, Idempotency-Key" {
			t.Fatalf("status=%d headers=%v", rec.Code, h)
		}
	})
	t.Run("foreign origin gets no CORS headers", func(t *testing.T) {
		rec := do(http.MethodPost, "https://evil.example", false)
		if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("status=%d acao=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})
	t.Run("foreign preflight is refused", func(t *testing.T) {
		if rec := do(http.MethodOptions, "https://evil.example", true); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d", rec.Code)
		}
	})
	t.Run("no origin passes through", func(t *testing.T) {
		if rec := do(http.MethodGet, "", false); rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}
