// Package conversions reports purchases to ad platforms from the server: Meta
// Conversions API and TikTok Events API. The browser pixels send the same
// events with the same event_id, and the platforms keep one of each pair.
package conversions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

const (
	metaGraphVersion = "v25.0"
	metaBaseURL      = "https://graph.facebook.com"
	tiktokURL        = "https://business-api.tiktok.com/open_api/v1.3/event/track/"
	currency         = "KGS"
	queueSize        = 1024
)

// Config enables a platform when both its pixel ID and access token are set.
type Config struct {
	MetaPixelID, MetaAccessToken, MetaTestEventCode       string
	TikTokPixelID, TikTokAccessToken, TikTokTestEventCode string
	// Storefront base URL: purchases are reported as happening on its checkout page.
	WebURL string
}

func (c Config) metaEnabled() bool   { return c.MetaPixelID != "" && c.MetaAccessToken != "" }
func (c Config) tiktokEnabled() bool { return c.TikTokPixelID != "" && c.TikTokAccessToken != "" }

// Sender queues purchases and delivers them in the background with retries.
type Sender struct {
	cfg        Config
	log        *slog.Logger
	client     *http.Client
	queue      chan service.Purchase
	metaURL    string
	tiktokURL  string
	retryDelay time.Duration
	attempts   int
}

var _ service.Conversions = (*Sender)(nil)

// New returns nil when no platform is configured.
func New(cfg Config, log *slog.Logger) *Sender {
	if !cfg.metaEnabled() && !cfg.tiktokEnabled() {
		return nil
	}
	return &Sender{
		cfg:        cfg,
		log:        log,
		client:     &http.Client{Timeout: 15 * time.Second},
		queue:      make(chan service.Purchase, queueSize),
		metaURL:    fmt.Sprintf("%s/%s/%s/events", metaBaseURL, metaGraphVersion, cfg.MetaPixelID),
		tiktokURL:  tiktokURL,
		retryDelay: 2 * time.Second,
		attempts:   4,
	}
}

// Platforms lists the enabled destinations, for the startup log.
func (s *Sender) Platforms() []string {
	var out []string
	if s.cfg.metaEnabled() {
		out = append(out, "meta")
	}
	if s.cfg.tiktokEnabled() {
		out = append(out, "tiktok")
	}
	return out
}

// Purchase queues a placed order; it never blocks the checkout.
func (s *Sender) Purchase(p service.Purchase) {
	select {
	case s.queue <- p:
	default:
		s.log.Error("conversions: queue is full, dropping purchase", slog.String("event_id", p.EventID))
	}
}

// Run delivers queued purchases until ctx ends.
func (s *Sender) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-s.queue:
			s.deliver(ctx, p)
		}
	}
}

func (s *Sender) deliver(ctx context.Context, p service.Purchase) {
	if s.cfg.metaEnabled() {
		s.report(ctx, "meta", p.EventID, func() error { return s.post(ctx, s.metaURL, nil, metaBody(p, s.cfg), checkMeta) })
	}
	if s.cfg.tiktokEnabled() {
		headers := map[string]string{"Access-Token": s.cfg.TikTokAccessToken}
		s.report(ctx, "tiktok", p.EventID, func() error { return s.post(ctx, s.tiktokURL, headers, tiktokBody(p, s.cfg), checkTikTok) })
	}
}

func (s *Sender) report(ctx context.Context, platform, eventID string, send func() error) {
	if err := s.retry(ctx, send); err != nil {
		if ctx.Err() == nil {
			s.log.Error("conversions: purchase not delivered", slog.String("platform", platform), slog.String("event_id", eventID), slog.Any("error", err))
		}
		return
	}
	s.log.Info("conversions: purchase delivered", slog.String("platform", platform), slog.String("event_id", eventID))
}

