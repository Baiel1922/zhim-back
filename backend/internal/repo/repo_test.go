package repo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// fixture creates catalog rows with minimal boilerplate.
type fixture struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	store *repo.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := pgtest.NewPool(t)
	return &fixture{t: t, ctx: t.Context(), pool: pool, store: repo.NewStore(pool)}
}

func (f *fixture) category(slug string, sort int32) db.Category {
	f.t.Helper()
	c, err := f.store.UpsertCategory(f.ctx, db.UpsertCategoryParams{Slug: slug, NameRu: "Кат " + slug, NameKy: "Кат " + slug, Sort: sort})
	must(f.t, err)
	return c
}

func (f *fixture) color(nameRu, hex string) db.Color {
	f.t.Helper()
	c, err := f.store.UpsertColor(f.ctx, db.UpsertColorParams{NameRu: nameRu, NameKy: nameRu, Hex: hex})
	must(f.t, err)
	return c
}

func (f *fixture) productParams(categoryID int64, slug string, price int32) db.UpsertProductParams {
	return db.UpsertProductParams{
		Slug: slug, CategoryID: categoryID, TitleRu: "Товар " + slug, TitleKy: "Буюм " + slug,
		Price: price, IsActive: true,
	}
}

func (f *fixture) product(categoryID int64, slug string, price int32, active bool, sort int32) db.Product {
	f.t.Helper()
	p := f.productParams(categoryID, slug, price)
	p.IsActive, p.Sort = active, sort
	row, err := f.store.UpsertProduct(f.ctx, p)
	must(f.t, err)
	return row
}

func (f *fixture) variant(productID, colorID int64, size, sku string, stock int32, active bool) db.ProductVariant {
	f.t.Helper()
	v, err := f.store.UpsertVariant(f.ctx, db.UpsertVariantParams{
		ProductID: productID, ColorID: colorID, Size: size, Sku: sku, Stock: stock, IsActive: active,
	})
	must(f.t, err)
	return v
}

