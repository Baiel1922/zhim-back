package service

import (
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// OrderEvents learns about order changes after they are committed (the
// Telegram bot). Implementations must return immediately.
type OrderEvents interface {
	OrderCreated(orderID int64)
	OrderUpdated(orderID int64)
}

type noopOrderEvents struct{}

func (noopOrderEvents) OrderCreated(int64) {}
func (noopOrderEvents) OrderUpdated(int64) {}

// SetOrderEvents subscribes e to new orders. Call before serving requests.
func (c *Checkout) SetOrderEvents(e OrderEvents) { c.events = e }

// SetOrderEvents subscribes e to status and payment changes. Call before serving requests.
func (a *Admin) SetOrderEvents(e OrderEvents) { a.events = e }

// Conversions reports placed orders to ad platforms (Meta Conversions API,
// TikTok Events API). Implementations must return immediately.
type Conversions interface {
	Purchase(p Purchase)
}

// Purchase is a placed order as the ad platforms see it.
type Purchase struct {
	// EventID is the order number; the browser pixels send the same ID, so
	// the platforms count the purchase once.
	EventID string
	Time    time.Time
	Lang    db.AppLang
	Value   int32
	// Phone in E.164 (+996XXXXXXXXX); hashed by the sender.
	Phone            string
	Fbp, Fbc, Ttclid string
	Client           ClientInfo
	Items            []PurchaseItem
}

// PurchaseItem is one order line; ID is the SKU, as in the catalog feed.
type PurchaseItem struct {
	ID    string
	Qty   int32
	Price int32
}

// ClientInfo is the shopper's browser as seen by the API.
type ClientInfo struct {
	IP        string
	UserAgent string
}

type noopConversions struct{}

func (noopConversions) Purchase(Purchase) {}

// SetConversions subscribes v to placed orders. Call before serving requests.
func (c *Checkout) SetConversions(v Conversions) { c.conversions = v }

func purchaseOf(d *OrderDetails, client ClientInfo) Purchase {
	o := d.Order
	p := Purchase{
		EventID: o.Number,
		Time:    o.CreatedAt,
		Lang:    o.Lang,
		Value:   o.Total,
		Phone:   o.Phone,
		Fbp:     deref(o.Fbp),
		Fbc:     deref(o.Fbc),
		Ttclid:  deref(o.Ttclid),
		Client:  client,
		Items:   make([]PurchaseItem, len(d.Items)),
	}
	for i, it := range d.Items {
		p.Items[i] = PurchaseItem{ID: it.Sku, Qty: it.Qty, Price: it.Price}
	}
	return p
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
