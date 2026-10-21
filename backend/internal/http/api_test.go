package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/auth"
	apihttp "github.com/Baiel1922/zhim-back/backend/internal/http"
	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/seed"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
	"github.com/Baiel1922/zhim-back/backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

const imgBase = "https://img.test"

type api struct {
	t       *testing.T
	srv     *httptest.Server
	store   *repo.Store
	objects *memObjects
}

const botToken = "123456:http-test-token"

// memObjects is an in-memory object store.
type memObjects struct {
	mu   sync.Mutex
	keys map[string]int
}

func (m *memObjects) Put(_ context.Context, key, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key] = len(data)
	return nil
}

func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, key)
	return nil
}

// newAPI serves the full router over a seeded database.
func newAPI(t *testing.T, checkoutLimit int) *api {
	t.Helper()
	store := repo.NewStore(pgtest.NewPool(t))
	if _, err := seed.Run(t.Context(), store, seed.Options{}); err != nil {
		t.Fatal(err)
	}
	images, err := imgproxy.NewSigner(imgBase, "zhim", "aa", "bb")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.NewTokens(strings.Repeat("t", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	objects := &memObjects{keys: map[string]int{}}
	srv := httptest.NewServer(apihttp.NewRouter(apihttp.Deps{
		Log:               log,
		Catalog:           service.NewCatalog(store),
		Checkout:          service.NewCheckout(store, nil),
		Admin:             service.NewAdmin(store, objects, nil, time.UTC, log),
		AdminAuth:         service.NewAdminAuth(store, tokens, botToken),
		Images:            images,
		CheckoutRateLimit: checkoutLimit,
	}))
	t.Cleanup(srv.Close)
	return &api{t: t, srv: srv, store: store, objects: objects}
}

// reply is what tests need from a response; the body is decoded and closed in call.
type reply struct {
	StatusCode int
	Header     http.Header
}

// call sends a request and decodes the JSON response into out (if non-nil).
func (a *api) call(method, path string, body any, headers map[string]string, out any) reply {
	a.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.srv.URL+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.srv.Client().Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		a.t.Fatalf("%s %s: content-type %q", method, path, ct)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			a.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return reply{StatusCode: resp.StatusCode, Header: resp.Header}
}

func (a *api) wantError(resp reply, body oapi.Error, status int, code oapi.ErrorCode) {
	a.t.Helper()
	if resp.StatusCode != status || body.Code != code {
		a.t.Fatalf("got %d %s (%s), want %d %s", resp.StatusCode, body.Code, body.Message, status, code)
	}
}

func TestCatalogEndpoints(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	core, err := a.store.GetProductBySlug(t.Context(), "termofutbolka-core")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.CreateProductImage(t.Context(), db.CreateProductImageParams{
		ProductID: core.ID, S3Key: "products/core/1.jpg", AltKy: "Кара футболка",
	}); err != nil {
		t.Fatal(err)
	}

	var cats oapi.CategoryList
	a.call("GET", "/v1/categories?lang=ky", nil, nil, &cats)
	if len(cats.Items) != 3 || cats.Items[0].Name != "Термофутболкалар" {
		t.Fatalf("categories = %+v", cats.Items)
	}

	var list oapi.ProductList
	a.call("GET", "/v1/products", nil, nil, &list)
	if len(list.Items) != 6 {
		t.Fatalf("products = %d", len(list.Items))
	}
	card := list.Items[0]
	if card.Slug != "termofutbolka-core" || card.Title != "Термофутболка Core" || !card.InStock ||
		strings.Join(card.Sizes, ",") != "S,M,L,XL,XXL" || len(card.Colors) != 2 || len(card.Images) != 1 {
		t.Fatalf("card = %+v", card)
	}
	img := card.Images[0]
	if !strings.HasPrefix(img.Card.Src, imgBase+"/") || !strings.Contains(img.Card.Src, "/rs:fill:540:675:0/") ||
		strings.Count(img.Card.Srcset, "w,")+1 != len(imgproxy.Card.Widths) || img.Alt != card.Title {
		t.Fatalf("image = %+v", img)
	}

	var sale oapi.ProductList
	a.call("GET", "/v1/products?category=shorty&sort=price_asc", nil, nil, &sale)
	if len(sale.Items) != 2 || sale.Items[0].Slug != "shorty-run" || sale.Items[0].OldPrice == nil {
		t.Fatalf("shorts by price = %+v", sale.Items)
	}

	var product oapi.Product
	a.call("GET", "/v1/products/termofutbolka-core?lang=ky", nil, nil, &product)
	if product.Title != "Core термофутболкасы" || product.Category.Slug != "termofutbolki" || len(product.Variants) != 10 ||
		strings.Join(product.Sizes, ",") != "S,M,L,XL,XXL" || len(product.Colors) != 2 || product.Colors[0].Name != "Кара" {
		t.Fatalf("product = %+v", product)
	}
	if product.SizeChart == nil || product.SizeChart.Columns[0].Name != "Көкүрөк айланасы" || len(product.SizeChart.Fit) != 5 {
		t.Fatalf("size chart = %+v", product.SizeChart)
	}
	if len(product.Images) != 1 || product.Images[0].Alt != "Кара футболка" {
		t.Fatalf("images = %+v", product.Images)
	}

	var settings oapi.PublicSettings
	a.call("GET", "/v1/settings/public?lang=ky", nil, nil, &settings)
	if len(settings.DeliveryZones) != 2 || settings.DeliveryZones[1].Name != "Бишкектин айланасы" ||
		!strings.HasPrefix(settings.Pickup.Address, "Бишкек ш.") || len(settings.Contacts.Phones) != 1 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestCatalogErrors(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	var e oapi.Error

	a.wantError(a.call("GET", "/v1/products?category=nope", nil, nil, &e), e, 404, oapi.ErrorCodeCategoryNotFound)
	a.wantError(a.call("GET", "/v1/products/nope", nil, nil, &e), e, 404, oapi.ErrorCodeProductNotFound)
	a.wantError(a.call("GET", "/v1/products?sort=random", nil, nil, &e), e, 400, oapi.ErrorCodeValidationError)
	e = oapi.Error{}
	a.wantError(a.call("GET", "/v1/categories?lang=en", nil, nil, &e), e, 400, oapi.ErrorCodeValidationError)
	if len(e.Fields) != 1 || e.Fields[0].Field != "lang" {
		t.Fatalf("fields = %+v", e.Fields)
	}
	a.wantError(a.call("GET", "/v1/nothing-here", nil, nil, &e), e, 404, oapi.ErrorCodeNotFound)
	a.wantError(a.call("DELETE", "/v1/categories", nil, nil, &e), e, 405, oapi.ErrorCodeMethodNotAllowed)
}

func TestCheckoutFlow(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	var core, soldOut int64
	for sku, id := range map[string]*int64{"ZH-TFC-BLK-M": &core, "ZH-TFP-RED-XXL": &soldOut} {
		rows, err := a.store.ListVariantsByProduct(t.Context(), mustProductID(t, a.store, sku))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ProductVariant.Sku == sku {
				*id = r.ProductVariant.ID
			}
		}
	}
	key := map[string]string{"Idempotency-Key": "4f8e7a2c-0001"}
	order := map[string]any{
		"customer_name":   "Азамат",
		"phone":           "0700123456",
		"delivery_method": "pickup",
		"payment_method":  "transfer",
		"items":           []map[string]any{{"variant_id": core, "qty": 2}},
		"attribution":     map[string]any{"utm_source": "tiktok", "ttclid": "abc"},
	}

	var e oapi.Error
	a.wantError(a.call("POST", "/v1/orders", order, nil, &e), e, 400, oapi.ErrorCodeValidationError)
	if len(e.Fields) != 1 || e.Fields[0].Field != "Idempotency-Key" || e.Fields[0].Code != oapi.FieldErrorCodeRequired {
		t.Fatalf("missing key fields = %+v", e.Fields)
	}
	a.wantError(a.call("POST", "/v1/orders", "{not json", key, &e), e, 400, oapi.ErrorCodeValidationError)

	var created oapi.Order
	resp := a.call("POST", "/v1/orders?lang=ky", order, key, &created)
	if resp.StatusCode != http.StatusCreated || created.Total != 3980 || created.Phone != "+996700123456" ||
		created.Pickup == nil || created.Items[0].Title != "Core термофутболкасы" || created.Items[0].LineTotal != 3980 ||
		created.Items[0].ProductSlug == nil {
		t.Fatalf("created = %d %+v", resp.StatusCode, created)
	}

	var replay oapi.Order
	if resp := a.call("POST", "/v1/orders", order, key, &replay); resp.StatusCode != 201 || replay.Number != created.Number {
		t.Fatalf("replay = %d %s", resp.StatusCode, replay.Number)
	}

	var fetched oapi.Order
	a.call("GET", "/v1/orders/"+created.Number+"?phone=%2B996700123456", nil, nil, &fetched)
	if fetched.Number != created.Number || fetched.Status != oapi.OrderStatusNew || fetched.PaymentStatus != oapi.PaymentStatusUnpaid {
		t.Fatalf("fetched = %+v", fetched)
	}
	a.wantError(a.call("GET", "/v1/orders/"+created.Number+"?phone=0555000111", nil, nil, &e), e, 404, oapi.ErrorCodeOrderNotFound)

	order["items"] = []map[string]any{{"variant_id": soldOut, "qty": 1}}
	e = oapi.Error{}
	a.wantError(a.call("POST", "/v1/orders", order, map[string]string{"Idempotency-Key": "4f8e7a2c-0002"}, &e), e, 409, oapi.ErrorCodeOutOfStock)
	if len(e.Items) != 1 || e.Items[0].VariantID != soldOut || e.Items[0].Available != 0 {
		t.Fatalf("stock issues = %+v", e.Items)
	}

	a.wantError(a.call("POST", "/v1/promo/validate", map[string]any{"code": "NOPE", "subtotal": 1000}, nil, &e), e, 422, oapi.ErrorCodePromoNotFound)
}

func TestCheckoutRateLimit(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 2)
	body := map[string]any{"code": "NOPE", "subtotal": 1000}
	var e oapi.Error
	for range 2 {
		a.wantError(a.call("POST", "/v1/promo/validate", body, nil, &e), e, 422, oapi.ErrorCodePromoNotFound)
	}
	resp := a.call("POST", "/v1/promo/validate", body, nil, &e)
	a.wantError(resp, e, 429, oapi.ErrorCodeRateLimited)
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	// Catalog reads are never limited (the storefront server fetches them from one IP).
	for range 5 {
		if resp := a.call("GET", "/v1/categories", nil, nil, nil); resp.StatusCode != 200 {
			t.Fatalf("catalog read limited: %d", resp.StatusCode)
		}
	}
}

func mustProductID(t *testing.T, store *repo.Store, sku string) int64 {
	t.Helper()
	slug := map[string]string{"ZH-TFC-BLK-M": "termofutbolka-core", "ZH-TFP-RED-XXL": "termofutbolka-pro-compression"}[sku]
	p, err := store.GetProductBySlug(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}