// pickupOrder returns valid params for a pickup order with one 1000 som item.
func pickupOrder(key string) db.CreateOrderParams {
	return db.CreateOrderParams{
		PaymentMethod:  db.PaymentMethodCash,
		CustomerName:   "Азамат",
		Phone:          "+996700123456",
		DeliveryMethod: db.DeliveryMethodPickup,
		Subtotal:       1000,
		Total:          1000,
		Lang:           db.AppLangKy,
		IdempotencyKey: key,
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantUnique(t *testing.T, err error, constraint string) {
	t.Helper()
	if !repo.IsUniqueViolation(err, constraint) {
		t.Fatalf("want unique violation %q, got %v", constraint, err)
	}
}

func wantCheck(t *testing.T, err error, constraint string) {
	t.Helper()
	if !repo.IsCheckViolation(err, constraint) {
		t.Fatalf("want check violation %q, got %v", constraint, err)
	}
}

func TestMigrationsRollBackAndReapply(t *testing.T) {
	t.Parallel()
	sqlDB, err := repo.OpenSQL(pgtest.NewDatabase(t))
	must(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	migrator, err := repo.NewMigrator(sqlDB)
	must(t, err)
	ctx := t.Context()

	_, err = migrator.DownTo(ctx, 0)
	must(t, err)
	if v, err := migrator.GetDBVersion(ctx); err != nil || v != 0 {
		t.Fatalf("after down: version = %d, err = %v", v, err)
	}
	_, err = migrator.Up(ctx)
	must(t, err)
	if pending, err := migrator.HasPending(ctx); err != nil || pending {
		t.Fatalf("after up: pending = %v, err = %v", pending, err)
	}
}

func TestCatalogConstraints(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	cat := f.category("tees", 0)
	black := f.color("Чёрный", "#0A0A0A")
	p := f.product(cat.ID, "core-tee", 1990, true, 0)
	f.variant(p.ID, black.ID, "M", "ZH-T-BLK-M", 3, true)

	t.Run("negative stock", func(t *testing.T) {
		_, err := f.store.UpsertVariant(f.ctx, db.UpsertVariantParams{ProductID: p.ID, ColorID: black.ID, Size: "L", Sku: "ZH-T-BLK-L", Stock: -1})
		wantCheck(t, err, "product_variants_stock_check")
	})
	t.Run("duplicate product+color+size", func(t *testing.T) {
		_, err := f.store.UpsertVariant(f.ctx, db.UpsertVariantParams{ProductID: p.ID, ColorID: black.ID, Size: "M", Sku: "ZH-T-BLK-M2", Stock: 1})
		wantUnique(t, err, "product_variants_product_color_size_key")
	})
	t.Run("old price must exceed price", func(t *testing.T) {
		params := f.productParams(cat.ID, "sale-tee", 1990)
		params.OldPrice = ptr(int32(1990))
		_, err := f.store.UpsertProduct(f.ctx, params)
		wantCheck(t, err, "products_old_price_chk")
	})
	t.Run("slug format", func(t *testing.T) {
		_, err := f.store.UpsertProduct(f.ctx, f.productParams(cat.ID, "Bad Slug", 1990))
		wantCheck(t, err, "products_slug_check")
	})
	t.Run("hex format", func(t *testing.T) {
		_, err := f.store.UpsertColor(f.ctx, db.UpsertColorParams{NameRu: "Красный", NameKy: "Кызыл", Hex: "red"})
		wantCheck(t, err, "colors_hex_check")
	})
	t.Run("category with products cannot be deleted", func(t *testing.T) {
		_, err := f.pool.Exec(f.ctx, "DELETE FROM categories WHERE id = $1", cat.ID)
		if !repo.IsForeignKeyViolation(err, "products_category_id_fkey") {
			t.Fatalf("want FK violation, got %v", err)
		}
	})
	t.Run("updated_at trigger", func(t *testing.T) {
		params := f.productParams(cat.ID, "core-tee", 2190)
		updated, err := f.store.UpsertProduct(f.ctx, params)
		must(t, err)
		if updated.ID != p.ID || updated.Price != 2190 || !updated.UpdatedAt.After(p.UpdatedAt) {
			t.Fatalf("upsert did not update in place: %+v", updated)
		}
	})
}

func TestListActiveProducts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tees := f.category("tees", 0)
	shorts := f.category("shorts", 10)
	black := f.color("Чёрный", "#0A0A0A")
	white := f.color("Белый", "#F5F5F5")

	tee := f.product(tees.ID, "tee", 2000, true, 0)
	f.variant(tee.ID, black.ID, "XXL", "ZH-T-BLK-XXL", 1, true)
	f.variant(tee.ID, black.ID, "S", "ZH-T-BLK-S", 2, true)
	f.variant(tee.ID, black.ID, "M", "ZH-T-BLK-M", 0, true)  // out of stock
	f.variant(tee.ID, white.ID, "L", "ZH-T-WHT-L", 5, false) // inactive
	for _, sort := range []int32{2, 0, 1} {
		_, err := f.store.CreateProductImage(f.ctx, db.CreateProductImageParams{
			ProductID: tee.ID, S3Key: fmt.Sprintf("products/tee/%d.jpg", sort), Sort: sort,
		})
		must(t, err)
	}
	short := f.product(shorts.ID, "short", 1000, true, 10)
	f.product(tees.ID, "hidden", 500, false, 5)

	all, err := f.store.ListActiveProducts(f.ctx, db.ListActiveProductsParams{})
	must(t, err)
	if got := slugs(all); fmt.Sprint(got) != "[tee short]" {
		t.Fatalf("default order = %v", got)
	}
	first := all[0]
	if fmt.Sprint(first.AvailableSizes) != "[S XXL]" {
		t.Errorf("available sizes = %v, want [S XXL] (size order, in stock, active only)", first.AvailableSizes)
	}
	if fmt.Sprint(first.AvailableColorIDs) != fmt.Sprint([]int64{black.ID}) {
		t.Errorf("available colors = %v, want only black", first.AvailableColorIDs)
	}
	if fmt.Sprint(first.PreviewS3Keys) != "[products/tee/0.jpg products/tee/1.jpg]" {
		t.Errorf("preview keys = %v", first.PreviewS3Keys)
	}
	if first.CategorySlug != "tees" {
		t.Errorf("category slug = %q", first.CategorySlug)
	}
	if len(all[1].AvailableSizes) != 0 || len(all[1].PreviewS3Keys) != 0 {
		t.Errorf("product without variants/images should have empty arrays: %+v", all[1])
	}

	byCategory, err := f.store.ListActiveProducts(f.ctx, db.ListActiveProductsParams{CategorySlug: ptr("shorts")})
	must(t, err)
	if len(byCategory) != 1 || byCategory[0].ID != short.ID {
		t.Fatalf("category filter = %v", slugs(byCategory))
	}

	cheapFirst, err := f.store.ListActiveProducts(f.ctx, db.ListActiveProductsParams{OrderBy: "price_asc"})
	must(t, err)
	if got := slugs(cheapFirst); fmt.Sprint(got) != "[short tee]" {
		t.Fatalf("price_asc order = %v", got)
	}
}

func TestVariantStock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	p := f.product(f.category("tees", 0).ID, "tee", 1000, true, 0)
	v := f.variant(p.ID, f.color("Чёрный", "#0A0A0A").ID, "M", "ZH-T-BLK-M", 1, true)

	n, err := f.store.DecrementVariantStock(f.ctx, db.DecrementVariantStockParams{ID: v.ID, Qty: 2})
	must(t, err)
	if n != 0 {
		t.Fatal("decrement beyond stock must affect no rows")
	}
	n, err = f.store.DecrementVariantStock(f.ctx, db.DecrementVariantStockParams{ID: v.ID, Qty: 1})
	must(t, err)
	if n != 1 {
		t.Fatal("decrement within stock must affect one row")
	}
	must(t, f.store.IncrementVariantStock(f.ctx, db.IncrementVariantStockParams{ID: v.ID, Qty: 3}))

	rows, err := f.store.ListVariantsByProduct(f.ctx, p.ID)
	must(t, err)
	if rows[0].ProductVariant.Stock != 3 || rows[0].ColorNameRu != "Чёрный" {
		t.Fatalf("variant = %+v", rows[0])
	}
}

// Two checkouts for the last item: the second one must wait for the first
// transaction and then see the already decremented stock.
func TestLockVariantsForOrderSerializesCheckouts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	pool, store := f.pool, f.store
	p := f.product(f.category("tees", 0).ID, "tee", 1000, true, 0)
	v := f.variant(p.ID, f.color("Чёрный", "#0A0A0A").ID, "M", "ZH-T-BLK-M", 1, true)
	ctx := t.Context()
	ids := []int64{v.ID}

	tx1, err := pool.Begin(ctx)
	must(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after commit
	q1 := store.WithTx(tx1)
	locked, err := q1.LockVariantsForOrder(ctx, ids)
	must(t, err)
	if len(locked) != 1 || locked[0].Stock != 1 || locked[0].Price != 1000 {
		t.Fatalf("locked = %+v", locked)
	}
	_, err = q1.DecrementVariantStock(ctx, db.DecrementVariantStockParams{ID: v.ID, Qty: 1})
	must(t, err)

	type result struct {
		stock int32
		err   error
	}
	second := make(chan result, 1)
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			second <- result{err: err}
			return
		}
		defer tx2.Rollback(ctx) //nolint:errcheck // read-only
		rows, err := store.WithTx(tx2).LockVariantsForOrder(ctx, ids)
		if err != nil {
			second <- result{err: err}
			return
		}
		second <- result{stock: rows[0].Stock}
	}()

	waitForLockWait(t, pool)
	select {
	case r := <-second:
		t.Fatalf("second checkout did not wait for the lock: %+v", r)
	default:
	}
	must(t, tx1.Commit(ctx))

	r := <-second
	must(t, r.err)
	if r.stock != 0 {
		t.Fatalf("second checkout saw stock %d, want 0", r.stock)
	}
}

