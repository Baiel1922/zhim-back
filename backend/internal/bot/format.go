package bot

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// Bot texts are Russian: the admin side of the store is Russian-only.

var statusLabels = map[db.OrderStatus]string{
	db.OrderStatusNew:            "Новый",
	db.OrderStatusConfirmed:      "Подтверждён",
	db.OrderStatusAssembling:     "Собирается",
	db.OrderStatusShipped:        "Отправлен",
	db.OrderStatusReadyForPickup: "Готов к выдаче",
	db.OrderStatusCompleted:      "Выполнен",
	db.OrderStatusCancelled:      "Отменён",
}

var paymentLabels = map[db.PaymentMethod]string{
	db.PaymentMethodCash:     "наличными",
	db.PaymentMethodTransfer: "переводом",
}

// money formats whole som as "1 990 сом" (non-breaking spaces).
func money(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "−", -v
	}
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return sign + b.String() + " сом"
}

func esc(s string) string { return html.EscapeString(s) }

// orderCard renders the order message (HTML parse mode).
func orderCard(o *service.AdminOrder, loc *time.Location) string {
	ord := o.Order
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Заказ %s</b> · %s\n", esc(ord.Number), statusLabels[ord.Status])
	fmt.Fprintf(&b, "%s\n\n", ord.CreatedAt.In(loc).Format("02.01.2006 15:04"))

	for i, it := range o.Items {
		fmt.Fprintf(&b, "%d. %s — %s, %s × %d — %s\n",
			i+1, esc(it.TitleRu), esc(it.ColorNameRu), esc(it.Size), it.Qty, money(int64(it.Price)*int64(it.Qty)))
	}

	fmt.Fprintf(&b, "\nТовары: %s\n", money(int64(ord.Subtotal)))
	if ord.Discount > 0 {
		promo := ""
		if o.PromoCode != "" {
			promo = " (" + esc(o.PromoCode) + ")"
		}
		fmt.Fprintf(&b, "Скидка%s: %s\n", promo, money(-int64(ord.Discount)))
	}
	if ord.DeliveryMethod == db.DeliveryMethodCourier {
		delivery := "бесплатно"
		if ord.DeliveryPrice > 0 {
			delivery = money(int64(ord.DeliveryPrice))
		}
		fmt.Fprintf(&b, "Доставка: %s\n", delivery)
	}
	fmt.Fprintf(&b, "<b>Итого: %s</b>\n", money(int64(ord.Total)))
	paid := "не оплачен"
	if ord.PaymentStatus == db.PaymentStatusPaid {
		paid = "оплачен ✅"
	}
	fmt.Fprintf(&b, "Оплата: %s при получении · %s\n\n", paymentLabels[ord.PaymentMethod], paid)

	if ord.DeliveryMethod == db.DeliveryMethodCourier {
		zone := ""
		if o.DeliveryZone != nil {
			zone = " · " + esc(o.DeliveryZone.NameRu)
		}
		fmt.Fprintf(&b, "Курьер%s\n", zone)
		if ord.Address != nil {
			fmt.Fprintf(&b, "%s\n", esc(*ord.Address))
		}
	} else {
		b.WriteString("Самовывоз\n")
	}
	// Telegram turns the phone number into a tappable link by itself.
	fmt.Fprintf(&b, "\n%s\n%s\n", esc(ord.CustomerName), esc(ord.Phone))
	if ord.Comment != "" {
		fmt.Fprintf(&b, "Комментарий: %s\n", esc(ord.Comment))
	}

	if n := len(o.History); n > 0 {
		last := o.History[n-1]
		if last.AdminName != nil && last.FromStatus != nil {
			fmt.Fprintf(&b, "\nИзменено: %s · %s\n", esc(*last.AdminName), last.CreatedAt.In(loc).Format("02.01 15:04"))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Callback data: "st:<order>:<status>" sets a status, "cx:<order>" asks to
// confirm cancellation, "bk:<order>" restores the normal keyboard.
const (
	actionStatus = "st"
	actionCancel = "cx"
	actionBack   = "bk"
)

type callback struct {
	action  string
	orderID int64
	status  db.OrderStatus
}

func parseCallback(data string) (callback, bool) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return callback{}, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return callback{}, false
	}
	c := callback{action: parts[0], orderID: id}
	switch {
	case c.action == actionStatus && len(parts) == 3:
		c.status = db.OrderStatus(parts[2])
		return c, c.status.Valid()
	case (c.action == actionCancel || c.action == actionBack) && len(parts) == 2:
		return c, true
	}
	return callback{}, false
}

// orderKeyboard: new orders get confirm/cancel buttons; every order links to
// the Mini App when adminURL is set (Telegram requires HTTPS for web_app).
func orderKeyboard(o db.Order, adminURL string) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{}
	if o.Status == db.OrderStatusNew {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "✅ Подтвердить", CallbackData: fmt.Sprintf("%s:%d:%s", actionStatus, o.ID, db.OrderStatusConfirmed)},
			{Text: "Отменить", CallbackData: fmt.Sprintf("%s:%d", actionCancel, o.ID)},
		})
	}
	if adminURL != "" {
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Открыть в админке", WebApp: &models.WebAppInfo{URL: fmt.Sprintf("%s/orders/%d", adminURL, o.ID)}},
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func confirmCancelKeyboard(orderID int64) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: "Да, отменить заказ", CallbackData: fmt.Sprintf("%s:%d:%s", actionStatus, orderID, db.OrderStatusCancelled)}},
		{{Text: "Назад", CallbackData: fmt.Sprintf("%s:%d", actionBack, orderID)}},
	}}
}

const maxReportLowStock = 15

// dailyReport renders the evening summary.
func dailyReport(day time.Time, d *service.Dashboard, awaiting int32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Итоги дня · %s</b>\n", day.Format("02.01.2006"))
	fmt.Fprintf(&b, "Заказов: %d\n", d.Today.Orders)
	fmt.Fprintf(&b, "Выручка: %s\n", money(d.Today.Revenue))
	fmt.Fprintf(&b, "Ждут подтверждения: %d\n", awaiting)
	fmt.Fprintf(&b, "За 7 дней: %d · %s\n", d.Last7Days.Orders, money(d.Last7Days.Revenue))

	if len(d.LowStock) == 0 {
		b.WriteString("\nОстатков ≤ 2 нет.")
		return b.String()
	}
	b.WriteString("\n<b>Заканчиваются:</b>\n")
	for i, v := range d.LowStock {
		if i == maxReportLowStock {
			fmt.Fprintf(&b, "…и ещё %d\n", len(d.LowStock)-maxReportLowStock)
			break
		}
		fmt.Fprintf(&b, "• %s — %s, %s: %d\n", esc(v.TitleRu), esc(v.ColorNameRu), esc(v.Size), v.Stock)
	}
	return strings.TrimRight(b.String(), "\n")
}

// nextRun is the next hour:minute in loc strictly after now.
func nextRun(now time.Time, hour, minute int, loc *time.Location) time.Time {
	n := now.In(loc)
	t := time.Date(n.Year(), n.Month(), n.Day(), hour, minute, 0, 0, loc)
	if !t.After(n) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
