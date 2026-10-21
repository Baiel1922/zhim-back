package http_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

func initData(userID int64) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":"Test"}`, userID),
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte("auth_date=" + fields["auth_date"] + "\nuser=" + fields["user"]))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}

// login creates an admin with the role and returns an Authorization header.
func (a *api) login(telegramID int64, role db.AdminRole) map[string]string {
	a.t.Helper()
	if _, err := a.store.UpsertAdmin(a.t.Context(), db.UpsertAdminParams{TelegramID: telegramID, Name: string(role), Role: role, IsActive: true}); err != nil {
		a.t.Fatal(err)
	}
	var s oapi.AdminSession
	if resp := a.call("POST", "/v1/admin/auth/telegram", map[string]string{"init_data": initData(telegramID)}, nil, &s); resp.StatusCode != 200 {
		a.t.Fatalf("login status %d", resp.StatusCode)
	}
	if s.Admin.Role != oapi.AdminRole(role) || s.Token == "" {
		a.t.Fatalf("session = %+v", s)
	}
	return map[string]string{"Authorization": "Bearer " + s.Token}
}

func TestAdminAuthAndRoles(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	var e oapi.Error

	resp := a.call("GET", "/v1/admin/orders", nil, nil, &e)
	a.wantError(resp, e, 401, oapi.ErrorCodeUnauthorized)
	if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Fatal("missing WWW-Authenticate")
	}
	a.wantError(a.call("GET", "/v1/admin/orders", nil, map[string]string{"Authorization": "Bearer nope"}, &e), e, 401, oapi.ErrorCodeUnauthorized)
	a.wantError(a.call("POST", "/v1/admin/auth/telegram", map[string]string{"init_data": initData(31337)}, nil, &e), e, 403, oapi.ErrorCodeNotAdmin)
	a.wantError(a.call("POST", "/v1/admin/auth/telegram", map[string]string{"init_data": "user=%7B%7D&hash=00"}, nil, &e), e, 401, oapi.ErrorCodeInitDataInvalid)

	owner := a.login(1001, db.AdminRoleOwner)
	manager := a.login(1002, db.AdminRoleManager)

	var me oapi.Admin
	a.call("GET", "/v1/admin/me", nil, manager, &me)
	if me.TelegramID != 1002 || me.Role != oapi.AdminRoleManager {
		t.Fatalf("me = %+v", me)
	}

	// Managers: orders and stock only.
	var orders oapi.AdminOrderList
	if resp := a.call("GET", "/v1/admin/orders", nil, manager, &orders); resp.StatusCode != 200 || orders.PerPage != 20 {
		t.Fatalf("manager orders = %d %+v", resp.StatusCode, orders)
	}
	var products oapi.AdminProductList
	a.call("GET", "/v1/admin/products", nil, manager, &products)
	if len(products.Items) != 6 {
		t.Fatalf("products = %d", len(products.Items))
	}
	var product oapi.AdminProduct
	a.call("GET", fmt.Sprintf("/v1/admin/products/%d", products.Items[0].ID), nil, manager, &product)
	var variant oapi.AdminVariant
	if resp := a.call("PUT", fmt.Sprintf("/v1/admin/variants/%d/stock", product.Variants[0].ID), map[string]int{"stock": 42}, manager, &variant); resp.StatusCode != 200 || variant.Stock != 42 {
		t.Fatalf("manager stock = %d %+v", resp.StatusCode, variant)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/v1/admin/categories"},
		{"PUT", fmt.Sprintf("/v1/admin/products/%d", product.ID)},
		{"GET", "/v1/admin/settings"},
		{"GET", "/v1/admin/admins"},
		{"GET", "/v1/admin/dashboard"},
		{"DELETE", fmt.Sprintf("/v1/admin/variants/%d", product.Variants[0].ID)},
	} {
		e = oapi.Error{}
		a.wantError(a.call(c.method, c.path, map[string]any{}, manager, &e), e, 403, oapi.ErrorCodeForbidden)
	}

	// Owners: everything.
	var created oapi.AdminCategory
	if resp := a.call("POST", "/v1/admin/categories", map[string]any{"slug": "hoodies", "name_ru": "Худи", "name_ky": "Худилер", "sort": 40}, owner, &created); resp.StatusCode != 201 || created.Slug != "hoodies" {
		t.Fatalf("create category = %d %+v", resp.StatusCode, created)
	}
	a.wantError(a.call("POST", "/v1/admin/categories", map[string]any{"slug": "hoodies", "name_ru": "Х", "name_ky": "Х"}, owner, &e), e, 409, oapi.ErrorCodeAlreadyExists)
	if len(e.Fields) != 1 || e.Fields[0].Field != "slug" || e.Fields[0].Code != oapi.FieldErrorCodeTaken {
		t.Fatalf("fields = %+v", e.Fields)
	}
	var dash oapi.Dashboard
	if resp := a.call("GET", "/v1/admin/dashboard", nil, owner, &dash); resp.StatusCode != 200 {
		t.Fatalf("dashboard = %d", resp.StatusCode)
	}

	// Demoting the manager takes effect on their existing token.
	var admins oapi.AdminList
	a.call("GET", "/v1/admin/admins", nil, owner, &admins)
	for _, adm := range admins.Items {
		if adm.TelegramID == 1002 {
			a.call("PUT", fmt.Sprintf("/v1/admin/admins/%d", adm.ID), map[string]any{"name": "m", "role": "manager", "is_active": false}, owner, nil)
		}
	}
	a.wantError(a.call("GET", "/v1/admin/me", nil, manager, &e), e, 401, oapi.ErrorCodeUnauthorized)
}

