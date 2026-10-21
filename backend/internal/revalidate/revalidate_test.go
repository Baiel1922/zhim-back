package revalidate

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu       sync.Mutex
	requests [][]string
	statuses []int // response status per request; 200 once exhausted
	auth     string
	done     chan struct{}
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var body struct{ Tags []string }
	_ = json.NewDecoder(req.Body).Decode(&body)
	r.requests = append(r.requests, body.Tags)
	r.auth = req.Header.Get("Authorization")
	status := http.StatusOK
	if len(r.statuses) > 0 {
		status, r.statuses = r.statuses[0], r.statuses[1:]
	}
	w.WriteHeader(status)
	if status < 300 {
		close(r.done)
	}
}

func start(t *testing.T, rec *recorder) *Notifier {
	t.Helper()
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	n := New(srv.URL+"/", "s3cret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	n.debounce, n.backoff = 20*time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go n.Run(ctx)
	return n
}

func wait(t *testing.T, rec *recorder) {
	t.Helper()
	select {
	case <-rec.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no successful revalidation")
	}
}

func TestMergesTagsWithinDebounceWindow(t *testing.T) {
	rec := &recorder{done: make(chan struct{})}
	n := start(t, rec)
	n.Notify(TagProducts, ProductTag("tee"))
	n.Notify(TagProducts, TagSettings)
	wait(t, rec)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.requests) != 1 || strings.Join(rec.requests[0], ",") != "product:tee,products,settings" {
		t.Fatalf("requests = %v", rec.requests)
	}
	if rec.auth != "Bearer s3cret" {
		t.Fatalf("auth = %q", rec.auth)
	}
}

func TestRetriesServerErrors(t *testing.T) {
	rec := &recorder{done: make(chan struct{}), statuses: []int{502, 503}}
	n := start(t, rec)
	n.Notify(TagCategories)
	wait(t, rec)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.requests) != 3 {
		t.Fatalf("attempts = %d, want 3", len(rec.requests))
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	rec := &recorder{done: make(chan struct{}), statuses: []int{401}}
	n := start(t, rec)
	n.Notify(TagCategories)
	time.Sleep(200 * time.Millisecond)
	n.Notify(TagSettings) // next batch succeeds
	wait(t, rec)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.requests) != 2 || rec.requests[0][0] != TagCategories {
		t.Fatalf("requests = %v", rec.requests)
	}
}

func TestDisabledWithoutURL(_ *testing.T) {
	n := New("", "x", slog.New(slog.NewTextHandler(io.Discard, nil)))
	n.Notify(TagProducts) // must not block or panic
	var nilNotifier *Notifier
	nilNotifier.Notify(TagProducts)
}
