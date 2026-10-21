package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// Input limits (mirrored in openapi.yaml).
const (
	maxNameLen        = 100
	maxAddressLen     = 300
	maxCommentLen     = 1000
	maxPromoLen       = 32
	maxAttributionLen = 255
	maxCartLines      = 30
	maxQty            = 99
	minIdempotencyLen = 8
	maxIdempotencyLen = 128
)

// Checkout validates promo codes and places orders.
type Checkout struct {
	store  *repo.Store
	notify Revalidator
	events OrderEvents
	now    func() time.Time
}

// NewCheckout creates a Checkout. notify (may be nil) learns about stock changes.
func NewCheckout(store *repo.Store, notify Revalidator) *Checkout {
	return &Checkout{store: store, notify: notifierOrNoop(notify), events: noopOrderEvents{}, now: time.Now}
}

// PromoResult is a promo code that applies to a subtotal.
type PromoResult struct {
	Promo    db.PromoCode
	Discount int32
}

// ValidatePromo checks code against subtotal without reserving a use.
func (c *Checkout) ValidatePromo(ctx context.Context, code string, subtotal int32) (*PromoResult, error) {
	code = strings.TrimSpace(code)
	var fields []FieldError
	switch {
	case code == "":
		fields = append(fields, FieldError{"code", FieldRequired})
	case utf8.RuneCountInString(code) > maxPromoLen:
		fields = append(fields, FieldError{"code", FieldTooLong})
	}
	if subtotal < 0 {
		fields = append(fields, FieldError{"subtotal", FieldInvalid})
	}
	if len(fields) > 0 {
		return nil, &Error{Code: CodeValidation, Message: "invalid promo request", Fields: fields}
	}

	promo, err := c.store.GetPromoCodeByCode(ctx, code)
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, newError(CodePromoNotFound, "promo code not found")
		}
		return nil, fmt.Errorf("get promo code: %w", err)
	}
	discount, err := PromoDiscount(promo, subtotal, c.now())
	if err != nil {
		return nil, err
	}
	return &PromoResult{Promo: promo, Discount: discount}, nil
}

// OrderInput is a checkout request as received from the client.
type OrderInput struct {
	IdempotencyKey string
	Lang           db.AppLang
	CustomerName   string
	Phone          string
	DeliveryMethod db.DeliveryMethod
	DeliveryZoneID *int64
	Address        string
	PaymentMethod  db.PaymentMethod
	Comment        string
	PromoCode      string
	Items          []ItemInput
	Attribution    Attribution
	// Honeypot must be empty; bots fill every field.
	Honeypot string
}

// ItemInput is one cart line.
type ItemInput struct {
	VariantID int64
	Qty       int32
}

// Attribution holds first-visit marketing parameters.
type Attribution struct {
	UtmSource, UtmMedium, UtmCampaign, UtmContent string
	Fbp, Fbc, Ttclid                              string
}

// OrderDetails is an order with everything needed to display it.
type OrderDetails struct {
	Order        db.Order
	Items        []db.OrderItem
	DeliveryZone *db.DeliveryZone
	PromoCode    string
	// Pickup point, for pickup orders.
	Pickup       *domain.Pickup
	ProductSlugs map[int64]string
}

// CreateOrder places an order in one transaction: it locks the variants, checks
// stock, prices everything on the server, decrements stock, reserves a promo
// use and records the initial status. A repeated idempotency key returns the
// original order.
func (c *Checkout) CreateOrder(ctx context.Context, in OrderInput) (*OrderDetails, error) {
	in, items, err := normalizeOrder(in)
	if err != nil {
		return nil, err
	}

	if existing, err := c.replay(ctx, in.IdempotencyKey, in.Phone); existing != nil || err != nil {
		return existing, err
	}

	var order db.Order
	err = c.store.InTx(ctx, func(q *db.Queries) error {
		order, err = c.placeOrder(ctx, q, in, items)
		return err
	})
	if repo.IsUniqueViolation(err, "orders_idempotency_key_key") {
		// A concurrent request with the same key won the race.
		return c.replay(ctx, in.IdempotencyKey, in.Phone)
	}
	if err != nil {
		return nil, err
	}
	d, err := loadOrderDetails(ctx, c.store, order)
	if err != nil {
		return nil, err
	}
	// Stock changed: refresh availability on the storefront.
	c.notify.Notify(productTags(d.ProductSlugs)...)
	c.events.OrderCreated(order.ID)
	return d, nil
}