// permanentError is a rejection that a retry will not fix (bad token, bad payload).
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func (s *Sender) retry(ctx context.Context, fn func() error) error {
	delay := s.retryDelay
	for attempt := 1; ; attempt++ {
		err := fn()
		var permanent permanentError
		if err == nil || attempt == s.attempts || errors.As(err, &permanent) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (s *Sender) post(ctx context.Context, url string, headers map[string]string, body any, check func(status int, resp []byte) error) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return permanentError{fmt.Errorf("encode: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return permanentError{err}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return check(resp.StatusCode, data)
}

// --- Meta --------------------------------------------------------------------

type metaRequest struct {
	Data          []metaEvent `json:"data"`
	AccessToken   string      `json:"access_token"`
	TestEventCode string      `json:"test_event_code,omitempty"`
}

type metaEvent struct {
	EventName      string         `json:"event_name"`
	EventTime      int64          `json:"event_time"`
	EventID        string         `json:"event_id"`
	ActionSource   string         `json:"action_source"`
	EventSourceURL string         `json:"event_source_url"`
	UserData       metaUserData   `json:"user_data"`
	CustomData     metaCustomData `json:"custom_data"`
}

type metaUserData struct {
	Phone     []string `json:"ph,omitempty"`
	Country   []string `json:"country"`
	Fbp       string   `json:"fbp,omitempty"`
	Fbc       string   `json:"fbc,omitempty"`
	IP        string   `json:"client_ip_address,omitempty"`
	UserAgent string   `json:"client_user_agent,omitempty"`
}

type metaCustomData struct {
	Currency    string        `json:"currency"`
	Value       int32         `json:"value"`
	OrderID     string        `json:"order_id"`
	ContentType string        `json:"content_type"`
	ContentIDs  []string      `json:"content_ids"`
	Contents    []metaContent `json:"contents"`
	NumItems    int32         `json:"num_items"`
}

type metaContent struct {
	ID       string `json:"id"`
	Quantity int32  `json:"quantity"`
	Price    int32  `json:"item_price"`
}

func metaBody(p service.Purchase, cfg Config) metaRequest {
	data := metaCustomData{Currency: currency, Value: p.Value, OrderID: p.EventID, ContentType: "product"}
	for _, it := range p.Items {
		data.ContentIDs = append(data.ContentIDs, it.ID)
		data.Contents = append(data.Contents, metaContent{ID: it.ID, Quantity: it.Qty, Price: it.Price})
		data.NumItems += it.Qty
	}
	user := metaUserData{
		Country:   []string{hash("kg")},
		Fbp:       p.Fbp,
		Fbc:       p.Fbc,
		IP:        p.Client.IP,
		UserAgent: p.Client.UserAgent,
	}
	// Meta wants digits with the country code, no "+".
	if digits := strings.TrimPrefix(p.Phone, "+"); digits != "" {
		user.Phone = []string{hash(digits)}
	}
	return metaRequest{
		Data: []metaEvent{{
			EventName:      "Purchase",
			EventTime:      p.Time.Unix(),
			EventID:        p.EventID,
			ActionSource:   "website",
			EventSourceURL: checkoutURL(cfg.WebURL, p),
			UserData:       user,
			CustomData:     data,
		}},
		AccessToken:   cfg.MetaAccessToken,
		TestEventCode: cfg.MetaTestEventCode,
	}
}

func checkMeta(status int, body []byte) error {
	if status == http.StatusOK {
		var r struct {
			EventsReceived int `json:"events_received"`
		}
		if json.Unmarshal(body, &r) == nil && r.EventsReceived > 0 {
			return nil
		}
		return permanentError{fmt.Errorf("meta: unexpected response: %s", truncate(body))}
	}
	err := fmt.Errorf("meta: HTTP %d: %s", status, truncate(body))
	if status == http.StatusTooManyRequests || status >= 500 {
		return err
	}
	return permanentError{err}
}

// --- TikTok ------------------------------------------------------------------

type tiktokRequest struct {
	EventSource   string        `json:"event_source"`
	EventSourceID string        `json:"event_source_id"`
	TestEventCode string        `json:"test_event_code,omitempty"`
	Data          []tiktokEvent `json:"data"`
}

type tiktokEvent struct {
	Event      string           `json:"event"`
	EventTime  int64            `json:"event_time"`
	EventID    string           `json:"event_id"`
	User       tiktokUser       `json:"user"`
	Properties tiktokProperties `json:"properties"`
	Page       tiktokPage       `json:"page"`
}

type tiktokUser struct {
	Phone     string `json:"phone,omitempty"`
	Ttclid    string `json:"ttclid,omitempty"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

type tiktokProperties struct {
	Currency    string          `json:"currency"`
	Value       int32           `json:"value"`
	OrderID     string          `json:"order_id"`
	ContentType string          `json:"content_type"`
	Contents    []tiktokContent `json:"contents"`
}

type tiktokContent struct {
	ContentID string `json:"content_id"`
	Quantity  int32  `json:"quantity"`
	Price     int32  `json:"price"`
}

type tiktokPage struct {
	URL string `json:"url"`
}

func tiktokBody(p service.Purchase, cfg Config) tiktokRequest {
	props := tiktokProperties{Currency: currency, Value: p.Value, OrderID: p.EventID, ContentType: "product"}
	for _, it := range p.Items {
		props.Contents = append(props.Contents, tiktokContent{ContentID: it.ID, Quantity: it.Qty, Price: it.Price})
	}
	user := tiktokUser{Ttclid: p.Ttclid, IP: p.Client.IP, UserAgent: p.Client.UserAgent}
	// TikTok wants E.164 with the "+".
	if p.Phone != "" {
		user.Phone = hash(p.Phone)
	}
	return tiktokRequest{
		EventSource:   "web",
		EventSourceID: cfg.TikTokPixelID,
		TestEventCode: cfg.TikTokTestEventCode,
		Data: []tiktokEvent{{
			// TikTok's standard purchase event (formerly CompletePayment).
			Event:      "Purchase",
			EventTime:  p.Time.Unix(),
			EventID:    p.EventID,
			User:       user,
			Properties: props,
			Page:       tiktokPage{URL: checkoutURL(cfg.WebURL, p)},
		}},
	}
}

func checkTikTok(status int, body []byte) error {
	if status >= 500 || status == http.StatusTooManyRequests {
		return fmt.Errorf("tiktok: HTTP %d: %s", status, truncate(body))
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &r) != nil {
		return permanentError{fmt.Errorf("tiktok: HTTP %d: %s", status, truncate(body))}
	}
	if r.Code != 0 {
		return permanentError{fmt.Errorf("tiktok: code %d: %s", r.Code, r.Message)}
	}
	return nil
}

// --- Helpers -----------------------------------------------------------------

// hash is the SHA-256 hex digest the platforms expect for personal data.
func hash(s string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(s))))
	return hex.EncodeToString(sum[:])
}

func checkoutURL(webURL string, p service.Purchase) string {
	return strings.TrimRight(webURL, "/") + "/" + string(p.Lang) + "/checkout"
}

func truncate(b []byte) string {
	const limit = 300
	if len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}
