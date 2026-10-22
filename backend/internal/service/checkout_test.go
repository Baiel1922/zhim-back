package service_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

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

// env is a seeded database with a Checkout on top. Seed prices: TFC 1990, TFP 2490;
// zone "Бишкек" costs 200, free from 5000.
type env struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	store    *repo.Store
	checkout *service.Checkout
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := pgtest.NewPool(t)
	store := repo.NewStore(pool)
	if _, err := seed.Run(t.Context(), store, seed.Options{}); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, ctx: t.Context(), pool: pool, store: store, checkout: service.NewCheckout(store, nil)}
}

func (e *env) variant(sku string) (id int64, stock int32) {
	e.t.Helper()
	err := e.pool.QueryRow(e.ctx, "SELECT id, stock FROM product_variants WHERE sku = $1", sku).Scan(&id, &stock)
	if err != nil {
		e.t.Fatalf("variant %s: %v", sku, err)
	}
	return id, stock
}

func (e *env) stock(sku string) int32 {
	e.t.Helper()
	_, s := e.variant(sku)
	return s
}

func (e *env) zone(nameRu string) int64 {
	e.t.Helper()
	var id int64
	if err := e.pool.QueryRow(e.ctx, "SELECT id FROM delivery_zones WHERE name_ru = $1", nameRu).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(e.ctx, sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) promo(code string, typ db.PromoType, value int32, maxUses *int32) db.PromoCode {
	e.t.Helper()
	p, err := e.store.CreatePromoCode(e.ctx, db.CreatePromoCodeParams{Code: code, Type: typ, Value: value, MaxUses: maxUses, IsActive: true})
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func pickupInput(key string, items ...service.ItemInput) service.OrderInput {
	return service.OrderInput{
		IdempotencyKey: key,
		Lang:           db.AppLangRu,
		CustomerName:   "  Азамат  ",
		Phone:          "0700 123 456",
		DeliveryMethod: db.DeliveryMethodPickup,
		PaymentMethod:  db.PaymentMethodCash,
		Items:          items,
	}
}

// wantCode fails unless err is a business error with the given code.
func wantCode(t *testing.T, err error, want service.Code) {
	t.Helper()
	_ = serviceError(t, err, want)
}

func serviceError(t *testing.T, err error, want service.Code) *service.Error {
	t.Helper()
	e, ok := service.AsError(err)
	if !ok || e.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
	return e
}

func TestCreateOrderPickup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M") // stock 9
	pro, _ := e.variant("ZH-TFP-BLK-S")  // stock 1

	in := pickupInput("key-pickup-0001",
		service.ItemInput{VariantID: core, Qty: 1},
		service.ItemInput{VariantID: pro, Qty: 1},
		service.ItemInput{VariantID: core, Qty: 1}, // duplicate line is merged
	)
	in.Attribution = service.Attribution{UtmSource: "instagram", Fbp: "fb.1.123"}
	in.Landmark = "у аптеки" // dropped: pickup orders have no courier hints
	d, err := e.checkout.CreateOrder(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	o := d.Order
	if !regexp.MustCompile(`^ZH-\d{6}$`).MatchString(o.Number) {
		t.Errorf("number = %q", o.Number)
	}
	if o.Subtotal != 1990*2+2490 || o.Discount != 0 || o.DeliveryPrice != 0 || o.Total != 6470 {
		t.Errorf("totals = %d/%d/%d/%d", o.Subtotal, o.Discount, o.DeliveryPrice, o.Total)
	}
	if o.Phone != "+996700123456" || o.CustomerName != "Азамат" {
		t.Errorf("customer = %q %q", o.CustomerName, o.Phone)
	}
	if o.UtmSource == nil || *o.UtmSource != "instagram" || o.UtmMedium != nil {
		t.Errorf("attribution = %v %v", o.UtmSource, o.UtmMedium)
	}
	if len(d.Items) != 2 || d.Items[0].Qty != 2 || d.Items[0].Sku != "ZH-TFC-BLK-M" || d.Items[0].ColorNameKy != "Кара" {
		t.Errorf("items = %+v", d.Items)
	}
	if d.ProductSlugs[*d.Items[0].ProductID] != "termofutbolka-core" {
		t.Errorf("slugs = %v", d.ProductSlugs)
	}
	if d.Pickup == nil || d.Pickup.AddressRu == "" || d.DeliveryZone != nil {
		t.Errorf("pickup = %+v, zone = %+v", d.Pickup, d.DeliveryZone)
	}
	if o.Address != nil || o.Landmark != nil {
		t.Errorf("address = %v landmark = %v", o.Address, o.Landmark)
	}

	if got := e.stock("ZH-TFC-BLK-M"); got != 7 {
		t.Errorf("core stock = %d, want 7", got)
	}
	if got := e.stock("ZH-TFP-BLK-S"); got != 0 {
		t.Errorf("pro stock = %d, want 0", got)
	}
	if n := e.count("SELECT count(*) FROM order_status_history WHERE order_id = $1 AND from_status IS NULL AND to_status = 'new'", o.ID); n != 1 {
		t.Errorf("history rows = %d", n)
	}
}

func TestCreateOrderCourierWithPromo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	promo := e.promo("WELCOME10", db.PromoTypePercent, 10, ptr(int32(5)))

	in := pickupInput("key-courier-0001", service.ItemInput{VariantID: core, Qty: 2})
	in.DeliveryMethod = db.DeliveryMethodCourier
	in.DeliveryZoneID = ptr(e.zone("Бишкек"))
	in.Address = " ул. Токтогула, 100 "
	in.Landmark = " вход со двора "
	in.PromoCode = "welcome10"
	d, err := e.checkout.CreateOrder(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	// 3980 − 10% (398) = 3582 < 5000 → delivery 200.
	o := d.Order
	if o.Subtotal != 3980 || o.Discount != 398 || o.DeliveryPrice != 200 || o.Total != 3782 {
		t.Errorf("totals = %d/%d/%d/%d", o.Subtotal, o.Discount, o.DeliveryPrice, o.Total)
	}
	if o.Address == nil || *o.Address != "ул. Токтогула, 100" || d.DeliveryZone == nil || d.Pickup != nil {
		t.Errorf("address = %v zone = %+v pickup = %+v", o.Address, d.DeliveryZone, d.Pickup)
	}
	if o.Landmark == nil || *o.Landmark != "вход со двора" {
		t.Errorf("landmark = %v", o.Landmark)
	}
	if d.PromoCode != "WELCOME10" {
		t.Errorf("promo = %q", d.PromoCode)
	}
	if n := e.count("SELECT used_count FROM promo_codes WHERE id = $1", promo.ID); n != 1 {
		t.Errorf("promo used_count = %d", n)
	}
}

func TestCreateOrderOutOfStockRollsBack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")      // 9
	pro, _ := e.variant("ZH-TFP-BLK-S")       // 1
	soldOut, _ := e.variant("ZH-TFP-RED-XXL") // 0

	_, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-oos-00001",
		service.ItemInput{VariantID: core, Qty: 1},
		service.ItemInput{VariantID: pro, Qty: 2},
		service.ItemInput{VariantID: soldOut, Qty: 1},
		service.ItemInput{VariantID: 999999, Qty: 1},
	))
	se := serviceError(t, err, service.CodeOutOfStock)
	want := fmt.Sprint([]service.StockIssue{{VariantID: pro, Available: 1}, {VariantID: soldOut, Available: 0}, {VariantID: 999999, Available: 0}})
	if got := fmt.Sprint(se.Items); got != want {
		t.Errorf("issues = %s, want %s", got, want)
	}
	if e.stock("ZH-TFC-BLK-M") != 9 || e.count("SELECT count(*) FROM orders") != 0 {
		t.Error("failed order must not change stock or create rows")
	}
}

