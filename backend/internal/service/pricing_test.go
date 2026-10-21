package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func code(err error) service.Code {
	var e *service.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestSubtotal(t *testing.T) {
	got, err := service.Subtotal([]service.Line{{Price: 1990, Qty: 2}, {Price: 2490, Qty: 1}})
	if err != nil || got != 6470 {
		t.Fatalf("subtotal = %d, %v", got, err)
	}
	if _, err := service.Subtotal([]service.Line{{Price: 2_000_000_000, Qty: 99}}); code(err) != service.CodeValidation {
		t.Fatalf("overflow must be rejected, got %v", err)
	}
}

func TestPromoDiscount(t *testing.T) {
	percent10 := db.PromoCode{Type: db.PromoTypePercent, Value: 10, IsActive: true}
	fixed500 := db.PromoCode{Type: db.PromoTypeFixed, Value: 500, IsActive: true}

	tests := []struct {
		name     string
		promo    db.PromoCode
		subtotal int32
		want     int32
		wantCode service.Code
	}{
		{"percent", percent10, 5000, 500, ""},
		{"percent rounds down", percent10, 1995, 199, ""},
		{"percent 100 makes items free", db.PromoCode{Type: db.PromoTypePercent, Value: 100, IsActive: true}, 1990, 1990, ""},
		{"fixed", fixed500, 1990, 500, ""},
		{"fixed is capped by subtotal", fixed500, 300, 300, ""},
		{"min total reached", withMinTotal(fixed500, 3000), 3000, 500, ""},
		{"min total not reached", withMinTotal(fixed500, 3000), 2999, 0, service.CodePromoMinTotal},
		{"inactive", db.PromoCode{Type: db.PromoTypeFixed, Value: 500}, 5000, 0, service.CodePromoInactive},
		{"not started", withValidity(fixed500, ptr(now.Add(time.Hour)), nil), 5000, 0, service.CodePromoNotStarted},
		{"started", withValidity(fixed500, ptr(now.Add(-time.Hour)), ptr(now.Add(time.Hour))), 5000, 500, ""},
		{"expired at valid_to", withValidity(fixed500, nil, ptr(now)), 5000, 0, service.CodePromoExpired},
		{"exhausted", withUses(fixed500, 3, 3), 5000, 0, service.CodePromoExhausted},
		{"uses left", withUses(fixed500, 3, 2), 5000, 500, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.PromoDiscount(tt.promo, tt.subtotal, now)
			if code(err) != tt.wantCode {
				t.Fatalf("error = %v, want code %q", err, tt.wantCode)
			}
			if got != tt.want {
				t.Fatalf("discount = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPromoMinTotalDetail(t *testing.T) {
	_, err := service.PromoDiscount(withMinTotal(db.PromoCode{Type: db.PromoTypeFixed, Value: 100, IsActive: true}, 3000), 100, now)
	var e *service.Error
	if !errors.As(err, &e) || e.MinTotal == nil || *e.MinTotal != 3000 {
		t.Fatalf("want min_total detail 3000, got %+v", err)
	}
}

func TestDeliveryPrice(t *testing.T) {
	zone := &db.DeliveryZone{Price: 200, FreeFrom: ptr(int32(5000))}
	noThreshold := &db.DeliveryZone{Price: 350}

	tests := []struct {
		name   string
		method db.DeliveryMethod
		zone   *db.DeliveryZone
		amount int32
		want   int32
	}{
		{"pickup is free", db.DeliveryMethodPickup, nil, 100, 0},
		{"courier below threshold", db.DeliveryMethodCourier, zone, 4999, 200},
		{"courier at threshold", db.DeliveryMethodCourier, zone, 5000, 0},
		{"courier without threshold", db.DeliveryMethodCourier, noThreshold, 100000, 350},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.DeliveryPrice(tt.method, tt.zone, tt.amount); got != tt.want {
				t.Fatalf("delivery = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestComputeTotals(t *testing.T) {
	zone := &db.DeliveryZone{Price: 200, FreeFrom: ptr(int32(5000))}
	lines := []service.Line{{Price: 2790, Qty: 2}} // 5580

	t.Run("no promo, free delivery over threshold", func(t *testing.T) {
		got, err := service.ComputeTotals(lines, nil, db.DeliveryMethodCourier, zone, now)
		want := service.Totals{Subtotal: 5580, Discount: 0, DeliveryPrice: 0, Total: 5580}
		if err != nil || got != want {
			t.Fatalf("totals = %+v, %v", got, err)
		}
	})
	t.Run("discount pushes below threshold, delivery is charged", func(t *testing.T) {
		promo := &db.PromoCode{Type: db.PromoTypePercent, Value: 15, IsActive: true}
		got, err := service.ComputeTotals(lines, promo, db.DeliveryMethodCourier, zone, now)
		// 15% of 5580 = 837 → 4743 < 5000 → +200
		want := service.Totals{Subtotal: 5580, Discount: 837, DeliveryPrice: 200, Total: 4943}
		if err != nil || got != want {
			t.Fatalf("totals = %+v, %v", got, err)
		}
	})
	t.Run("pickup with fixed promo", func(t *testing.T) {
		promo := &db.PromoCode{Type: db.PromoTypeFixed, Value: 1000, IsActive: true}
		got, err := service.ComputeTotals(lines, promo, db.DeliveryMethodPickup, nil, now)
		want := service.Totals{Subtotal: 5580, Discount: 1000, DeliveryPrice: 0, Total: 4580}
		if err != nil || got != want {
			t.Fatalf("totals = %+v, %v", got, err)
		}
	})
	t.Run("promo error propagates", func(t *testing.T) {
		promo := &db.PromoCode{Type: db.PromoTypeFixed, Value: 1000}
		if _, err := service.ComputeTotals(lines, promo, db.DeliveryMethodPickup, nil, now); code(err) != service.CodePromoInactive {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNormalizePhone(t *testing.T) {
	valid := map[string]string{
		"+996700123456":       "+996700123456",
		"+996 (700) 12-34-56": "+996700123456",
		"996700123456":        "+996700123456",
		"0700 123 456":        "+996700123456",
		"700123456":           "+996700123456",
		" +996555000111 ":     "+996555000111",
	}
	for in, want := range valid {
		if got, ok := service.NormalizePhone(in); !ok || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "+7 700 123 45 67", "99670012345", "+996700123456789", "0700-12345", "+996 700 123 45a", "++996700123456"} {
		if got, ok := service.NormalizePhone(in); ok {
			t.Errorf("NormalizePhone(%q) = %q, want invalid", in, got)
		}
	}
}

func withMinTotal(p db.PromoCode, v int32) db.PromoCode { p.MinTotal = v; return p }

func withValidity(p db.PromoCode, from, to *time.Time) db.PromoCode {
	p.ValidFrom, p.ValidTo = from, to
	return p
}

func withUses(p db.PromoCode, maxUses, used int32) db.PromoCode {
	p.MaxUses, p.UsedCount = &maxUses, used
	return p
}