func TestAdminOrderStatusOverHTTP(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	owner := a.login(2001, db.AdminRoleOwner)
	var list oapi.AdminProductList
	a.call("GET", "/v1/admin/products", nil, owner, &list)
	var product oapi.AdminProduct
	a.call("GET", fmt.Sprintf("/v1/admin/products/%d", list.Items[0].ID), nil, owner, &product)

	var order oapi.Order
	a.call("POST", "/v1/orders", map[string]any{
		"customer_name": "Айгерим", "phone": "0555123456", "delivery_method": "pickup", "payment_method": "cash",
		"items": []map[string]any{{"variant_id": product.Variants[0].ID, "qty": 1}},
	}, map[string]string{"Idempotency-Key": "admin-http-0001"}, &order)

	var orders oapi.AdminOrderList
	a.call("GET", "/v1/admin/orders?status=new&q=555", nil, owner, &orders)
	if orders.Total != 1 || orders.StatusCounts.New != 1 || orders.Items[0].Number != order.Number {
		t.Fatalf("orders = %+v", orders)
	}
	id := orders.Items[0].ID

	var e oapi.Error
	a.wantError(a.call("PUT", fmt.Sprintf("/v1/admin/orders/%d/status", id), map[string]string{"status": "completed"}, owner, &e), e, 409, oapi.ErrorCodeInvalidStatusTransition)
	var updated oapi.AdminOrder
	a.call("PUT", fmt.Sprintf("/v1/admin/orders/%d/status", id), map[string]string{"status": "confirmed"}, owner, &updated)
	if updated.Status != oapi.OrderStatusConfirmed || len(updated.History) != 2 || updated.History[1].AdminName == nil {
		t.Fatalf("updated = %+v", updated)
	}
	a.call("PUT", fmt.Sprintf("/v1/admin/orders/%d/payment", id), map[string]string{"payment_status": "paid"}, owner, &updated)
	a.call("PUT", fmt.Sprintf("/v1/admin/orders/%d/comment", id), map[string]string{"internal_comment": "оплатил переводом"}, owner, &updated)
	if updated.PaymentStatus != oapi.PaymentStatusPaid || updated.InternalComment != "оплатил переводом" {
		t.Fatalf("updated = %+v", updated)
	}
}

func multipartBody(t *testing.T, fields map[string]string, file []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		fw, err := w.CreateFormFile("file", "photo.jpg")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(file)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

// upload posts a multipart body and returns the status code and response body.
func (a *api) upload(productID int64, headers map[string]string, body *bytes.Buffer, contentType string) (int, []byte) {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), "POST", fmt.Sprintf("%s/v1/admin/products/%d/images", a.srv.URL, productID), body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.srv.Client().Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func TestAdminImageUploadOverHTTP(t *testing.T) {
	t.Parallel()
	a := newAPI(t, 0)
	owner := a.login(3001, db.AdminRoleOwner)
	var list oapi.AdminProductList
	a.call("GET", "/v1/admin/products", nil, owner, &list)
	productID := list.Items[0].ID

	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 16, 20)), nil); err != nil {
		t.Fatal(err)
	}
	body, ct := multipartBody(t, map[string]string{"alt_ru": "Фото", "alt_ky": "Сүрөт"}, jpg.Bytes())
	status, raw := a.upload(productID, owner, body, ct)
	if status != 201 || !strings.Contains(string(raw), `"alt_ky":"Сүрөт"`) || !strings.Contains(string(raw), imgBase) {
		t.Fatalf("upload = %d %s", status, raw)
	}
	if len(a.objects.keys) != 1 {
		t.Fatalf("objects = %v", a.objects.keys)
	}

	body, ct = multipartBody(t, nil, []byte("plain text, not an image"))
	if status, raw := a.upload(productID, owner, body, ct); status != 415 {
		t.Fatalf("text upload = %d %s", status, raw)
	}
	big := make([]byte, 17<<20)
	copy(big, jpg.Bytes())
	body, ct = multipartBody(t, nil, big)
	if status, raw := a.upload(productID, owner, body, ct); status != 413 {
		t.Fatalf("oversized upload = %d %s", status, raw)
	}
	body, ct = multipartBody(t, nil, jpg.Bytes())
	if status, _ := a.upload(productID, nil, body, ct); status != 401 {
		t.Fatalf("anonymous upload = %d", status)
	}

	var product oapi.AdminProduct
	a.call("GET", fmt.Sprintf("/v1/admin/products/%d", productID), nil, owner, &product)
	if len(product.Images) != 1 || product.Images[0].AltRu != "Фото" {
		t.Fatalf("images = %+v", product.Images)
	}
}