func TestCreateOrderInactiveProduct(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	if _, err := e.pool.Exec(e.ctx, "UPDATE products SET is_active = false WHERE slug = 'termofutbolka-core'"); err != nil {
		t.Fatal(err)
	}
	_, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-inactive-1", service.ItemInput{VariantID: core, Qty: 1}))
	se := serviceError(t, err, service.CodeOutOfStock)
	if len(se.Items) != 1 || se.Items[0].Available != 0 {
		t.Errorf("issues = %+v", se.Items)
	}
}

func TestCreateOrderValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)

	in := service.OrderInput{
		IdempotencyKey: "short",
		CustomerName:   " ",
		Phone:          "+7 700 123 45 67",
		DeliveryMethod: db.DeliveryMethodCourier,
		Landmark:       strings.Repeat("я", 201),
		PaymentMethod:  "card",
		Items:          []service.ItemInput{{VariantID: 1, Qty: 0}, {VariantID: -1, Qty: 1}},
	}
	_, err := e.checkout.CreateOrder(e.ctx, in)
	se := serviceError(t, err, service.CodeValidation)
	got := map[string]string{}
	for _, f := range se.Fields {
		got[f.Field] = f.Code
	}
	want := map[string]string{
		"Idempotency-Key":     service.FieldInvalid,
		"customer_name":       service.FieldRequired,
		"phone":               service.FieldInvalid,
		"payment_method":      service.FieldInvalid,
		"delivery_zone_id":    service.FieldRequired,
		"address":             service.FieldRequired,
		"landmark":            service.FieldTooLong,
		"items[0].qty":        service.FieldInvalid,
		"items[1].variant_id": service.FieldInvalid,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("fields = %v\nwant     %v", got, want)
	}

	_, err = e.checkout.CreateOrder(e.ctx, pickupInput("key-empty-cart"))
	if se := serviceError(t, err, service.CodeValidation); len(se.Fields) != 1 || se.Fields[0].Field != "items" {
		t.Errorf("empty cart fields = %+v", se.Fields)
	}
}

