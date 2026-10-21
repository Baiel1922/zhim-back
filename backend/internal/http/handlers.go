package http

import (
	"context"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// Handlers implements the generated strict server interface.
type Handlers struct {
	catalog  *service.Catalog
	checkout *service.Checkout
	images   *imgproxy.Signer
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// NewHandlers constructs the API handlers.
func NewHandlers(catalog *service.Catalog, checkout *service.Checkout, images *imgproxy.Signer) *Handlers {
	return &Handlers{catalog: catalog, checkout: checkout, images: images}
}

// presenter validates the ?lang parameter (absent = ru).
func (h *Handlers) presenter(lang *oapi.Lang) (presenter, error) {
	p := presenter{lang: oapi.LangRu, images: h.images}
	if lang != nil {
		if !lang.Valid() {
			return p, validationError("lang", service.FieldInvalid)
		}
		p.lang = *lang
	}
	return p, nil
}

// GetHealthz is a liveness probe.
func (h *Handlers) GetHealthz(_ context.Context, _ oapi.GetHealthzRequestObject) (oapi.GetHealthzResponseObject, error) {
	return oapi.GetHealthz200JSONResponse{Status: oapi.HealthStatusOk}, nil
}

// ListCategories returns categories in display order.
func (h *Handlers) ListCategories(ctx context.Context, req oapi.ListCategoriesRequestObject) (oapi.ListCategoriesResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	rows, err := h.catalog.Categories(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.Category, 0, len(rows))
	for _, c := range rows {
		items = append(items, p.category(c))
	}
	return oapi.ListCategories200JSONResponse{Items: items}, nil
}

// ListProducts returns the catalog grid.
func (h *Handlers) ListProducts(ctx context.Context, req oapi.ListProductsRequestObject) (oapi.ListProductsResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	var category string
	if req.Params.Category != nil {
		category = *req.Params.Category
	}
	sort := service.SortFeatured
	if req.Params.Sort != nil {
		sort = service.ProductSort(*req.Params.Sort)
	}
	list, err := h.catalog.Products(ctx, category, sort)
	if err != nil {
		return nil, err
	}
	return oapi.ListProducts200JSONResponse(p.productCards(list)), nil
}

// GetProduct returns the product page payload.
func (h *Handlers) GetProduct(ctx context.Context, req oapi.GetProductRequestObject) (oapi.GetProductResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	d, err := h.catalog.Product(ctx, req.Slug)
	if err != nil {
		return nil, err
	}
	return oapi.GetProduct200JSONResponse(p.product(d)), nil
}

// GetPublicSettings returns contacts, pickup point, hero texts and delivery zones.
func (h *Handlers) GetPublicSettings(ctx context.Context, req oapi.GetPublicSettingsRequestObject) (oapi.GetPublicSettingsResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	s, err := h.catalog.PublicSettings(ctx)
	if err != nil {
		return nil, err
	}
	return oapi.GetPublicSettings200JSONResponse(p.settings(s)), nil
}

// ValidatePromo checks a promo code against a subtotal.
func (h *Handlers) ValidatePromo(ctx context.Context, req oapi.ValidatePromoRequestObject) (oapi.ValidatePromoResponseObject, error) {
	if req.Body == nil {
		return nil, validationError("body", service.FieldRequired)
	}
	res, err := h.checkout.ValidatePromo(ctx, req.Body.Code, req.Body.Subtotal)
	if err != nil {
		return nil, err
	}
	return oapi.ValidatePromo200JSONResponse{
		Code:     res.Promo.Code,
		Type:     oapi.PromoType(res.Promo.Type),
		Value:    res.Promo.Value,
		Discount: res.Discount,
	}, nil
}

// CreateOrder places an order.
func (h *Handlers) CreateOrder(ctx context.Context, req oapi.CreateOrderRequestObject) (oapi.CreateOrderResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, validationError("body", service.FieldRequired)
	}

	in := service.OrderInput{
		IdempotencyKey: req.Params.IdempotencyKey,
		Lang:           db.AppLang(p.lang),
		CustomerName:   b.CustomerName,
		Phone:          b.Phone,
		DeliveryMethod: db.DeliveryMethod(b.DeliveryMethod),
		DeliveryZoneID: b.DeliveryZoneID,
		Address:        deref(b.Address),
		PaymentMethod:  db.PaymentMethod(b.PaymentMethod),
		Comment:        deref(b.Comment),
		PromoCode:      deref(b.PromoCode),
		Honeypot:       deref(b.Website),
		Items:          make([]service.ItemInput, 0, len(b.Items)),
	}
	for _, it := range b.Items {
		in.Items = append(in.Items, service.ItemInput{VariantID: it.VariantID, Qty: it.Qty})
	}
	if a := b.Attribution; a != nil {
		in.Attribution = service.Attribution{
			UtmSource: deref(a.UtmSource), UtmMedium: deref(a.UtmMedium),
			UtmCampaign: deref(a.UtmCampaign), UtmContent: deref(a.UtmContent),
			Fbp: deref(a.Fbp), Fbc: deref(a.Fbc), Ttclid: deref(a.Ttclid),
		}
	}

	d, err := h.checkout.CreateOrder(ctx, in)
	if err != nil {
		return nil, err
	}
	return oapi.CreateOrder201JSONResponse(p.order(d)), nil
}

// GetOrder returns an order for the thank-you page.
func (h *Handlers) GetOrder(ctx context.Context, req oapi.GetOrderRequestObject) (oapi.GetOrderResponseObject, error) {
	p, err := h.presenter(req.Params.Lang)
	if err != nil {
		return nil, err
	}
	d, err := h.checkout.GetOrder(ctx, req.Number, req.Params.Phone)
	if err != nil {
		return nil, err
	}
	return oapi.GetOrder200JSONResponse(p.order(d)), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