func (c *Checkout) placeOrder(ctx context.Context, q *db.Queries, in OrderInput, items []ItemInput) (db.Order, error) {
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.VariantID
	}
	locked, err := q.LockVariantsForOrder(ctx, ids)
	if err != nil {
		return db.Order{}, fmt.Errorf("lock variants: %w", err)
	}
	byID := make(map[int64]db.LockVariantsForOrderRow, len(locked))
	for _, v := range locked {
		byID[v.ID] = v
	}

	var issues []StockIssue
	lines := make([]Line, len(items))
	for i, it := range items {
		v, ok := byID[it.VariantID]
		switch {
		case !ok || !v.IsActive || !v.ProductIsActive:
			issues = append(issues, StockIssue{VariantID: it.VariantID, Available: 0})
		case v.Stock < it.Qty:
			issues = append(issues, StockIssue{VariantID: it.VariantID, Available: v.Stock})
		}
		lines[i] = Line{Price: v.Price, Qty: it.Qty}
	}
	if len(issues) > 0 {
		return db.Order{}, &Error{Code: CodeOutOfStock, Message: "some items are out of stock", Items: issues}
	}

	var zone *db.DeliveryZone
	if in.DeliveryMethod == db.DeliveryMethodCourier {
		z, err := q.GetDeliveryZone(ctx, *in.DeliveryZoneID)
		if err != nil && !repo.IsNotFound(err) {
			return db.Order{}, fmt.Errorf("get delivery zone: %w", err)
		}
		if err != nil || !z.IsActive {
			return db.Order{}, newError(CodeDeliveryZoneUnavailable, "delivery zone %d is unavailable", *in.DeliveryZoneID)
		}
		zone = &z
	}

	var promo *db.PromoCode
	if in.PromoCode != "" {
		p, err := q.GetPromoCodeByCode(ctx, in.PromoCode)
		if err != nil {
			if repo.IsNotFound(err) {
				return db.Order{}, newError(CodePromoNotFound, "promo code not found")
			}
			return db.Order{}, fmt.Errorf("get promo code: %w", err)
		}
		promo = &p
	}

	totals, err := ComputeTotals(lines, promo, in.DeliveryMethod, zone, c.now())
	if err != nil {
		return db.Order{}, err
	}

	params := db.CreateOrderParams{
		PaymentMethod:  in.PaymentMethod,
		CustomerName:   in.CustomerName,
		Phone:          in.Phone,
		Comment:        in.Comment,
		DeliveryMethod: in.DeliveryMethod,
		Subtotal:       totals.Subtotal,
		DeliveryPrice:  totals.DeliveryPrice,
		Discount:       totals.Discount,
		Total:          totals.Total,
		Lang:           in.Lang,
		UtmSource:      optional(in.Attribution.UtmSource),
		UtmMedium:      optional(in.Attribution.UtmMedium),
		UtmCampaign:    optional(in.Attribution.UtmCampaign),
		UtmContent:     optional(in.Attribution.UtmContent),
		Fbp:            optional(in.Attribution.Fbp),
		Fbc:            optional(in.Attribution.Fbc),
		Ttclid:         optional(in.Attribution.Ttclid),
		IdempotencyKey: in.IdempotencyKey,
	}
	if zone != nil {
		params.DeliveryZoneID = &zone.ID
		params.Address = &in.Address
	}
	if promo != nil {
		params.PromoCodeID = &promo.ID
	}
	order, err := q.CreateOrder(ctx, params)
	if err != nil {
		return db.Order{}, fmt.Errorf("create order: %w", err)
	}

	for _, it := range items {
		v := byID[it.VariantID]
		if _, err := q.CreateOrderItem(ctx, db.CreateOrderItemParams{
			OrderID:     order.ID,
			VariantID:   &v.ID,
			ProductID:   &v.ProductID,
			TitleRu:     v.TitleRu,
			TitleKy:     v.TitleKy,
			ColorNameRu: v.ColorNameRu,
			ColorNameKy: v.ColorNameKy,
			Size:        v.Size,
			Sku:         v.Sku,
			Price:       v.Price,
			Qty:         it.Qty,
		}); err != nil {
			return db.Order{}, fmt.Errorf("create order item: %w", err)
		}
		n, err := q.DecrementVariantStock(ctx, db.DecrementVariantStockParams{ID: v.ID, Qty: it.Qty})
		if err != nil {
			return db.Order{}, fmt.Errorf("decrement stock: %w", err)
		}
		if n != 1 {
			// Impossible while the row is locked; guards against a broken invariant.
			return db.Order{}, fmt.Errorf("decrement stock for variant %d affected %d rows", v.ID, n)
		}
	}

	if promo != nil {
		n, err := q.IncrementPromoCodeUsage(ctx, promo.ID)
		if err != nil {
			return db.Order{}, fmt.Errorf("reserve promo use: %w", err)
		}
		if n == 0 {
			return db.Order{}, newError(CodePromoExhausted, "promo code has been used up")
		}
	}

	if _, err := q.CreateOrderStatusHistory(ctx, db.CreateOrderStatusHistoryParams{
		OrderID: order.ID, ToStatus: db.OrderStatusNew,
	}); err != nil {
		return db.Order{}, fmt.Errorf("record status history: %w", err)
	}
	return order, nil
}