func TestCreateOrderHoneypot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	in := pickupInput("key-honeypot-1", service.ItemInput{VariantID: core, Qty: 1})
	in.Honeypot = "http://spam.example"
	_, err := e.checkout.CreateOrder(e.ctx, in)
	if se := serviceError(t, err, service.CodeValidation); len(se.Fields) != 0 {
		t.Errorf("honeypot must not reveal the field: %+v", se.Fields)
	}
	if e.count("SELECT count(*) FROM orders") != 0 {
		t.Error("bot order was created")
	}
}

func TestCreateOrderIdempotency(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	in := pickupInput("key-idem-00001", service.ItemInput{VariantID: core, Qty: 1})

	first, err := e.checkout.CreateOrder(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Phone = "+996 700 123 456" // same number, different formatting
	second, err := e.checkout.CreateOrder(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.Order.ID != first.Order.ID || e.stock("ZH-TFC-BLK-M") != 8 {
		t.Fatalf("replay created a new order or decremented stock again")
	}

	in.Phone = "+996555000111"
	_, err = e.checkout.CreateOrder(e.ctx, in)
	wantCode(t, err, service.CodeIdempotencyConflict)
}

func TestCreateOrderConcurrentSameKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	in := pickupInput("key-same-00001", service.ItemInput{VariantID: core, Qty: 1})

	numbers := runConcurrently(t, 5, func(int) (*service.OrderDetails, error) {
		return e.checkout.CreateOrder(e.ctx, in)
	})
	for _, r := range numbers {
		if r.err != nil {
			t.Fatalf("concurrent retry failed: %v", r.err)
		}
		if r.order.Order.Number != numbers[0].order.Order.Number {
			t.Fatalf("got different orders for one key")
		}
	}
	if e.count("SELECT count(*) FROM orders") != 1 || e.stock("ZH-TFC-BLK-M") != 8 {
		t.Fatal("same key must produce exactly one order")
	}
}

func TestCreateOrderZoneUnavailable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	zone, err := e.store.UpsertDeliveryZone(e.ctx, db.UpsertDeliveryZoneParams{NameRu: "Ош", NameKy: "Ош", Price: 500, IsActive: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, zoneID := range []int64{zone.ID, 999999} {
		in := pickupInput(fmt.Sprintf("key-zone-%06d", zoneID), service.ItemInput{VariantID: core, Qty: 1})
		in.DeliveryMethod, in.DeliveryZoneID, in.Address = db.DeliveryMethodCourier, &zoneID, "ул. Ленина, 1"
		_, err := e.checkout.CreateOrder(e.ctx, in)
		wantCode(t, err, service.CodeDeliveryZoneUnavailable)
	}
	if e.stock("ZH-TFC-BLK-M") != 9 {
		t.Error("stock changed")
	}
}

// Ten customers race for the last unit: exactly one order wins.
func TestCreateOrderLastItemRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	last, stock := e.variant("ZH-TFP-BLK-S")
	if stock != 1 {
		t.Fatalf("seed stock = %d, want 1", stock)
	}

	results := runConcurrently(t, 10, func(i int) (*service.OrderDetails, error) {
		return e.checkout.CreateOrder(e.ctx, pickupInput(fmt.Sprintf("key-race-%05d", i), service.ItemInput{VariantID: last, Qty: 1}))
	})
	var won int
	for _, r := range results {
		if r.err == nil {
			won++
			continue
		}
		if se := serviceError(t, r.err, service.CodeOutOfStock); se.Items[0].Available != 0 {
			t.Errorf("loser saw available = %d", se.Items[0].Available)
		}
	}
	if won != 1 {
		t.Fatalf("%d orders won the last item, want 1", won)
	}
	if e.stock("ZH-TFP-BLK-S") != 0 || e.count("SELECT count(*) FROM orders") != 1 {
		t.Fatal("stock or order count is inconsistent")
	}
}

// A single-use promo code raced by five orders is applied once.
func TestCreateOrderPromoRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	promo := e.promo("ONCE", db.PromoTypeFixed, 300, ptr(int32(1)))

	results := runConcurrently(t, 5, func(i int) (*service.OrderDetails, error) {
		in := pickupInput(fmt.Sprintf("key-promo-%05d", i), service.ItemInput{VariantID: core, Qty: 1})
		in.PromoCode = "ONCE"
		return e.checkout.CreateOrder(e.ctx, in)
	})
	var won int
	for _, r := range results {
		if r.err == nil {
			won++
			continue
		}
		wantCode(t, r.err, service.CodePromoExhausted)
	}
	if won != 1 || e.count("SELECT used_count FROM promo_codes WHERE id = $1", promo.ID) != 1 {
		t.Fatalf("promo applied %d times", won)
	}
	if e.stock("ZH-TFC-BLK-M") != 8 {
		t.Fatal("rejected orders must not keep their stock")
	}
}

func TestGetOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	created, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-get-000001", service.ItemInput{VariantID: core, Qty: 1}))
	if err != nil {
		t.Fatal(err)
	}
	number := created.Order.Number

	got, err := e.checkout.GetOrder(e.ctx, " "+regexp.MustCompile(`ZH`).ReplaceAllString(number, "zh"), "+996 (700) 123-456")
	if err != nil || got.Order.ID != created.Order.ID || len(got.Items) != 1 {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
	_, err = e.checkout.GetOrder(e.ctx, number, "+996555000111")
	wantCode(t, err, service.CodeOrderNotFound)
	_, err = e.checkout.GetOrder(e.ctx, number, "garbage")
	wantCode(t, err, service.CodeValidation)
}

func TestValidatePromo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.promo("SPRING", db.PromoTypePercent, 20, nil)
	min, err := e.store.CreatePromoCode(e.ctx, db.CreatePromoCodeParams{Code: "BIG", Type: db.PromoTypeFixed, Value: 500, MinTotal: 5000, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.checkout.ValidatePromo(e.ctx, " spring ", 2490)
	if err != nil || res.Discount != 498 || res.Promo.Code != "SPRING" {
		t.Fatalf("validate = %+v, %v", res, err)
	}
	_, err = e.checkout.ValidatePromo(e.ctx, "NOPE", 2490)
	wantCode(t, err, service.CodePromoNotFound)
	_, err = e.checkout.ValidatePromo(e.ctx, min.Code, 4999)
	if se := serviceError(t, err, service.CodePromoMinTotal); se.MinTotal == nil || *se.MinTotal != 5000 {
		t.Errorf("min total = %v", se.MinTotal)
	}
	_, err = e.checkout.ValidatePromo(e.ctx, "", 100)
	wantCode(t, err, service.CodeValidation)
}

type result struct {
	order *service.OrderDetails
	err   error
}

// runConcurrently starts n calls at once and collects their results.
func runConcurrently(t *testing.T, n int, fn func(i int) (*service.OrderDetails, error)) []result {
	t.Helper()
	results := make([]result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			o, err := fn(i)
			results[i] = result{o, err}
		}()
	}
	close(start)
	wg.Wait()
	return results
}
