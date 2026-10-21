package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/seed"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
	"github.com/Baiel1922/zhim-back/backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// fakeTelegram records Bot API calls and replies like Telegram.
type fakeTelegram struct {
	mu       sync.Mutex
	calls    []apiCall
	nextID   int
	failures map[string][]string // method → queued error responses
}

type apiCall struct {
	method string
	fields map[string]string
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := path.Base(r.URL.Path)
	fields := map[string]string{}
	if err := r.ParseMultipartForm(1 << 20); err == nil {
		for k, v := range r.MultipartForm.Value {
			fields[k] = v[0]
		}
	}
	if method == "getUpdates" {
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, apiCall{method: method, fields: fields})
	if q := f.failures[method]; len(q) > 0 {
		f.failures[method] = q[1:]
		_, _ = io.WriteString(w, q[0])
		return
	}
	switch method {
	case "sendMessage", "editMessageText", "editMessageReplyMarkup":
		id, _ := strconv.Atoi(fields["message_id"])
		if id == 0 {
			f.nextID++
			id = f.nextID
		}
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"date":0,"chat":{"id":%s,"type":"private"}}}`, id, fields["chat_id"])
	default:
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func (f *fakeTelegram) callsOf(method string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// waitFor waits until at least n calls of method were made.
func (f *fakeTelegram) waitFor(t *testing.T, method string, n int) []apiCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls := f.callsOf(method); len(calls) >= n {
			return calls
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d %s calls, got %d", n, method, len(f.callsOf(method)))
	return nil
}

type harness struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *repo.Store
	tg       *fakeTelegram
	bot      *Bot
	checkout *service.Checkout
	owner    db.Admin
	manager  db.Admin
}

// newHarness seeds a database with an owner (1001), a manager (1002) and an
// inactive admin (1003), and runs the bot's event loop against a fake Telegram.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	pool := pgtest.NewPool(t)
	store := repo.NewStore(pool)
	if _, err := seed.Run(ctx, store, seed.Options{OwnerTelegramID: 1001, OwnerName: "Владелец"}); err != nil {
		t.Fatal(err)
	}
	manager, err := store.UpsertAdmin(ctx, db.UpsertAdminParams{TelegramID: 1002, Name: "Менеджер", Role: db.AdminRoleManager, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAdmin(ctx, db.UpsertAdminParams{TelegramID: 1003, Name: "Бывший", Role: db.AdminRoleManager, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	owner, err := store.GetActiveAdminByTelegramID(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}

	tg := &fakeTelegram{failures: map[string][]string{}}
	srv := httptest.NewServer(tg)
	t.Cleanup(srv.Close)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	admin := service.NewAdmin(store, nil, nil, bishkek, log)
	b, err := New(Config{
		Token: "123:test", AdminURL: "https://admin.zhim.test", WebURL: "https://zhim.test",
		ServerURL: srv.URL, Location: bishkek, ReportHour: 21,
	}, store, admin, log)
	if err != nil {
		t.Fatal(err)
	}
	b.retryDelay = time.Millisecond
	checkout := service.NewCheckout(store, nil)
	checkout.SetOrderEvents(b)
	admin.SetOrderEvents(b)
	go b.eventLoop(ctx)

	return &harness{t: t, ctx: ctx, pool: pool, store: store, tg: tg, bot: b, checkout: checkout, owner: owner, manager: manager}
}

func (h *harness) placeOrder(key string) db.Order {
	h.t.Helper()
	var variantID int64
	rows, err := h.store.ListVariantsByProduct(h.ctx, mustProduct(h.t, h.store, "termofutbolka-core"))
	if err != nil {
		h.t.Fatal(err)
	}
	for _, r := range rows {
		if r.ProductVariant.Sku == "ZH-TFC-BLK-M" {
			variantID = r.ProductVariant.ID
		}
	}
	d, err := h.checkout.CreateOrder(h.ctx, service.OrderInput{
		IdempotencyKey: key, Lang: db.AppLangRu, CustomerName: "Азамат", Phone: "0700123456",
		DeliveryMethod: db.DeliveryMethodPickup, PaymentMethod: db.PaymentMethodCash,
		Items: []service.ItemInput{{VariantID: variantID, Qty: 2}},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return d.Order
}

func mustProduct(t *testing.T, store *repo.Store, slug string) int64 {
	t.Helper()
	p, err := store.GetProductBySlug(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// press simulates an admin tapping an inline button on their copy of the message.
func (h *harness) press(telegramID int64, messageID int, data string) {
	h.bot.handleCallback(h.ctx, nil, &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   fmt.Sprintf("q-%d-%s", telegramID, data),
		From: models.User{ID: telegramID},
		Data: data,
		Message: models.MaybeInaccessibleMessage{
			Type:    models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{ID: messageID, Chat: models.Chat{ID: telegramID}},
		},
	}})
}

// refs waits until the bot has stored n message references for the order
// (it saves them after Telegram answers sendMessage).
func (h *harness) refs(orderID int64, n int) []db.OrderNotification {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		refs, err := h.store.ListOrderNotifications(h.ctx, orderID)
		if err != nil {
			h.t.Fatal(err)
		}
		if len(refs) >= n || time.Now().After(deadline) {
			if len(refs) != n {
				h.t.Fatalf("message refs = %d, want %d", len(refs), n)
			}
			return refs
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *harness) lastAnswer() apiCall {
	h.t.Helper()
	answers := h.tg.waitFor(h.t, "answerCallbackQuery", 1)
	return answers[len(answers)-1]
}

func (h *harness) status(orderID int64) db.OrderStatus {
	h.t.Helper()
	o, err := h.store.GetOrder(h.ctx, orderID)
	if err != nil {
		h.t.Fatal(err)
	}
	return o.Status
}

func TestNewOrderIsSentToActiveAdmins(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	order := h.placeOrder("bot-key-0001")

	sent := h.tg.waitFor(t, "sendMessage", 2)
	chats := map[string]bool{}
	for _, c := range sent {
		chats[c.fields["chat_id"]] = true
		if c.fields["parse_mode"] != "HTML" || !strings.Contains(c.fields["text"], "<b>Заказ "+order.Number+"</b> · Новый") ||
			!strings.Contains(c.fields["text"], "+996700123456") {
			t.Errorf("message = %+v", c.fields)
		}
		var kb models.InlineKeyboardMarkup
		if err := json.Unmarshal([]byte(c.fields["reply_markup"]), &kb); err != nil {
			t.Fatal(err)
		}
		if kb.InlineKeyboard[0][0].CallbackData != fmt.Sprintf("st:%d:confirmed", order.ID) ||
			kb.InlineKeyboard[1][0].WebApp.URL != fmt.Sprintf("https://admin.zhim.test/orders/%d", order.ID) {
			t.Errorf("keyboard = %+v", kb.InlineKeyboard)
		}
	}
	if len(chats) != 2 || !chats["1001"] || !chats["1002"] {
		t.Fatalf("sent to %v, want the two active admins", chats)
	}
	h.refs(order.ID, 2)
}

func TestConfirmButtonUpdatesEveryCopy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	order := h.placeOrder("bot-key-0002")
	refs := h.refs(order.ID, 2)

	h.press(1002, int(refs[1].MessageID), fmt.Sprintf("st:%d:confirmed", order.ID))
	if a := h.lastAnswer(); a.fields["text"] != "Готово: Подтверждён" {
		t.Fatalf("answer = %+v", a.fields)
	}
	if h.status(order.ID) != db.OrderStatusConfirmed {
		t.Fatal("status did not change")
	}
	edits := h.tg.waitFor(t, "editMessageText", 2)
	for _, e := range edits {
		if !strings.Contains(e.fields["text"], "· Подтверждён") || !strings.Contains(e.fields["text"], "Изменено: Менеджер") {
			t.Errorf("edited text = %s", e.fields["text"])
		}
		if strings.Contains(e.fields["reply_markup"], "confirmed") {
			t.Errorf("confirm button still shown: %s", e.fields["reply_markup"])
		}
	}

	// The owner taps the stale button on their copy.
	h.press(1001, int(refs[0].MessageID), fmt.Sprintf("st:%d:confirmed", order.ID))
	if a := h.lastAnswer(); a.fields["show_alert"] != "true" || !strings.Contains(a.fields["text"], "уже изменён") {
		t.Fatalf("stale press answer = %+v", a.fields)
	}
}

func TestCancelAsksForConfirmationAndRestocks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	order := h.placeOrder("bot-key-0003")
	refs := h.refs(order.ID, 2)
	msg := int(refs[0].MessageID)

	h.press(1001, msg, fmt.Sprintf("cx:%d", order.ID))
	kbEdit := h.tg.waitFor(t, "editMessageReplyMarkup", 1)[0]
	if !strings.Contains(kbEdit.fields["reply_markup"], "Да, отменить заказ") || h.status(order.ID) != db.OrderStatusNew {
		t.Fatalf("cancel must ask first: %+v", kbEdit.fields)
	}
	h.press(1001, msg, fmt.Sprintf("bk:%d", order.ID))
	back := h.tg.waitFor(t, "editMessageReplyMarkup", 2)[1]
	if !strings.Contains(back.fields["reply_markup"], "confirmed") {
		t.Fatalf("back did not restore the keyboard: %s", back.fields["reply_markup"])
	}

	h.press(1001, msg, fmt.Sprintf("st:%d:cancelled", order.ID))
	if h.status(order.ID) != db.OrderStatusCancelled {
		t.Fatal("order not cancelled")
	}
	var stock int32
	if err := h.pool.QueryRow(h.ctx, "SELECT stock FROM product_variants WHERE sku = 'ZH-TFC-BLK-M'").Scan(&stock); err != nil || stock != 9 {
		t.Fatalf("stock = %d, %v (cancel must restock)", stock, err)
	}
}

func TestNonAdminCannotPressButtons(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	order := h.placeOrder("bot-key-0004")
	h.tg.waitFor(t, "sendMessage", 2)

	for _, id := range []int64{9999, 1003} { // stranger, deactivated admin
		h.press(id, 1, fmt.Sprintf("st:%d:confirmed", order.ID))
		if a := h.lastAnswer(); a.fields["text"] != "Нет доступа" {
			t.Fatalf("answer for %d = %+v", id, a.fields)
		}
	}
	if h.status(order.ID) != db.OrderStatusNew {
		t.Fatal("a non-admin changed the status")
	}
}

func TestRetriesRateLimitsButNotForbidden(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.tg.mu.Lock()
	rateLimited := `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 0","parameters":{"retry_after":0}}`
	forbidden := `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	h.tg.failures["sendMessage"] = []string{rateLimited, rateLimited, forbidden}
	h.tg.mu.Unlock()

	order := h.placeOrder("bot-key-0005")
	// First admin: 429, 429, then delivered. Second admin: 403, given up at once.
	h.tg.waitFor(t, "sendMessage", 4)
	h.refs(order.ID, 1)
	time.Sleep(100 * time.Millisecond)
	if n := len(h.tg.callsOf("sendMessage")); n != 4 {
		t.Fatalf("sendMessage calls = %d, want 4", n)
	}
}

func TestStartCommand(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	start := func(userID int64) apiCall {
		before := len(h.tg.callsOf("sendMessage"))
		h.bot.handleStart(h.ctx, nil, &models.Update{Message: &models.Message{
			Text: "/start", From: &models.User{ID: userID}, Chat: models.Chat{ID: userID},
		}})
		return h.tg.waitFor(t, "sendMessage", before+1)[before]
	}

	admin := start(1002)
	if !strings.Contains(admin.fields["text"], "Здравствуйте, Менеджер") || !strings.Contains(admin.fields["reply_markup"], `"web_app":{"url":"https://admin.zhim.test"}`) {
		t.Fatalf("admin /start = %+v", admin.fields)
	}
	stranger := start(4242)
	if !strings.Contains(stranger.fields["text"], "<code>4242</code>") || !strings.Contains(stranger.fields["reply_markup"], "https://zhim.test") {
		t.Fatalf("stranger /start = %+v", stranger.fields)
	}
}

func TestDailyReportGoesToOwners(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.placeOrder("bot-key-0006")
	h.tg.waitFor(t, "sendMessage", 2)

	if err := h.bot.SendDailyReport(h.ctx); err != nil {
		t.Fatal(err)
	}
	sent := h.tg.waitFor(t, "sendMessage", 3)
	report := sent[2]
	if report.fields["chat_id"] != "1001" || !strings.Contains(report.fields["text"], "Заказов: 1") ||
		!strings.Contains(report.fields["text"], "Ждут подтверждения: 1") {
		t.Fatalf("report = %+v", report.fields)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(h.tg.callsOf("sendMessage")); n != 3 {
		t.Fatalf("report must go to owners only; sendMessage calls = %d", n)
	}
}
