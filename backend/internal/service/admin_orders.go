package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

const (
	defaultPerPage     = 20
	maxPerPage         = 100
	maxInternalComment = 2000
	lowStockThreshold  = 2
	topProductsLimit   = 5
)

// OrderFilter narrows the admin order list. Dates are calendar days in the
// business timezone, both inclusive.
type OrderFilter struct {
	Status        *db.OrderStatus
	From, To      *time.Time
	Query         string
	Page, PerPage int32
}

// OrderList is one page of orders plus per-status counts.
type OrderList struct {
	Orders       []db.AdminListOrdersRow
	Total        int32
	Page         int32
	PerPage      int32
	StatusCounts map[db.OrderStatus]int32
}

// ListOrders returns orders newest first.
func (a *Admin) ListOrders(ctx context.Context, f OrderFilter) (*OrderList, error) {
	var fe fields
	if f.Status != nil && !f.Status.Valid() {
		fe.add("status", FieldInvalid)
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PerPage == 0 {
		f.PerPage = defaultPerPage
	}
	if f.Page < 1 {
		fe.add("page", FieldInvalid)
	}
	if f.PerPage < 1 || f.PerPage > maxPerPage {
		fe.add("per_page", FieldInvalid)
	}
	if f.From != nil && f.To != nil && f.To.Before(*f.From) {
		fe.add("to", FieldInvalid)
	}
	if err := fe.err(); err != nil {
		return nil, err
	}

	var from, to *time.Time
	if f.From != nil {
		t := a.calendarDay(*f.From)
		from = &t
	}
	if f.To != nil {
		t := a.calendarDay(*f.To).AddDate(0, 0, 1)
		to = &t
	}
	var search, digits *string
	if q := strings.TrimSpace(f.Query); q != "" {
		escaped := escapeLike(q)
		search = &escaped
		if d := onlyDigits(q); len(d) >= 3 {
			digits = &d
		}
	}

	rows, err := a.store.AdminListOrders(ctx, db.AdminListOrdersParams{
		Status: f.Status, CreatedFrom: from, CreatedTo: to, Search: search, SearchDigits: digits,
		LimitRows: f.PerPage, OffsetRows: (f.Page - 1) * f.PerPage,
	})
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	counts, err := a.store.AdminCountOrdersByStatus(ctx, db.AdminCountOrdersByStatusParams{
		CreatedFrom: from, CreatedTo: to, Search: search, SearchDigits: digits,
	})
	if err != nil {
		return nil, fmt.Errorf("count orders: %w", err)
	}

	out := &OrderList{Orders: rows, Page: f.Page, PerPage: f.PerPage, StatusCounts: map[db.OrderStatus]int32{}}
	for _, s := range db.AllOrderStatusValues() {
		out.StatusCounts[s] = 0
	}
	for _, c := range counts {
		out.StatusCounts[c.Status] = c.Count
		if f.Status == nil || *f.Status == c.Status {
			out.Total += c.Count
		}
	}
	return out, nil
}

// AdminOrder is an order with its history and next possible statuses.
type AdminOrder struct {
	*OrderDetails
	History []db.ListOrderHistoryWithAdminsRow
	Allowed []db.OrderStatus
}

// GetOrder returns an order for the admin panel.
func (a *Admin) GetOrder(ctx context.Context, id int64) (*AdminOrder, error) {
	order, err := a.store.GetOrder(ctx, id)
	if err != nil {
		return nil, lookupErr(err, "order")
	}
	return a.adminOrder(ctx, order)
}

func (a *Admin) adminOrder(ctx context.Context, order db.Order) (*AdminOrder, error) {
	d, err := loadOrderDetails(ctx, a.store, order)
	if err != nil {
		return nil, err
	}
	history, err := a.store.ListOrderHistoryWithAdmins(ctx, order.ID)
	if err != nil {
		return nil, fmt.Errorf("list order history: %w", err)
	}
	return &AdminOrder{
		OrderDetails: d,
		History:      history,
		Allowed:      AllowedTransitions(order.Status, order.DeliveryMethod),
	}, nil
}

// SetOrderStatus moves an order along the allowed transitions and records who
// did it (adminID nil = system). Cancelling returns stock and the promo use.
func (a *Admin) SetOrderStatus(ctx context.Context, adminID *int64, orderID int64, to db.OrderStatus) (*AdminOrder, error) {
	if !to.Valid() {
		return nil, invalidField("status")
	}
	var (
		order   db.Order
		touched = map[int64]string{}
	)
	err := a.store.InTx(ctx, func(q *db.Queries) error {
		current, err := q.LockOrder(ctx, orderID)
		if err != nil {
			return lookupErr(err, "order")
		}
		if !CanTransition(current.Status, to, current.DeliveryMethod) {
			return newError(CodeInvalidStatusTransition, "cannot move order from %s to %s", current.Status, to)
		}
		if order, err = q.UpdateOrderStatus(ctx, db.UpdateOrderStatusParams{ID: orderID, FromStatus: current.Status, ToStatus: to}); err != nil {
			return fmt.Errorf("update order status: %w", err)
		}
		from := current.Status
		if _, err := q.CreateOrderStatusHistory(ctx, db.CreateOrderStatusHistoryParams{
			OrderID: orderID, FromStatus: &from, ToStatus: to, AdminID: adminID,
		}); err != nil {
			return fmt.Errorf("record status history: %w", err)
		}
		if to == db.OrderStatusCancelled {
			return restock(ctx, q, order, touched)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(touched) > 0 {
		a.notify.Notify(productTags(touched)...)
	}
	a.events.OrderUpdated(orderID)
	return a.adminOrder(ctx, order)
}

// restock returns the items of a cancelled order to stock and frees the promo use.
// touched collects product slugs whose availability changed.
func restock(ctx context.Context, q *db.Queries, order db.Order, touched map[int64]string) error {
	items, err := q.ListOrderItems(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("list order items: %w", err)
	}
	var productIDs []int64
	for _, it := range items {
		if it.VariantID == nil {
			continue // variant was deleted from the catalog
		}
		if err := q.IncrementVariantStock(ctx, db.IncrementVariantStockParams{ID: *it.VariantID, Qty: it.Qty}); err != nil {
			return fmt.Errorf("return stock: %w", err)
		}
		if it.ProductID != nil {
			productIDs = append(productIDs, *it.ProductID)
		}
	}
	if len(productIDs) > 0 {
		slugs, err := q.ListProductSlugs(ctx, productIDs)
		if err != nil {
			return fmt.Errorf("list product slugs: %w", err)
		}
		for _, s := range slugs {
			touched[s.ID] = s.Slug
		}
	}
	if order.PromoCodeID != nil {
		if err := q.DecrementPromoCodeUsage(ctx, *order.PromoCodeID); err != nil {
			return fmt.Errorf("free promo use: %w", err)
		}
	}
	return nil
}

// SetOrderPayment marks an order paid or unpaid.
func (a *Admin) SetOrderPayment(ctx context.Context, orderID int64, status db.PaymentStatus) (*AdminOrder, error) {
	if !status.Valid() {
		return nil, invalidField("payment_status")
	}
	order, err := a.store.UpdateOrderPaymentStatus(ctx, db.UpdateOrderPaymentStatusParams{ID: orderID, PaymentStatus: status})
	if err != nil {
		return nil, lookupErr(err, "order")
	}
	a.events.OrderUpdated(orderID)
	return a.adminOrder(ctx, order)
}

// SetOrderComment replaces the internal (admin-only) comment.
func (a *Admin) SetOrderComment(ctx context.Context, orderID int64, comment string) (*AdminOrder, error) {
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > maxInternalComment {
		return nil, &Error{Code: CodeValidation, Message: "comment is too long", Fields: []FieldError{{"internal_comment", FieldTooLong}}}
	}
	order, err := a.store.UpdateOrderInternalComment(ctx, db.UpdateOrderInternalCommentParams{ID: orderID, InternalComment: comment})
	if err != nil {
		return nil, lookupErr(err, "order")
	}
	return a.adminOrder(ctx, order)
}

// PeriodStats is the number of orders and revenue in a period.
type PeriodStats struct {
	Orders  int32
	Revenue int64
}

// Dashboard summarizes sales and stock.
type Dashboard struct {
	Today, Last7Days, Last30Days PeriodStats
	TopProducts                  []db.TopProductsSinceRow
	LowStock                     []db.ListLowStockVariantsRow
}

// Dashboard returns sales for today and the last 7/30 days (calendar days in
// the business timezone, today included), top sellers and low stock.
func (a *Admin) Dashboard(ctx context.Context) (*Dashboard, error) {
	today := a.startOfDay(a.now())
	d := &Dashboard{}
	for _, p := range []struct {
		since time.Time
		into  *PeriodStats
	}{
		{today, &d.Today},
		{today.AddDate(0, 0, -6), &d.Last7Days},
		{today.AddDate(0, 0, -29), &d.Last30Days},
	} {
		row, err := a.store.OrderStatsSince(ctx, p.since)
		if err != nil {
			return nil, fmt.Errorf("order stats: %w", err)
		}
		*p.into = PeriodStats{Orders: row.Orders, Revenue: row.Revenue}
	}
	var err error
	if d.TopProducts, err = a.store.TopProductsSince(ctx, db.TopProductsSinceParams{Since: today.AddDate(0, 0, -29), LimitRows: topProductsLimit}); err != nil {
		return nil, fmt.Errorf("top products: %w", err)
	}
	if d.LowStock, err = a.store.ListLowStockVariants(ctx, lowStockThreshold); err != nil {
		return nil, fmt.Errorf("low stock: %w", err)
	}
	return d, nil
}

// startOfDay is midnight, in the business timezone, of the day t falls on there.
func (a *Admin) startOfDay(t time.Time) time.Time {
	return a.calendarDay(t.In(a.loc))
}

// calendarDay is midnight in the business timezone of t's year-month-day as written
// (dates from query parameters arrive as UTC midnight).
func (a *Admin) calendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, a.loc)
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
