// Package revalidate tells the storefront to refresh cached pages after catalog
// or settings changes (Next.js on-demand revalidation by tag).
//
// Notify never blocks the caller: tags are queued, merged over a short debounce
// window and sent from a background goroutine with retries.
package revalidate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Cache tags understood by the storefront.
const (
	TagCategories = "categories"
	TagProducts   = "products"
	TagSettings   = "settings"
)

// ProductTag is the tag of one product page.
func ProductTag(slug string) string { return "product:" + slug }

// Notifier sends revalidation requests to {baseURL}/api/revalidate.
type Notifier struct {
	endpoint string
	secret   string
	client   *http.Client
	log      *slog.Logger
	queue    chan []string

	debounce time.Duration
	attempts int
	backoff  time.Duration
}

// New creates a Notifier. An empty baseURL disables it (Notify becomes a no-op).
func New(baseURL, secret string, log *slog.Logger) *Notifier {
	n := &Notifier{
		secret:   secret,
		client:   &http.Client{Timeout: 10 * time.Second},
		log:      log,
		queue:    make(chan []string, 256),
		debounce: 500 * time.Millisecond,
		attempts: 4,
		backoff:  time.Second,
	}
	if baseURL != "" {
		n.endpoint = strings.TrimRight(baseURL, "/") + "/api/revalidate"
	}
	return n
}

// Notify queues tags for revalidation.
func (n *Notifier) Notify(tags ...string) {
	if n == nil || n.endpoint == "" || len(tags) == 0 {
		return
	}
	select {
	case n.queue <- tags:
	default:
		n.log.Warn("revalidate queue full, dropping tags", slog.Any("tags", tags))
	}
}

// Run delivers queued tags until ctx is cancelled.
func (n *Notifier) Run(ctx context.Context) {
	if n.endpoint == "" {
		return
	}
	for {
		var first []string
		select {
		case <-ctx.Done():
			return
		case first = <-n.queue:
		}
		pending := map[string]bool{}
		for _, t := range first {
			pending[t] = true
		}
		timer := time.NewTimer(n.debounce)
	collect:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case more := <-n.queue:
				for _, t := range more {
					pending[t] = true
				}
			case <-timer.C:
				break collect
			}
		}
		tags := make([]string, 0, len(pending))
		for t := range pending {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		n.deliver(ctx, tags)
	}
}

func (n *Notifier) deliver(ctx context.Context, tags []string) {
	delay := n.backoff
	for attempt := 1; ; attempt++ {
		retry, err := n.send(ctx, tags)
		if err == nil {
			n.log.Debug("storefront revalidated", slog.Any("tags", tags))
			return
		}
		if !retry || attempt == n.attempts {
			n.log.Warn("storefront revalidation failed", slog.Any("tags", tags), slog.Int("attempts", attempt), slog.Any("error", err))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// send makes one request; retry reports whether a failure is worth retrying.
func (n *Notifier) send(ctx context.Context, tags []string) (retry bool, err error) {
	body, err := json.Marshal(map[string][]string{"tags": tags})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.secret)
	resp, err := n.client.Do(req)
	if err != nil {
		return true, err
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("storefront responded %d", resp.StatusCode)
	default:
		return false, fmt.Errorf("storefront responded %d", resp.StatusCode)
	}
}
