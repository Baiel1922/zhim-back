package conversions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var purchase = service.Purchase{
	EventID: "ZH-000042",
	Time:    time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC),
	Lang:    "ky",
	Value:   3782,
	Phone:   "+996555123456",
	Fbp:     "fb.1.1.111",
	Fbc:     "fb.1.2.abc",
	Ttclid:  "tt-click",
	Client:  service.ClientInfo{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone)"},
	Items:   []service.PurchaseItem{{ID: "ZH-TFC-BLK-L", Qty: 2, Price: 1990}},
}

var cfg = Config{
	MetaPixelID: "111", MetaAccessToken: "meta-token", MetaTestEventCode: "TEST1",
	TikTokPixelID: "TT1", TikTokAccessToken: "tt-token",
	WebURL: "https://zhim.kg/",
}

func TestMetaBody(t *testing.T) {
	b := metaBody(purchase, cfg)
	if b.AccessToken != "meta-token" || b.TestEventCode != "TEST1" || len(b.Data) != 1 {
		t.Fatalf("request = %+v", b)
	}
	e := b.Data[0]
	if e.EventName != "Purchase" || e.EventID != "ZH-000042" || e.ActionSource != "website" ||
		e.EventTime != purchase.Time.Unix() || e.EventSourceURL != "https://zhim.kg/ky/checkout" {
		t.Errorf("event = %+v", e)
	}
	u := e.UserData
	// Digits with the country code, no "+".
	if len(u.Phone) != 1 || u.Phone[0] != sha("996555123456") || u.Country[0] != sha("kg") {
		t.Errorf("hashed user data = %+v", u)
	}
	if u.Fbp != "fb.1.1.111" || u.Fbc != "fb.1.2.abc" || u.IP != "203.0.113.7" || u.UserAgent != "Mozilla/5.0 (iPhone)" {
		t.Errorf("user data = %+v", u)
	}
	d := e.CustomData
	if d.Currency != "KGS" || d.Value != 3782 || d.NumItems != 2 || d.ContentIDs[0] != "ZH-TFC-BLK-L" || d.Contents[0].Price != 1990 {
		t.Errorf("custom data = %+v", d)
	}
}

func TestTikTokBody(t *testing.T) {
	b := tiktokBody(purchase, cfg)
	if b.EventSource != "web" || b.EventSourceID != "TT1" || b.TestEventCode != "" || len(b.Data) != 1 {
		t.Fatalf("request = %+v", b)
	}
	e := b.Data[0]
	if e.Event != "Purchase" || e.EventID != "ZH-000042" || e.Page.URL != "https://zhim.kg/ky/checkout" {
		t.Errorf("event = %+v", e)
	}
	// E.164 with the "+".
	if e.User.Phone != sha("+996555123456") || e.User.Ttclid != "tt-click" || e.User.IP != "203.0.113.7" {
		t.Errorf("user = %+v", e.User)
	}
	if e.Properties.Value != 3782 || e.Properties.Contents[0].ContentID != "ZH-TFC-BLK-L" || e.Properties.Contents[0].Quantity != 2 {
		t.Errorf("properties = %+v", e.Properties)
	}
}

func TestNewDisabled(t *testing.T) {
	if New(Config{MetaPixelID: "1"}, slog.New(slog.DiscardHandler)) != nil {
		t.Error("a pixel without a token must not enable the server-side sender")
	}
}

type recorder struct {
	mu       sync.Mutex
	bodies   [][]byte
	headers  []http.Header
	statuses []int // served in order; the last one repeats
	reply    string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body, _ := io.ReadAll(req.Body)
	r.bodies = append(r.bodies, body)
	r.headers = append(r.headers, req.Header.Clone())
	status := r.statuses[min(len(r.bodies), len(r.statuses))-1]
	w.WriteHeader(status)
	_, _ = io.WriteString(w, r.reply)
}

func (r *recorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func TestDeliverRetriesTransientErrors(t *testing.T) {
	meta := &recorder{statuses: []int{500, 200}, reply: `{"events_received":1}`}
	tiktok := &recorder{statuses: []int{200}, reply: `{"code":0,"message":"OK"}`}
	metaSrv, tiktokSrv := httptest.NewServer(meta), httptest.NewServer(tiktok)
	defer metaSrv.Close()
	defer tiktokSrv.Close()

	s := New(cfg, slog.New(slog.DiscardHandler))
	s.metaURL, s.tiktokURL, s.retryDelay = metaSrv.URL, tiktokSrv.URL, time.Millisecond
	s.deliver(context.Background(), purchase)

	if meta.calls() != 2 {
		t.Errorf("meta calls = %d, want a retry after 500", meta.calls())
	}
	if tiktok.calls() != 1 || tiktok.headers[0].Get("Access-Token") != "tt-token" {
		t.Errorf("tiktok calls = %d, token = %q", tiktok.calls(), tiktok.headers[0].Get("Access-Token"))
	}
	var sent metaRequest
	if err := json.Unmarshal(meta.bodies[1], &sent); err != nil || sent.Data[0].EventID != "ZH-000042" {
		t.Errorf("meta body = %s (%v)", meta.bodies[1], err)
	}
}

func TestDeliverStopsOnRejection(t *testing.T) {
	meta := &recorder{statuses: []int{400}, reply: `{"error":{"message":"Invalid parameter"}}`}
	tiktok := &recorder{statuses: []int{200}, reply: `{"code":40001,"message":"bad token"}`}
	metaSrv, tiktokSrv := httptest.NewServer(meta), httptest.NewServer(tiktok)
	defer metaSrv.Close()
	defer tiktokSrv.Close()

	s := New(cfg, slog.New(slog.DiscardHandler))
	s.metaURL, s.tiktokURL, s.retryDelay = metaSrv.URL, tiktokSrv.URL, time.Millisecond
	s.deliver(context.Background(), purchase)

	if meta.calls() != 1 || tiktok.calls() != 1 {
		t.Errorf("calls = meta %d, tiktok %d; rejections must not be retried", meta.calls(), tiktok.calls())
	}
}