// replay returns the order already created with key, or nil if there is none.
func (c *Checkout) replay(ctx context.Context, key, phone string) (*OrderDetails, error) {
	order, err := c.store.GetOrderByIdempotencyKey(ctx, key)
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get order by idempotency key: %w", err)
	}
	if order.Phone != phone {
		return nil, newError(CodeIdempotencyConflict, "idempotency key was used for another order")
	}
	return loadOrderDetails(ctx, c.store, order)
}

// GetOrder returns an order for the thank-you page. A wrong phone looks like a missing order.
func (c *Checkout) GetOrder(ctx context.Context, number, phone string) (*OrderDetails, error) {
	normalized, ok := NormalizePhone(phone)
	if !ok {
		return nil, &Error{Code: CodeValidation, Message: "invalid phone", Fields: []FieldError{{"phone", FieldInvalid}}}
	}
	order, err := c.store.GetOrderByNumberAndPhone(ctx, db.GetOrderByNumberAndPhoneParams{
		Number: strings.ToUpper(strings.TrimSpace(number)), Phone: normalized,
	})
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, newError(CodeOrderNotFound, "order not found")
		}
		return nil, fmt.Errorf("get order: %w", err)
	}
	return loadOrderDetails(ctx, c.store, order)
}

// loadOrderDetails gathers items, zone, promo code and pickup point of an order.
func loadOrderDetails(ctx context.Context, store *repo.Store, order db.Order) (*OrderDetails, error) {
	d := &OrderDetails{Order: order, ProductSlugs: map[int64]string{}}
	var err error
	if d.Items, err = store.ListOrderItems(ctx, order.ID); err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}

	var productIDs []int64
	for _, it := range d.Items {
		if it.ProductID != nil {
			productIDs = append(productIDs, *it.ProductID)
		}
	}
	if len(productIDs) > 0 {
		slugs, err := store.ListProductSlugs(ctx, productIDs)
		if err != nil {
			return nil, fmt.Errorf("list product slugs: %w", err)
		}
		for _, s := range slugs {
			d.ProductSlugs[s.ID] = s.Slug
		}
	}

	if order.DeliveryZoneID != nil {
		zone, err := store.GetDeliveryZone(ctx, *order.DeliveryZoneID)
		if err != nil {
			return nil, fmt.Errorf("get delivery zone: %w", err)
		}
		d.DeliveryZone = &zone
	}
	if order.DeliveryMethod == db.DeliveryMethodPickup {
		pickup, err := readPickup(ctx, store.Queries)
		if err != nil {
			return nil, err
		}
		d.Pickup = &pickup
	}
	if order.PromoCodeID != nil {
		promo, err := store.GetPromoCodeByID(ctx, *order.PromoCodeID)
		if err != nil && !repo.IsNotFound(err) {
			return nil, fmt.Errorf("get promo code: %w", err)
		}
		d.PromoCode = promo.Code
	}
	return d, nil
}