func waitForLockWait(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := pool.QueryRow(t.Context(),
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'",
		).Scan(&waiting)
		must(t, err)
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no backend is waiting on the row lock")
}

func TestOrders(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zone, err := f.store.UpsertDeliveryZone(f.ctx, db.UpsertDeliveryZoneParams{NameRu: "Бишкек", NameKy: "Бишкек", Price: 200, IsActive: true})
	must(t, err)

	first, err := f.store.CreateOrder(f.ctx, pickupOrder("key-00000001"))
	must(t, err)
	if !regexp.MustCompile(`^ZH-\d{6}$`).MatchString(first.Number) {
		t.Fatalf("order number = %q", first.Number)
	}
	if first.Status != db.OrderStatusNew || first.PaymentStatus != db.PaymentStatusUnpaid {
		t.Fatalf("defaults: status=%s payment=%s", first.Status, first.PaymentStatus)
	}
	second, err := f.store.CreateOrder(f.ctx, pickupOrder("key-00000002"))
	must(t, err)
	var n1, n2 int
	_, _ = fmt.Sscanf(first.Number, "ZH-%d", &n1)
	_, _ = fmt.Sscanf(second.Number, "ZH-%d", &n2)
	if n2 != n1+1 {
		t.Fatalf("numbers not sequential: %s, %s", first.Number, second.Number)
	}

	t.Run("idempotency key is unique", func(t *testing.T) {
		_, err := f.store.CreateOrder(f.ctx, pickupOrder("key-00000001"))
		wantUnique(t, err, "orders_idempotency_key_key")
		got, err := f.store.GetOrderByIdempotencyKey(f.ctx, "key-00000001")
		must(t, err)
		if got.ID != first.ID {
			t.Fatalf("lookup by key returned order %d", got.ID)
		}
	})
	t.Run("phone format", func(t *testing.T) {
		o := pickupOrder("key-phone-1")
		o.Phone = "+99670012345"
		_, err := f.store.CreateOrder(f.ctx, o)
		wantCheck(t, err, "orders_phone_check")
	})
	t.Run("courier needs zone and address", func(t *testing.T) {
		o := pickupOrder("key-courier-1")
		o.DeliveryMethod = db.DeliveryMethodCourier
		_, err := f.store.CreateOrder(f.ctx, o)
		wantCheck(t, err, "orders_delivery_chk")

		o.DeliveryZoneID, o.Address, o.DeliveryPrice, o.Total = &zone.ID, ptr("ул. Токтогула, 1"), 200, 1200
		_, err = f.store.CreateOrder(f.ctx, o)
		must(t, err)
	})
	t.Run("pickup has no delivery price", func(t *testing.T) {
		o := pickupOrder("key-pickup-1")
		o.DeliveryPrice, o.Total = 200, 1200
		_, err := f.store.CreateOrder(f.ctx, o)
		wantCheck(t, err, "orders_delivery_chk")
	})
	t.Run("total must add up", func(t *testing.T) {
		o := pickupOrder("key-total-1")
		o.Discount, o.Total = 100, 1000
		_, err := f.store.CreateOrder(f.ctx, o)
		wantCheck(t, err, "orders_total_chk")
	})
	t.Run("discount cannot exceed subtotal", func(t *testing.T) {
		o := pickupOrder("key-discount-1")
		o.Discount, o.Total = 1500, -500
		_, err := f.store.CreateOrder(f.ctx, o)
		wantCheck(t, err, "orders_discount_chk")
	})
	t.Run("number grows past six digits", func(t *testing.T) {
		_, err := f.pool.Exec(f.ctx, "SELECT setval('order_number_seq', 999999)")
		must(t, err)
		o, err := f.store.CreateOrder(f.ctx, pickupOrder("key-big-number"))
		must(t, err)
		if o.Number != "ZH-1000000" {
			t.Fatalf("number = %q", o.Number)
		}
	})
	t.Run("items and public lookup", func(t *testing.T) {
		_, err := f.store.CreateOrderItem(f.ctx, db.CreateOrderItemParams{
			OrderID: first.ID, TitleRu: "Футболка", TitleKy: "Футболка", ColorNameRu: "Чёрный", ColorNameKy: "Кара",
			Size: "M", Sku: "ZH-T-BLK-M", Price: 1000, Qty: 1,
		})
		must(t, err)
		items, err := f.store.ListOrderItems(f.ctx, first.ID)
		must(t, err)
		if len(items) != 1 {
			t.Fatalf("items = %d", len(items))
		}
		if _, err := f.store.GetOrderByNumberAndPhone(f.ctx, db.GetOrderByNumberAndPhoneParams{Number: first.Number, Phone: first.Phone}); err != nil {
			t.Fatal(err)
		}
		_, err = f.store.GetOrderByNumberAndPhone(f.ctx, db.GetOrderByNumberAndPhoneParams{Number: first.Number, Phone: "+996555000000"})
		if !repo.IsNotFound(err) {
			t.Fatalf("wrong phone must not find the order, got %v", err)
		}
	})
	t.Run("status compare-and-set and history", func(t *testing.T) {
		_, err := f.store.CreateOrderStatusHistory(f.ctx, db.CreateOrderStatusHistoryParams{OrderID: second.ID, ToStatus: db.OrderStatusNew})
		must(t, err)
		_, err = f.store.UpdateOrderStatus(f.ctx, db.UpdateOrderStatusParams{ID: second.ID, FromStatus: db.OrderStatusNew, ToStatus: db.OrderStatusConfirmed})
		must(t, err)
		from := db.OrderStatusNew
		_, err = f.store.CreateOrderStatusHistory(f.ctx, db.CreateOrderStatusHistoryParams{OrderID: second.ID, FromStatus: &from, ToStatus: db.OrderStatusConfirmed})
		must(t, err)

		_, err = f.store.UpdateOrderStatus(f.ctx, db.UpdateOrderStatusParams{ID: second.ID, FromStatus: db.OrderStatusNew, ToStatus: db.OrderStatusCancelled})
		if !repo.IsNotFound(err) {
			t.Fatalf("stale from_status must not update, got %v", err)
		}
		confirmed := db.OrderStatusConfirmed
		_, err = f.store.CreateOrderStatusHistory(f.ctx, db.CreateOrderStatusHistoryParams{OrderID: second.ID, FromStatus: &confirmed, ToStatus: db.OrderStatusConfirmed})
		wantCheck(t, err, "order_status_history_change_chk")

		history, err := f.store.ListOrderStatusHistory(f.ctx, second.ID)
		must(t, err)
		if len(history) != 2 || history[0].FromStatus != nil || *history[1].FromStatus != db.OrderStatusNew {
			t.Fatalf("history = %+v", history)
		}
	})
}

