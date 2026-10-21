package service_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/auth"
	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

const testBotToken = "123456:service-test-token"

// memObjects is an in-memory ObjectStore.
type memObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memObjects) Put(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memObjects) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tagRecorder records revalidation tags.
type tagRecorder struct {
	mu   sync.Mutex
	tags map[string]bool
}

func (r *tagRecorder) Notify(tags ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range tags {
		r.tags[t] = true
	}
}

func (r *tagRecorder) has(tag string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tags[tag]
}

type adminEnv struct {
	*env
	admin   *service.Admin
	objects *memObjects
	tags    *tagRecorder
	owner   db.Admin
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := newEnv(t)
	objects := &memObjects{objects: map[string][]byte{}}
	tags := &tagRecorder{tags: map[string]bool{}}
	loc, err := time.LoadLocation("Asia/Bishkek")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := e.store.UpsertAdmin(e.ctx, db.UpsertAdminParams{TelegramID: 424242, Name: "Owner", Role: db.AdminRoleOwner, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	return &adminEnv{
		env:     e,
		admin:   service.NewAdmin(e.store, objects, tags, loc, slog.New(slog.NewTextHandler(io.Discard, nil))),
		objects: objects,
		tags:    tags,
		owner:   owner,
	}
}

// signInitData builds Telegram initData for a user (see internal/telegram tests).
func signInitData(userID int64, authDate time.Time, token string) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(authDate.Unix(), 10),
		"query_id":  "AAH",
		"user":      fmt.Sprintf(`{"id":%d,"first_name":"Test"}`, userID),
	}
	keys := []string{"auth_date", "query_id", "user"}
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + fields[k]
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}

