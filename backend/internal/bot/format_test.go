package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

var bishkek = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Bishkek")
	if err != nil {
		panic(err)
	}
	return loc
}()

func TestMoney(t *testing.T) {
	for v, want := range map[int64]string{
		0:       "0\u00a0сом",
		990:     "990\u00a0сом",
		1990:    "1\u00a0990\u00a0сом",
		1000000: "1\u00a0000\u00a0000\u00a0сом",
		-398:    "−398\u00a0сом",
	} {
		if got := money(v); got != want {
			t.Errorf("money(%d) = %q, want %q", v, got, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestOrderCard(t *testing.T) {
	created := time.Date(2026, 10, 21, 8, 5, 0, 0, time.UTC) // 14:05 in Bishkek
	newStatus := db.OrderStatusNew
	o := &service.AdminOrder{
		OrderDetails: &service.OrderDetails{
			Order: db.Order{
				ID: 7, Number: "ZH-000123", Status: db.OrderStatusConfirmed, PaymentMethod: db.PaymentMethodTransfer,
				PaymentStatus: db.PaymentStatusUnpaid, DeliveryMethod: db.DeliveryMethodCourier,
				CustomerName: "Азамат <b>", Phone: "+996700123456", Comment: "позвонить & ждать",
				Address: ptr("ул. Токтогула, 1"), Subtotal: 6470, Discount: 398, DeliveryPrice: 200, Total: 6272, CreatedAt: created,
			},
			Items: []db.OrderItem{
				{TitleRu: "Термофутболка Core", ColorNameRu: "Чёрный", Size: "M", Price: 1990, Qty: 2},
				{TitleRu: "Термофутболка Pro", ColorNameRu: "Графит", Size: "S", Price: 2490, Qty: 1},
			},
			DeliveryZone: &db.DeliveryZone{NameRu: "Бишкек"},
			PromoCode:    "WELCOME10",
		},
		History: []db.ListOrderHistoryWithAdminsRow{
			{ToStatus: db.OrderStatusNew, CreatedAt: created},
			{FromStatus: &newStatus, ToStatus: db.OrderStatusConfirmed, AdminName: ptr("Айгерим"), CreatedAt: created.Add(2 * time.Minute)},
		},
	}
	card := orderCard(o, bishkek)
	for _, want := range []string{
		"<b>Заказ ZH-000123</b> · Подтверждён",
		"21.10.2026 14:05",
		"1. Термофутболка Core — Чёрный, M × 2 — 3\u00a0980\u00a0сом",
		"2. Термофутболка Pro — Графит, S × 1 — 2\u00a0490\u00a0сом",
		"Скидка (WELCOME10): −398\u00a0сом",
		"Доставка: 200\u00a0сом",
		"<b>Итого: 6\u00a0272\u00a0сом</b>",
		"Оплата: переводом при получении · не оплачен",
		"Курьер · Бишкек\nул. Токтогула, 1",
		"Азамат &lt;b&gt;\n+996700123456",
		"Комментарий: позвонить &amp; ждать",
		"Изменено: Айгерим · 21.10 14:07",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card is missing %q:\n%s", want, card)
		}
	}

	o.Order.DeliveryMethod, o.Order.Discount, o.Order.DeliveryPrice, o.History = db.DeliveryMethodPickup, 0, 0, o.History[:1]
	o.Order.PaymentStatus = db.PaymentStatusPaid
	pickup := orderCard(o, bishkek)
	if !strings.Contains(pickup, "Самовывоз") || strings.Contains(pickup, "Доставка:") || strings.Contains(pickup, "Скидка") ||
		strings.Contains(pickup, "Изменено") || !strings.Contains(pickup, "оплачен ✅") {
		t.Errorf("pickup card:\n%s", pickup)
	}
}

func TestParseCallback(t *testing.T) {
	valid := map[string]callback{
		"st:12:confirmed": {action: actionStatus, orderID: 12, status: db.OrderStatusConfirmed},
		"st:12:cancelled": {action: actionStatus, orderID: 12, status: db.OrderStatusCancelled},
		"cx:12":           {action: actionCancel, orderID: 12},
		"bk:12":           {action: actionBack, orderID: 12},
	}
	for data, want := range valid {
		if got, ok := parseCallback(data); !ok || got != want {
			t.Errorf("parseCallback(%q) = %+v, %v", data, got, ok)
		}
	}
	for _, data := range []string{"", "st", "st:x:confirmed", "st:12:deleted", "st:12", "cx:12:extra", "zz:12", "st:-1:new"} {
		if got, ok := parseCallback(data); ok {
			t.Errorf("parseCallback(%q) accepted: %+v", data, got)
		}
	}
}

func TestOrderKeyboard(t *testing.T) {
	kb := orderKeyboard(db.Order{ID: 5, Status: db.OrderStatusNew}, "https://admin.zhim.kg")
	if len(kb.InlineKeyboard) != 2 || kb.InlineKeyboard[0][0].CallbackData != "st:5:confirmed" ||
		kb.InlineKeyboard[0][1].CallbackData != "cx:5" || kb.InlineKeyboard[1][0].WebApp.URL != "https://admin.zhim.kg/orders/5" {
		t.Fatalf("new order keyboard = %+v", kb.InlineKeyboard)
	}
	if kb := orderKeyboard(db.Order{ID: 5, Status: db.OrderStatusConfirmed}, "https://admin.zhim.kg"); len(kb.InlineKeyboard) != 1 || kb.InlineKeyboard[0][0].WebApp == nil {
		t.Fatalf("confirmed keyboard = %+v", kb.InlineKeyboard)
	}
	if kb := orderKeyboard(db.Order{ID: 5, Status: db.OrderStatusCancelled}, ""); len(kb.InlineKeyboard) != 0 {
		t.Fatalf("keyboard without Mini App = %+v", kb.InlineKeyboard)
	}
}

func TestNextRun(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, bishkek) }
	tests := []struct {
		now, want time.Time
	}{
		{at(10, 0), at(21, 0)},
		{at(20, 59), at(21, 0)},
		{at(21, 0), at(21, 0).AddDate(0, 0, 1)},
		{at(23, 30), at(21, 0).AddDate(0, 0, 1)},
		{time.Date(2026, 10, 21, 14, 59, 0, 0, time.UTC), at(21, 0)},                  // 14:59 UTC = 20:59 in Bishkek
		{time.Date(2026, 10, 21, 15, 30, 0, 0, time.UTC), at(21, 0).AddDate(0, 0, 1)}, // 15:30 UTC = 21:30 in Bishkek
	}
	for _, tt := range tests {
		if got := nextRun(tt.now, 21, 0, bishkek); !got.Equal(tt.want) {
			t.Errorf("nextRun(%v) = %v, want %v", tt.now, got, tt.want)
		}
	}
}

func TestDailyReport(t *testing.T) {
	d := &service.Dashboard{
		Today:     service.PeriodStats{Orders: 3, Revenue: 12470},
		Last7Days: service.PeriodStats{Orders: 18, Revenue: 89500},
		LowStock:  []db.ListLowStockVariantsRow{{TitleRu: "Шорты Run", ColorNameRu: "Белый", Size: "L", Stock: 1}},
	}
	text := dailyReport(time.Date(2026, 10, 21, 21, 0, 0, 0, bishkek), d, 2)
	for _, want := range []string{"Итоги дня · 21.10.2026", "Заказов: 3", "Выручка: 12\u00a0470\u00a0сом", "Ждут подтверждения: 2", "За 7 дней: 18 · 89\u00a0500\u00a0сом", "• Шорты Run — Белый, L: 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
	d.LowStock = make([]db.ListLowStockVariantsRow, 20)
	if text := dailyReport(time.Now(), d, 0); !strings.Contains(text, "…и ещё 5") {
		t.Errorf("long low-stock list is not truncated:\n%s", text)
	}
}