func TestPromoCodes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	promo, err := f.store.CreatePromoCode(f.ctx, db.CreatePromoCodeParams{
		Code: "Welcome10", Type: db.PromoTypePercent, Value: 10, MaxUses: ptr(int32(1)), IsActive: true,
	})
	must(t, err)

	for _, code := range []string{"WELCOME10", "welcome10"} {
		got, err := f.store.GetPromoCodeByCode(f.ctx, code)
		must(t, err)
		if got.ID != promo.ID {
			t.Fatalf("lookup %q returned %d", code, got.ID)
		}
	}

	_, err = f.store.CreatePromoCode(f.ctx, db.CreatePromoCodeParams{Code: "WELCOME10", Type: db.PromoTypeFixed, Value: 100, IsActive: true})
	wantUnique(t, err, "promo_codes_code_key")

	_, err = f.store.CreatePromoCode(f.ctx, db.CreatePromoCodeParams{Code: "HALFPLUS", Type: db.PromoTypePercent, Value: 150, IsActive: true})
	wantCheck(t, err, "promo_codes_percent_range_chk")

	now := time.Now()
	_, err = f.store.CreatePromoCode(f.ctx, db.CreatePromoCodeParams{
		Code: "BACKWARDS", Type: db.PromoTypeFixed, Value: 100, ValidFrom: &now, ValidTo: ptr(now.Add(-time.Hour)), IsActive: true,
	})
	wantCheck(t, err, "promo_codes_validity_chk")

	for i, want := range []int64{1, 0} {
		n, err := f.store.IncrementPromoCodeUsage(f.ctx, promo.ID)
		must(t, err)
		if n != want {
			t.Fatalf("use #%d affected %d rows, want %d", i+1, n, want)
		}
	}
}