func TestAdminLogin(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	tokens, err := auth.NewTokens(strings.Repeat("k", 32), 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	authn := service.NewAdminAuth(e.store, tokens, testBotToken)

	session, err := authn.Login(e.ctx, signInitData(424242, time.Now().Add(-time.Hour), testBotToken))
	if err != nil {
		t.Fatal(err)
	}
	if session.Admin.ID != e.owner.ID || time.Until(session.ExpiresAt) < 11*time.Hour {
		t.Fatalf("session = %+v", session)
	}
	got, err := authn.Authenticate(e.ctx, session.Token)
	if err != nil || got.ID != e.owner.ID || got.Role != db.AdminRoleOwner {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}

	_, err = authn.Login(e.ctx, signInitData(777, time.Now(), testBotToken))
	wantCode(t, err, service.CodeNotAdmin)
	_, err = authn.Login(e.ctx, signInitData(424242, time.Now().Add(-25*time.Hour), testBotToken))
	wantCode(t, err, service.CodeInitDataExpired)
	_, err = authn.Login(e.ctx, signInitData(424242, time.Now(), "999:forged"))
	wantCode(t, err, service.CodeInitDataInvalid)
	_, err = authn.Authenticate(e.ctx, session.Token+"x")
	wantCode(t, err, service.CodeUnauthorized)

	// Deactivation revokes tokens that were already issued.
	if _, err := e.store.UpsertAdmin(e.ctx, db.UpsertAdminParams{TelegramID: 424242, Name: "Owner", Role: db.AdminRoleOwner, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	_, err = authn.Authenticate(e.ctx, session.Token)
	wantCode(t, err, service.CodeUnauthorized)
	_, err = authn.Login(e.ctx, signInitData(424242, time.Now(), testBotToken))
	wantCode(t, err, service.CodeNotAdmin)
}

func TestOrderStatusFlowAndCancelRestock(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M") // 9
	e.promo("ONCE", db.PromoTypeFixed, 300, ptr(int32(1)))
	in := pickupInput("key-admin-0001", service.ItemInput{VariantID: core, Qty: 3})
	in.PromoCode = "ONCE"
	created, err := e.checkout.CreateOrder(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	id := created.Order.ID
	if e.stock("ZH-TFC-BLK-M") != 6 {
		t.Fatal("order did not take stock")
	}

	_, err = e.admin.SetOrderStatus(e.ctx, &e.owner.ID, id, db.OrderStatusShipped)
	wantCode(t, err, service.CodeInvalidStatusTransition)

	o, err := e.admin.SetOrderStatus(e.ctx, &e.owner.ID, id, db.OrderStatusConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	if o.Order.Status != db.OrderStatusConfirmed || fmt.Sprint(o.Allowed) != "[assembling ready_for_pickup cancelled]" {
		t.Fatalf("after confirm: %s, allowed %v", o.Order.Status, o.Allowed)
	}
	last := o.History[len(o.History)-1]
	if len(o.History) != 2 || last.AdminName == nil || *last.AdminName != "Owner" || *last.FromStatus != db.OrderStatusNew {
		t.Fatalf("history = %+v", o.History)
	}

	o, err = e.admin.SetOrderStatus(e.ctx, &e.owner.ID, id, db.OrderStatusCancelled)
	if err != nil {
		t.Fatal(err)
	}
	if e.stock("ZH-TFC-BLK-M") != 9 {
		t.Errorf("cancel did not return stock: %d", e.stock("ZH-TFC-BLK-M"))
	}
	if n := e.count("SELECT used_count FROM promo_codes WHERE code = 'ONCE'"); n != 0 {
		t.Errorf("cancel did not free the promo use: %d", n)
	}
	if len(o.Allowed) != 0 || !e.tags.has("product:termofutbolka-core") {
		t.Errorf("allowed = %v, product revalidated = %v", o.Allowed, e.tags.has("product:termofutbolka-core"))
	}
	_, err = e.admin.SetOrderStatus(e.ctx, &e.owner.ID, id, db.OrderStatusCancelled)
	wantCode(t, err, service.CodeInvalidStatusTransition)
	_, err = e.admin.SetOrderStatus(e.ctx, nil, 999999, db.OrderStatusConfirmed)
	wantCode(t, err, service.CodeNotFound)
}

func TestOrderPaymentAndComment(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	created, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-pay-000001", service.ItemInput{VariantID: core, Qty: 1}))
	if err != nil {
		t.Fatal(err)
	}
	o, err := e.admin.SetOrderPayment(e.ctx, created.Order.ID, db.PaymentStatusPaid)
	if err != nil || o.Order.PaymentStatus != db.PaymentStatusPaid {
		t.Fatalf("payment = %+v, %v", o, err)
	}
	o, err = e.admin.SetOrderComment(e.ctx, created.Order.ID, "  перезвонить после 18:00 ")
	if err != nil || o.Order.InternalComment != "перезвонить после 18:00" {
		t.Fatalf("comment = %q, %v", o.Order.InternalComment, err)
	}
	_, err = e.admin.SetOrderComment(e.ctx, created.Order.ID, strings.Repeat("я", 2001))
	wantCode(t, err, service.CodeValidation)
}

func TestListOrdersFilters(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	var numbers []string
	for i, phone := range []string{"0700111222", "0555333444", "0700999888"} {
		in := pickupInput(fmt.Sprintf("key-list-%06d", i), service.ItemInput{VariantID: core, Qty: 1})
		in.Phone = phone
		d, err := e.checkout.CreateOrder(e.ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		numbers = append(numbers, d.Order.Number)
	}
	first, _ := e.store.GetOrderByNumber(e.ctx, numbers[0])
	if _, err := e.admin.SetOrderStatus(e.ctx, nil, first.ID, db.OrderStatusConfirmed); err != nil {
		t.Fatal(err)
	}

	all, err := e.admin.ListOrders(e.ctx, service.OrderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || all.Orders[0].Order.Number != numbers[2] || all.StatusCounts[db.OrderStatusNew] != 2 || all.StatusCounts[db.OrderStatusConfirmed] != 1 {
		t.Fatalf("all = total %d first %s counts %v", all.Total, all.Orders[0].Order.Number, all.StatusCounts)
	}

	newOnly, err := e.admin.ListOrders(e.ctx, service.OrderFilter{Status: ptr(db.OrderStatusNew)})
	if err != nil || newOnly.Total != 2 || len(newOnly.Orders) != 2 || newOnly.StatusCounts[db.OrderStatusConfirmed] != 1 {
		t.Fatalf("status filter = %+v, %v", newOnly, err)
	}

	byPhone, err := e.admin.ListOrders(e.ctx, service.OrderFilter{Query: "999 888"})
	if err != nil || byPhone.Total != 1 || byPhone.Orders[0].Order.Number != numbers[2] {
		t.Fatalf("phone search = %+v, %v", byPhone, err)
	}
	byNumber, err := e.admin.ListOrders(e.ctx, service.OrderFilter{Query: strings.ToLower(numbers[1])})
	if err != nil || byNumber.Total != 1 {
		t.Fatalf("number search = %+v, %v", byNumber, err)
	}
	wildcard, err := e.admin.ListOrders(e.ctx, service.OrderFilter{Query: "%"})
	if err != nil || wildcard.Total != 0 {
		t.Fatalf("LIKE wildcard must match literally: %+v, %v", wildcard, err)
	}

	page2, err := e.admin.ListOrders(e.ctx, service.OrderFilter{Page: 2, PerPage: 2})
	if err != nil || len(page2.Orders) != 1 || page2.Total != 3 {
		t.Fatalf("page 2 = %+v, %v", page2, err)
	}
	tomorrow := time.Now().AddDate(0, 0, 1)
	future, err := e.admin.ListOrders(e.ctx, service.OrderFilter{From: &tomorrow})
	if err != nil || future.Total != 0 {
		t.Fatalf("date filter = %+v, %v", future, err)
	}
	_, err = e.admin.ListOrders(e.ctx, service.OrderFilter{PerPage: 500})
	wantCode(t, err, service.CodeValidation)
}

func TestDashboard(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	core, _ := e.variant("ZH-TFC-BLK-M")
	pro, _ := e.variant("ZH-TFP-BLK-M")
	a, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-dash-00001", service.ItemInput{VariantID: core, Qty: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-dash-00002", service.ItemInput{VariantID: pro, Qty: 1})); err != nil {
		t.Fatal(err)
	}
	cancelled, err := e.checkout.CreateOrder(e.ctx, pickupInput("key-dash-00003", service.ItemInput{VariantID: pro, Qty: 5}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.SetOrderStatus(e.ctx, nil, cancelled.Order.ID, db.OrderStatusCancelled); err != nil {
		t.Fatal(err)
	}

	d, err := e.admin.Dashboard(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := service.PeriodStats{Orders: 2, Revenue: int64(a.Order.Total) + 2490}
	if d.Today != want || d.Last7Days != want || d.Last30Days != want {
		t.Fatalf("stats = %+v / %+v / %+v, want %+v", d.Today, d.Last7Days, d.Last30Days, want)
	}
	if len(d.TopProducts) != 2 || d.TopProducts[0].Title != "Термофутболка Core" || d.TopProducts[0].Qty != 2 {
		t.Fatalf("top = %+v", d.TopProducts)
	}
	for _, v := range d.LowStock {
		if v.Stock > 2 {
			t.Fatalf("low stock includes %+v", v)
		}
	}
	if len(d.LowStock) == 0 || d.LowStock[0].Stock != 0 {
		t.Fatalf("low stock = %+v", d.LowStock)
	}
}

func TestCatalogAdmin(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	cats, err := e.admin.ListCategories(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tees := cats[0]
	if tees.ProductCount != 2 {
		t.Fatalf("product count = %d", tees.ProductCount)
	}

	_, err = e.admin.CreateCategory(e.ctx, service.CategoryInput{Slug: "shorty", NameRu: "Ещё шорты", NameKy: "Шорттор"})
	wantCode(t, err, service.CodeAlreadyExists)
	err = e.admin.DeleteCategory(e.ctx, tees.ID)
	wantCode(t, err, service.CodeInUse)
	_, err = e.admin.CreateCategory(e.ctx, service.CategoryInput{Slug: "Bad Slug", NameRu: "", NameKy: "x"})
	if se := serviceError(t, err, service.CodeValidation); len(se.Fields) != 2 {
		t.Fatalf("fields = %+v", se.Fields)
	}

	p, err := e.admin.CreateProduct(e.ctx, service.ProductInput{
		Slug: "hoodie", CategoryID: tees.ID, TitleRu: "Худи", TitleKy: "Худи", Price: 3990, OldPrice: ptr(int32(4990)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !e.tags.has("product:hoodie") {
		t.Error("create did not revalidate the product page")
	}
	_, err = e.admin.CreateProduct(e.ctx, service.ProductInput{Slug: "hoodie", CategoryID: tees.ID, TitleRu: "X", TitleKy: "X", Price: 1})
	wantCode(t, err, service.CodeAlreadyExists)
	_, err = e.admin.CreateProduct(e.ctx, service.ProductInput{Slug: "x", CategoryID: 999999, TitleRu: "X", TitleKy: "X", Price: 1})
	if se := serviceError(t, err, service.CodeValidation); se.Fields[0].Field != "category_id" {
		t.Fatalf("fields = %+v", se.Fields)
	}

	colors, err := e.admin.ListColors(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	matrix := service.VariantMatrixInput{ColorIDs: []int64{colors[0].ID, colors[1].ID}, Sizes: []string{"m", "L", "M", " xl "}, Stock: 4}
	withVariants, err := e.admin.CreateVariants(e.ctx, p.Product.ID, matrix)
	if err != nil {
		t.Fatal(err)
	}
	if len(withVariants.Variants) != 6 || withVariants.Variants[0].Size != "M" || withVariants.Variants[0].Stock != 4 {
		t.Fatalf("variants = %+v", withVariants.Variants)
	}
	again, err := e.admin.CreateVariants(e.ctx, p.Product.ID, matrix)
	if err != nil || len(again.Variants) != 6 {
		t.Fatalf("matrix must be idempotent: %d, %v", len(again.Variants), err)
	}

	v := withVariants.Variants[0]
	if _, err := e.admin.SetVariantStock(e.ctx, v.ID, 12); err != nil {
		t.Fatal(err)
	}
	_, err = e.admin.SetVariantStock(e.ctx, v.ID, -1)
	wantCode(t, err, service.CodeValidation)
	_, err = e.admin.UpdateVariant(e.ctx, v.ID, "zh-tfc-blk-m", true)
	wantCode(t, err, service.CodeAlreadyExists)
	updated, err := e.admin.UpdateVariant(e.ctx, v.ID, "hood-blk-m", false)
	if err != nil || updated.Sku != "HOOD-BLK-M" || updated.IsActive {
		t.Fatalf("update variant = %+v, %v", updated, err)
	}

	err = e.admin.DeleteColor(e.ctx, colors[0].ID)
	wantCode(t, err, service.CodeInUse)
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 10))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImages(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	p, err := e.store.GetProductBySlug(e.ctx, "termofutbolka-core")
	if err != nil {
		t.Fatal(err)
	}
	colors, _ := e.admin.ListColors(e.ctx)

	first, err := e.admin.UploadImage(e.ctx, p.ID, service.ImageUpload{Data: pngBytes(t), AltRu: "Спереди"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.admin.UploadImage(e.ctx, p.ID, service.ImageUpload{Data: pngBytes(t), ColorID: &colors[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sort != 0 || second.Sort != 1 || !strings.HasPrefix(first.S3Key, fmt.Sprintf("products/%d/", p.ID)) ||
		!strings.HasSuffix(first.S3Key, ".png") || len(e.objects.keys()) != 2 {
		t.Fatalf("uploads = %+v %+v, objects %v", first, second, e.objects.keys())
	}

	_, err = e.admin.UploadImage(e.ctx, p.ID, service.ImageUpload{Data: []byte("<svg onload=alert(1)>")})
	wantCode(t, err, service.CodeUnsupportedMediaType)
	_, err = e.admin.UploadImage(e.ctx, p.ID, service.ImageUpload{Data: make([]byte, service.MaxImageBytes+1)})
	wantCode(t, err, service.CodeFileTooLarge)
	_, err = e.admin.UploadImage(e.ctx, p.ID, service.ImageUpload{Data: pngBytes(t), ColorID: ptr(int64(999999))})
	wantCode(t, err, service.CodeValidation)
	if len(e.objects.keys()) != 2 {
		t.Fatalf("failed upload left an object: %v", e.objects.keys())
	}

	reordered, err := e.admin.ReorderImages(e.ctx, p.ID, []int64{second.ID, first.ID})
	if err != nil || reordered.Images[0].ID != second.ID {
		t.Fatalf("reorder = %+v, %v", reordered, err)
	}
	_, err = e.admin.ReorderImages(e.ctx, p.ID, []int64{second.ID})
	wantCode(t, err, service.CodeValidation)

	img, err := e.admin.UpdateImage(e.ctx, first.ID, nil, "Общий вид", "Жалпы көрүнүш")
	if err != nil || img.ColorID != nil || img.AltKy != "Жалпы көрүнүш" {
		t.Fatalf("update image = %+v, %v", img, err)
	}

	if err := e.admin.DeleteImage(e.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(e.objects.keys()) != fmt.Sprint([]string{second.S3Key}) {
		t.Fatalf("objects after delete = %v", e.objects.keys())
	}
	if err := e.admin.DeleteProduct(e.ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.objects.keys()) != 0 {
		t.Fatalf("product delete left objects: %v", e.objects.keys())
	}
}

func TestLastOwnerIsProtected(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)

	_, err := e.admin.UpdateAdmin(e.ctx, e.owner.ID, service.AdminInput{Name: "Owner", Role: db.AdminRoleManager, IsActive: true})
	wantCode(t, err, service.CodeLastOwner)
	_, err = e.admin.UpdateAdmin(e.ctx, e.owner.ID, service.AdminInput{Name: "Owner", Role: db.AdminRoleOwner, IsActive: false})
	wantCode(t, err, service.CodeLastOwner)
	wantCode(t, e.admin.DeleteAdmin(e.ctx, e.owner.ID), service.CodeLastOwner)

	manager, err := e.admin.CreateAdmin(e.ctx, service.AdminInput{TelegramID: 5555, Name: "Менеджер", Role: db.AdminRoleManager, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.admin.CreateAdmin(e.ctx, service.AdminInput{TelegramID: 5555, Name: "Дубль", Role: db.AdminRoleManager, IsActive: true})
	wantCode(t, err, service.CodeAlreadyExists)
	if err := e.admin.DeleteAdmin(e.ctx, manager.ID); err != nil {
		t.Fatal(err)
	}

	second, err := e.admin.CreateAdmin(e.ctx, service.AdminInput{TelegramID: 6666, Name: "Второй", Role: db.AdminRoleOwner, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.UpdateAdmin(e.ctx, e.owner.ID, service.AdminInput{Name: "Owner", Role: db.AdminRoleManager, IsActive: true}); err != nil {
		t.Fatalf("demote with another owner present: %v", err)
	}
	wantCode(t, e.admin.DeleteAdmin(e.ctx, second.ID), service.CodeLastOwner)
}

func TestSettingsAdmin(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	s, err := e.admin.GetSettings(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Contacts = domain.Contacts{Phones: []string{"0700 123 456"}, WhatsApp: "+996 555 000 111", Telegram: "@zhim_store", Instagram: "@zhim.kg"}
	s.Pickup.MapURL = "https://2gis.kg/bishkek/firm/1"
	saved, err := e.admin.UpdateSettings(e.ctx, *s)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Contacts.Phones[0] != "+996700123456" || saved.Contacts.WhatsApp != "+996555000111" || saved.Contacts.Telegram != "zhim_store" {
		t.Fatalf("contacts not normalized: %+v", saved.Contacts)
	}
	if !e.tags.has("settings") {
		t.Error("settings were not revalidated")
	}

	s.Contacts.Phones = []string{"12345"}
	s.Pickup.MapURL = "javascript:alert(1)"
	_, err = e.admin.UpdateSettings(e.ctx, *s)
	if se := serviceError(t, err, service.CodeValidation); len(se.Fields) != 2 {
		t.Fatalf("fields = %+v", se.Fields)
	}
}

func TestPromoAndZonesAdmin(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	p, err := e.admin.CreatePromoCode(e.ctx, service.PromoCodeInput{Code: "summer10", Type: db.PromoTypePercent, Value: 10, MaxUses: ptr(int32(5)), IsActive: true})
	if err != nil || p.Code != "SUMMER10" {
		t.Fatalf("promo = %+v, %v", p, err)
	}
	_, err = e.admin.CreatePromoCode(e.ctx, service.PromoCodeInput{Code: "Summer10", Type: db.PromoTypeFixed, Value: 100, IsActive: true})
	wantCode(t, err, service.CodeAlreadyExists)
	_, err = e.admin.CreatePromoCode(e.ctx, service.PromoCodeInput{Code: "HUGE", Type: db.PromoTypePercent, Value: 101, IsActive: true})
	wantCode(t, err, service.CodeValidation)

	if _, err := e.pool.Exec(e.ctx, "UPDATE promo_codes SET used_count = 3 WHERE id = $1", p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.admin.UpdatePromoCode(e.ctx, p.ID, service.PromoCodeInput{Code: "SUMMER10", Type: db.PromoTypePercent, Value: 10, MaxUses: ptr(int32(2)), IsActive: true})
	wantCode(t, err, service.CodeValidation)

	zoneID := e.zone("Бишкек")
	core, _ := e.variant("ZH-TFC-BLK-M")
	in := pickupInput("key-zone-del-01", service.ItemInput{VariantID: core, Qty: 1})
	in.DeliveryMethod, in.DeliveryZoneID, in.Address = db.DeliveryMethodCourier, &zoneID, "ул. Ленина, 1"
	if _, err := e.checkout.CreateOrder(e.ctx, in); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.admin.DeleteDeliveryZone(e.ctx, zoneID), service.CodeInUse)
	z, err := e.admin.UpdateDeliveryZone(e.ctx, zoneID, service.DeliveryZoneInput{NameRu: "Бишкек", NameKy: "Бишкек", Price: 250, IsActive: false})
	if err != nil || z.IsActive || z.FreeFrom != nil {
		t.Fatalf("zone = %+v, %v", z, err)
	}
}