// normalizeOrder validates the input, trims strings, normalizes the phone and
// merges duplicate cart lines (returned in first-seen order).
func normalizeOrder(in OrderInput) (OrderInput, []ItemInput, error) {
	var fields []FieldError
	add := func(field, code string) { fields = append(fields, FieldError{field, code}) }

	if in.Honeypot != "" {
		return in, nil, newError(CodeValidation, "invalid request")
	}
	if n := len(in.IdempotencyKey); n < minIdempotencyLen || n > maxIdempotencyLen {
		add("Idempotency-Key", FieldInvalid)
	}
	if !in.Lang.Valid() {
		in.Lang = db.AppLangRu
	}

	in.CustomerName = strings.TrimSpace(in.CustomerName)
	switch {
	case in.CustomerName == "":
		add("customer_name", FieldRequired)
	case utf8.RuneCountInString(in.CustomerName) > maxNameLen:
		add("customer_name", FieldTooLong)
	}

	if strings.TrimSpace(in.Phone) == "" {
		add("phone", FieldRequired)
	} else if phone, ok := NormalizePhone(in.Phone); ok {
		in.Phone = phone
	} else {
		add("phone", FieldInvalid)
	}

	if !in.PaymentMethod.Valid() {
		add("payment_method", FieldInvalid)
	}

	in.Address = strings.TrimSpace(in.Address)
	switch in.DeliveryMethod {
	case db.DeliveryMethodCourier:
		if in.DeliveryZoneID == nil {
			add("delivery_zone_id", FieldRequired)
		}
		switch {
		case in.Address == "":
			add("address", FieldRequired)
		case utf8.RuneCountInString(in.Address) > maxAddressLen:
			add("address", FieldTooLong)
		}
	case db.DeliveryMethodPickup:
		in.DeliveryZoneID, in.Address = nil, ""
	default:
		add("delivery_method", FieldInvalid)
	}

	in.Comment = strings.TrimSpace(in.Comment)
	if utf8.RuneCountInString(in.Comment) > maxCommentLen {
		add("comment", FieldTooLong)
	}
	in.PromoCode = strings.TrimSpace(in.PromoCode)
	if utf8.RuneCountInString(in.PromoCode) > maxPromoLen {
		add("promo_code", FieldTooLong)
	}

	var items []ItemInput
	switch {
	case len(in.Items) == 0:
		add("items", FieldRequired)
	case len(in.Items) > maxCartLines:
		add("items", FieldTooMany)
	default:
		index := map[int64]int{}
		for i, it := range in.Items {
			if it.VariantID <= 0 {
				add(fmt.Sprintf("items[%d].variant_id", i), FieldInvalid)
				continue
			}
			if it.Qty < 1 || it.Qty > maxQty {
				add(fmt.Sprintf("items[%d].qty", i), FieldInvalid)
				continue
			}
			if j, ok := index[it.VariantID]; ok {
				items[j].Qty += it.Qty
				if items[j].Qty > maxQty {
					add(fmt.Sprintf("items[%d].qty", i), FieldInvalid)
				}
				continue
			}
			index[it.VariantID] = len(items)
			items = append(items, it)
		}
	}

	a := &in.Attribution
	for _, s := range []*string{&a.UtmSource, &a.UtmMedium, &a.UtmCampaign, &a.UtmContent, &a.Fbp, &a.Fbc, &a.Ttclid} {
		*s = truncate(strings.TrimSpace(*s), maxAttributionLen)
	}

	if len(fields) > 0 {
		return in, nil, &Error{Code: CodeValidation, Message: "invalid order", Fields: fields}
	}
	return in, items, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}

// AsError extracts a business error.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