func TestSettingsZonesAdmins(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	for _, v := range []string{`{"title_ru":"A"}`, `{"title_ru":"B"}`} {
		_, err := f.store.UpsertSetting(f.ctx, db.UpsertSettingParams{Key: "hero", Value: json.RawMessage(v)})
		must(t, err)
	}
	_, err := f.store.UpsertSetting(f.ctx, db.UpsertSettingParams{Key: "pickup", Value: json.RawMessage(`{}`)})
	must(t, err)
	settings, err := f.store.ListSettings(f.ctx, []string{"hero", "missing"})
	must(t, err)
	var hero struct {
		TitleRu string `json:"title_ru"`
	}
	must(t, json.Unmarshal(settings[0].Value, &hero))
	if len(settings) != 1 || hero.TitleRu != "B" {
		t.Fatalf("settings = %+v", settings)
	}

	for i, z := range []struct {
		name   string
		active bool
	}{{"Пригород", true}, {"Бишкек", true}, {"Архив", false}} {
		_, err := f.store.UpsertDeliveryZone(f.ctx, db.UpsertDeliveryZoneParams{NameRu: z.name, NameKy: z.name, Price: 100, IsActive: z.active, Sort: int32(10 - i)})
		must(t, err)
	}
	zones, err := f.store.ListActiveDeliveryZones(f.ctx)
	must(t, err)
	if len(zones) != 2 || zones[0].NameRu != "Бишкек" {
		t.Fatalf("zones = %+v", zones)
	}

	admin, err := f.store.UpsertAdmin(f.ctx, db.UpsertAdminParams{TelegramID: 1001, Name: "Owner", Role: db.AdminRoleOwner, IsActive: true})
	must(t, err)
	got, err := f.store.GetActiveAdminByTelegramID(f.ctx, 1001)
	must(t, err)
	if got.ID != admin.ID || got.Role != db.AdminRoleOwner {
		t.Fatalf("admin = %+v", got)
	}
	_, err = f.store.UpsertAdmin(f.ctx, db.UpsertAdminParams{TelegramID: 1001, Name: "Owner", Role: db.AdminRoleOwner, IsActive: false})
	must(t, err)
	if _, err := f.store.GetActiveAdminByTelegramID(f.ctx, 1001); !repo.IsNotFound(err) {
		t.Fatalf("inactive admin must not be returned, got %v", err)
	}
}

func slugs(rows []db.ListActiveProductsRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Slug
	}
	return out
}

func ptr[T any](v T) *T { return &v }
