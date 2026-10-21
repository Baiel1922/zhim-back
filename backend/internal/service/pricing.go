package service

import (
	"math"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// Line is one priced cart line. Price is the server-side unit price.
type Line struct {
	Price int32
	Qty   int32
}

// Totals is the full price breakdown of an order.
type Totals struct {
	Subtotal      int32
	Discount      int32
	DeliveryPrice int32
	Total         int32
}

// ComputeTotals prices a cart: subtotal, promo discount (promo may be nil),
// delivery (zone is required for courier) and the grand total.
func ComputeTotals(lines []Line, promo *db.PromoCode, method db.DeliveryMethod, zone *db.DeliveryZone, now time.Time) (Totals, error) {
	var t Totals
	subtotal, err := Subtotal(lines)
	if err != nil {
		return t, err
	}
	t.Subtotal = subtotal
	if promo != nil {
		if t.Discount, err = PromoDiscount(*promo, subtotal, now); err != nil {
			return t, err
		}
	}
	t.DeliveryPrice = DeliveryPrice(method, zone, t.Subtotal-t.Discount)
	t.Total = t.Subtotal - t.Discount + t.DeliveryPrice
	return t, nil
}

// Subtotal sums the lines, rejecting carts too large to represent.
func Subtotal(lines []Line) (int32, error) {
	var sum int64
	for _, l := range lines {
		sum += int64(l.Price) * int64(l.Qty)
	}
	if sum > math.MaxInt32/2 { // leave room for delivery on top
		return 0, newError(CodeValidation, "order total is too large")
	}
	return int32(sum), nil
}

// PromoDiscount returns the discount the promo code gives on subtotal, or a
// promo_* error explaining why it does not apply. It does not reserve a use.
func PromoDiscount(p db.PromoCode, subtotal int32, now time.Time) (int32, error) {
	switch {
	case !p.IsActive:
		return 0, newError(CodePromoInactive, "promo code is disabled")
	case p.ValidFrom != nil && now.Before(*p.ValidFrom):
		return 0, newError(CodePromoNotStarted, "promo code is not active yet")
	case p.ValidTo != nil && !now.Before(*p.ValidTo):
		return 0, newError(CodePromoExpired, "promo code has expired")
	case p.MaxUses != nil && p.UsedCount >= *p.MaxUses:
		return 0, newError(CodePromoExhausted, "promo code has been used up")
	case subtotal < p.MinTotal:
		e := newError(CodePromoMinTotal, "subtotal is below the promo minimum")
		minTotal := p.MinTotal
		e.MinTotal = &minTotal
		return 0, e
	}

	var discount int32
	switch p.Type {
	case db.PromoTypePercent:
		// Rounded down to whole som, in the customer's disfavor by at most 1 som.
		d := min(int64(subtotal)*int64(p.Value)/100, int64(subtotal))
		discount = int32(d) //nolint:gosec // bounded by subtotal, which is int32
	case db.PromoTypeFixed:
		discount = p.Value
	}
	return min(discount, subtotal), nil
}

// DeliveryPrice is 0 for pickup; for courier it is the zone price unless the
// amount (subtotal after discount) reaches the zone's free delivery threshold.
func DeliveryPrice(method db.DeliveryMethod, zone *db.DeliveryZone, amount int32) int32 {
	if method != db.DeliveryMethodCourier || zone == nil {
		return 0
	}
	if zone.FreeFrom != nil && amount >= *zone.FreeFrom {
		return 0
	}
	return zone.Price
}
