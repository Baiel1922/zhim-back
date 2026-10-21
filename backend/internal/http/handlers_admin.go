package http

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

func bodyRequired() error { return validationError("body", service.FieldRequired) }

// ---------- Auth and admins ----------

// AdminAuthTelegram exchanges Telegram initData for a token.
func (h *Handlers) AdminAuthTelegram(ctx context.Context, req oapi.AdminAuthTelegramRequestObject) (oapi.AdminAuthTelegramResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	s, err := h.adminAuth.Login(ctx, req.Body.InitData)
	if err != nil {
		return nil, err
	}
	return oapi.AdminAuthTelegram200JSONResponse{Token: s.Token, ExpiresAt: s.ExpiresAt, Admin: adminDTO(s.Admin)}, nil
}

// AdminGetMe returns the current admin.
func (h *Handlers) AdminGetMe(ctx context.Context, _ oapi.AdminGetMeRequestObject) (oapi.AdminGetMeResponseObject, error) {
	return oapi.AdminGetMe200JSONResponse(adminDTO(currentAdmin(ctx))), nil
}

// AdminListAdmins lists admin accounts.
func (h *Handlers) AdminListAdmins(ctx context.Context, _ oapi.AdminListAdminsRequestObject) (oapi.AdminListAdminsResponseObject, error) {
	rows, err := h.admin.ListAdmins(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.Admin, 0, len(rows))
	for _, a := range rows {
		items = append(items, adminDTO(a))
	}
	return oapi.AdminListAdmins200JSONResponse{Items: items}, nil
}

// AdminCreateAdmin adds an admin.
func (h *Handlers) AdminCreateAdmin(ctx context.Context, req oapi.AdminCreateAdminRequestObject) (oapi.AdminCreateAdminResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	b := req.Body
	a, err := h.admin.CreateAdmin(ctx, service.AdminInput{TelegramID: b.TelegramID, Name: b.Name, Role: db.AdminRole(b.Role), IsActive: b.IsActive})
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateAdmin201JSONResponse(adminDTO(a)), nil
}

// AdminUpdateAdmin changes an admin.
func (h *Handlers) AdminUpdateAdmin(ctx context.Context, req oapi.AdminUpdateAdminRequestObject) (oapi.AdminUpdateAdminResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	b := req.Body
	a, err := h.admin.UpdateAdmin(ctx, req.ID, service.AdminInput{Name: b.Name, Role: db.AdminRole(b.Role), IsActive: b.IsActive})
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateAdmin200JSONResponse(adminDTO(a)), nil
}

// AdminDeleteAdmin removes an admin.
func (h *Handlers) AdminDeleteAdmin(ctx context.Context, req oapi.AdminDeleteAdminRequestObject) (oapi.AdminDeleteAdminResponseObject, error) {
	if err := h.admin.DeleteAdmin(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteAdmin204Response{}, nil
}

// ---------- Categories ----------

// AdminListCategories lists categories.
func (h *Handlers) AdminListCategories(ctx context.Context, _ oapi.AdminListCategoriesRequestObject) (oapi.AdminListCategoriesResponseObject, error) {
	rows, err := h.admin.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminCategory, 0, len(rows))
	for _, r := range rows {
		items = append(items, adminCategoryDTO(db.Category{ID: r.ID, Slug: r.Slug, NameRu: r.NameRu, NameKy: r.NameKy, Sort: r.Sort}, r.ProductCount))
	}
	return oapi.AdminListCategories200JSONResponse{Items: items}, nil
}

func categoryInput(b oapi.AdminCategoryInput) service.CategoryInput {
	return service.CategoryInput{Slug: b.Slug, NameRu: b.NameRu, NameKy: b.NameKy, Sort: b.Sort}
}

// AdminCreateCategory adds a category.
func (h *Handlers) AdminCreateCategory(ctx context.Context, req oapi.AdminCreateCategoryRequestObject) (oapi.AdminCreateCategoryResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.CreateCategory(ctx, categoryInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateCategory201JSONResponse(adminCategoryDTO(c, 0)), nil
}

// AdminUpdateCategory changes a category.
func (h *Handlers) AdminUpdateCategory(ctx context.Context, req oapi.AdminUpdateCategoryRequestObject) (oapi.AdminUpdateCategoryResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.UpdateCategory(ctx, req.ID, categoryInput(*req.Body))
	if err != nil {
		return nil, err
	}
	rows, err := h.admin.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	var count int32
	for _, r := range rows {
		if r.ID == c.ID {
			count = r.ProductCount
		}
	}
	return oapi.AdminUpdateCategory200JSONResponse(adminCategoryDTO(c, count)), nil
}

// AdminDeleteCategory removes a category.
func (h *Handlers) AdminDeleteCategory(ctx context.Context, req oapi.AdminDeleteCategoryRequestObject) (oapi.AdminDeleteCategoryResponseObject, error) {
	if err := h.admin.DeleteCategory(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteCategory204Response{}, nil
}

// ---------- Colors ----------

// AdminListColors lists colors.
func (h *Handlers) AdminListColors(ctx context.Context, _ oapi.AdminListColorsRequestObject) (oapi.AdminListColorsResponseObject, error) {
	rows, err := h.admin.ListColors(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminColor, 0, len(rows))
	for _, c := range rows {
		items = append(items, adminColorDTO(c))
	}
	return oapi.AdminListColors200JSONResponse{Items: items}, nil
}

// AdminCreateColor adds a color.
func (h *Handlers) AdminCreateColor(ctx context.Context, req oapi.AdminCreateColorRequestObject) (oapi.AdminCreateColorResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.CreateColor(ctx, service.ColorInput{NameRu: req.Body.NameRu, NameKy: req.Body.NameKy, Hex: req.Body.Hex})
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateColor201JSONResponse(adminColorDTO(c)), nil
}

// AdminUpdateColor changes a color.
func (h *Handlers) AdminUpdateColor(ctx context.Context, req oapi.AdminUpdateColorRequestObject) (oapi.AdminUpdateColorResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.UpdateColor(ctx, req.ID, service.ColorInput{NameRu: req.Body.NameRu, NameKy: req.Body.NameKy, Hex: req.Body.Hex})
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateColor200JSONResponse(adminColorDTO(c)), nil
}

// AdminDeleteColor removes a color.
func (h *Handlers) AdminDeleteColor(ctx context.Context, req oapi.AdminDeleteColorRequestObject) (oapi.AdminDeleteColorResponseObject, error) {
	if err := h.admin.DeleteColor(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteColor204Response{}, nil
}

// ---------- Size charts ----------

// AdminListSizeCharts lists size charts.
func (h *Handlers) AdminListSizeCharts(ctx context.Context, _ oapi.AdminListSizeChartsRequestObject) (oapi.AdminListSizeChartsResponseObject, error) {
	rows, err := h.admin.ListSizeCharts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminSizeChart, 0, len(rows))
	for _, c := range rows {
		dto, err := adminSizeChartDTO(c)
		if err != nil {
			return nil, err
		}
		items = append(items, dto)
	}
	return oapi.AdminListSizeCharts200JSONResponse{Items: items}, nil
}

// AdminCreateSizeChart adds a size chart.
func (h *Handlers) AdminCreateSizeChart(ctx context.Context, req oapi.AdminCreateSizeChartRequestObject) (oapi.AdminCreateSizeChartResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.CreateSizeChart(ctx, service.SizeChartInput{Name: req.Body.Name, Data: sizeChartDataInput(req.Body.Data)})
	if err != nil {
		return nil, err
	}
	dto, err := adminSizeChartDTO(c)
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateSizeChart201JSONResponse(dto), nil
}

// AdminUpdateSizeChart changes a size chart.
func (h *Handlers) AdminUpdateSizeChart(ctx context.Context, req oapi.AdminUpdateSizeChartRequestObject) (oapi.AdminUpdateSizeChartResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := h.admin.UpdateSizeChart(ctx, req.ID, service.SizeChartInput{Name: req.Body.Name, Data: sizeChartDataInput(req.Body.Data)})
	if err != nil {
		return nil, err
	}
	dto, err := adminSizeChartDTO(c)
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateSizeChart200JSONResponse(dto), nil
}

// AdminDeleteSizeChart removes a size chart.
func (h *Handlers) AdminDeleteSizeChart(ctx context.Context, req oapi.AdminDeleteSizeChartRequestObject) (oapi.AdminDeleteSizeChartResponseObject, error) {
	if err := h.admin.DeleteSizeChart(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteSizeChart204Response{}, nil
}

// ---------- Products and variants ----------

// AdminListProducts lists products.
func (h *Handlers) AdminListProducts(ctx context.Context, req oapi.AdminListProductsRequestObject) (oapi.AdminListProductsResponseObject, error) {
	rows, err := h.admin.ListProducts(ctx, service.ProductFilter{CategoryID: req.Params.CategoryID, Query: deref(req.Params.Q)})
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminProductSummary, 0, len(rows))
	for _, r := range rows {
		item := oapi.AdminProductSummary{
			ID: r.ID, Slug: r.Slug, TitleRu: r.TitleRu, TitleKy: r.TitleKy, CategoryID: r.CategoryID,
			Price: r.Price, OldPrice: r.OldPrice, IsActive: r.IsActive, IsNew: r.IsNew, Sort: r.Sort,
			TotalStock: r.TotalStock, VariantCount: r.VariantCount,
		}
		if r.PreviewS3Key != "" {
			s := h.images.Source(r.PreviewS3Key, thumbPreset)
			item.Preview = &oapi.ImageSource{Src: s.Src, Srcset: s.Srcset}
		}
		items = append(items, item)
	}
	return oapi.AdminListProducts200JSONResponse{Items: items}, nil
}

// AdminGetProduct returns a product for editing.
func (h *Handlers) AdminGetProduct(ctx context.Context, req oapi.AdminGetProductRequestObject) (oapi.AdminGetProductResponseObject, error) {
	p, err := h.admin.GetProduct(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return oapi.AdminGetProduct200JSONResponse(h.adminProductDTO(p)), nil
}

// AdminCreateProduct adds a product.
func (h *Handlers) AdminCreateProduct(ctx context.Context, req oapi.AdminCreateProductRequestObject) (oapi.AdminCreateProductResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.CreateProduct(ctx, productInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateProduct201JSONResponse(h.adminProductDTO(p)), nil
}

// AdminUpdateProduct changes a product.
func (h *Handlers) AdminUpdateProduct(ctx context.Context, req oapi.AdminUpdateProductRequestObject) (oapi.AdminUpdateProductResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.UpdateProduct(ctx, req.ID, productInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateProduct200JSONResponse(h.adminProductDTO(p)), nil
}

// AdminDeleteProduct removes a product.
func (h *Handlers) AdminDeleteProduct(ctx context.Context, req oapi.AdminDeleteProductRequestObject) (oapi.AdminDeleteProductResponseObject, error) {
	if err := h.admin.DeleteProduct(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteProduct204Response{}, nil
}

// AdminCreateVariants creates the size × color matrix.
func (h *Handlers) AdminCreateVariants(ctx context.Context, req oapi.AdminCreateVariantsRequestObject) (oapi.AdminCreateVariantsResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.CreateVariants(ctx, req.ID, service.VariantMatrixInput{ColorIDs: req.Body.ColorIds, Sizes: req.Body.Sizes, Stock: req.Body.Stock})
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateVariants201JSONResponse(h.adminProductDTO(p)), nil
}

// AdminUpdateVariant changes a variant.
func (h *Handlers) AdminUpdateVariant(ctx context.Context, req oapi.AdminUpdateVariantRequestObject) (oapi.AdminUpdateVariantResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	v, err := h.admin.UpdateVariant(ctx, req.ID, req.Body.Sku, req.Body.IsActive)
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateVariant200JSONResponse(adminVariantDTO(v)), nil
}

// AdminSetVariantStock sets a variant's stock.
func (h *Handlers) AdminSetVariantStock(ctx context.Context, req oapi.AdminSetVariantStockRequestObject) (oapi.AdminSetVariantStockResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	v, err := h.admin.SetVariantStock(ctx, req.ID, req.Body.Stock)
	if err != nil {
		return nil, err
	}
	return oapi.AdminSetVariantStock200JSONResponse(adminVariantDTO(v)), nil
}

// AdminDeleteVariant removes a variant.
func (h *Handlers) AdminDeleteVariant(ctx context.Context, req oapi.AdminDeleteVariantRequestObject) (oapi.AdminDeleteVariantResponseObject, error) {
	if err := h.admin.DeleteVariant(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteVariant204Response{}, nil
}

// ---------- Images ----------

// AdminUploadImage stores a product photo.
func (h *Handlers) AdminUploadImage(ctx context.Context, req oapi.AdminUploadImageRequestObject) (oapi.AdminUploadImageResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	var in service.ImageUpload
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, uploadReadErr(err)
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, service.MaxImageBytes+1))
			if err != nil {
				return nil, uploadReadErr(err)
			}
			in.Data = data
		case "color_id":
			v, err := readFormValue(part)
			if err != nil {
				return nil, uploadReadErr(err)
			}
			if v != "" {
				id, err := strconv.ParseInt(v, 10, 64)
				if err != nil || id <= 0 {
					return nil, validationError("color_id", service.FieldInvalid)
				}
				in.ColorID = &id
			}
		case "alt_ru", "alt_ky":
			v, err := readFormValue(part)
			if err != nil {
				return nil, uploadReadErr(err)
			}
			if part.FormName() == "alt_ru" {
				in.AltRu = v
			} else {
				in.AltKy = v
			}
		}
		_ = part.Close()
	}
	img, err := h.admin.UploadImage(ctx, req.ID, in)
	if err != nil {
		return nil, err
	}
	return oapi.AdminUploadImage201JSONResponse(h.adminImageDTO(img)), nil
}

func readFormValue(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4<<10))
	return strings.TrimSpace(string(b)), err
}

func uploadReadErr(err error) error {
	if isTooLarge(err) {
		return &service.Error{Code: service.CodeFileTooLarge, Message: "upload is too large"}
	}
	return &service.Error{Code: service.CodeValidation, Message: "malformed multipart body: " + err.Error()}
}

// AdminReorderImages reorders photos.
func (h *Handlers) AdminReorderImages(ctx context.Context, req oapi.AdminReorderImagesRequestObject) (oapi.AdminReorderImagesResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.ReorderImages(ctx, req.ID, req.Body.ImageIds)
	if err != nil {
		return nil, err
	}
	return oapi.AdminReorderImages200JSONResponse(h.adminProductDTO(p)), nil
}

// AdminUpdateImage binds a photo to a color and sets alt texts.
func (h *Handlers) AdminUpdateImage(ctx context.Context, req oapi.AdminUpdateImageRequestObject) (oapi.AdminUpdateImageResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	img, err := h.admin.UpdateImage(ctx, req.ID, req.Body.ColorID, req.Body.AltRu, req.Body.AltKy)
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateImage200JSONResponse(h.adminImageDTO(img)), nil
}

// AdminDeleteImage removes a photo.
func (h *Handlers) AdminDeleteImage(ctx context.Context, req oapi.AdminDeleteImageRequestObject) (oapi.AdminDeleteImageResponseObject, error) {
	if err := h.admin.DeleteImage(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteImage204Response{}, nil
}

// ---------- Delivery zones ----------

func zoneInput(b oapi.AdminDeliveryZoneInput) service.DeliveryZoneInput {
	return service.DeliveryZoneInput{NameRu: b.NameRu, NameKy: b.NameKy, Price: b.Price, FreeFrom: b.FreeFrom, IsActive: b.IsActive, Sort: b.Sort}
}

// AdminListDeliveryZones lists zones.
func (h *Handlers) AdminListDeliveryZones(ctx context.Context, _ oapi.AdminListDeliveryZonesRequestObject) (oapi.AdminListDeliveryZonesResponseObject, error) {
	rows, err := h.admin.ListDeliveryZones(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminDeliveryZone, 0, len(rows))
	for _, z := range rows {
		items = append(items, adminZoneDTO(z))
	}
	return oapi.AdminListDeliveryZones200JSONResponse{Items: items}, nil
}

// AdminCreateDeliveryZone adds a zone.
func (h *Handlers) AdminCreateDeliveryZone(ctx context.Context, req oapi.AdminCreateDeliveryZoneRequestObject) (oapi.AdminCreateDeliveryZoneResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	z, err := h.admin.CreateDeliveryZone(ctx, zoneInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreateDeliveryZone201JSONResponse(adminZoneDTO(z)), nil
}

// AdminUpdateDeliveryZone changes a zone.
func (h *Handlers) AdminUpdateDeliveryZone(ctx context.Context, req oapi.AdminUpdateDeliveryZoneRequestObject) (oapi.AdminUpdateDeliveryZoneResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	z, err := h.admin.UpdateDeliveryZone(ctx, req.ID, zoneInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateDeliveryZone200JSONResponse(adminZoneDTO(z)), nil
}

// AdminDeleteDeliveryZone removes a zone.
func (h *Handlers) AdminDeleteDeliveryZone(ctx context.Context, req oapi.AdminDeleteDeliveryZoneRequestObject) (oapi.AdminDeleteDeliveryZoneResponseObject, error) {
	if err := h.admin.DeleteDeliveryZone(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeleteDeliveryZone204Response{}, nil
}

// ---------- Promo codes ----------

// AdminListPromoCodes lists promo codes.
func (h *Handlers) AdminListPromoCodes(ctx context.Context, _ oapi.AdminListPromoCodesRequestObject) (oapi.AdminListPromoCodesResponseObject, error) {
	rows, err := h.admin.ListPromoCodes(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]oapi.AdminPromoCode, 0, len(rows))
	for _, p := range rows {
		items = append(items, adminPromoDTO(p))
	}
	return oapi.AdminListPromoCodes200JSONResponse{Items: items}, nil
}

// AdminCreatePromoCode adds a promo code.
func (h *Handlers) AdminCreatePromoCode(ctx context.Context, req oapi.AdminCreatePromoCodeRequestObject) (oapi.AdminCreatePromoCodeResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.CreatePromoCode(ctx, promoInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminCreatePromoCode201JSONResponse(adminPromoDTO(p)), nil
}

// AdminUpdatePromoCode changes a promo code.
func (h *Handlers) AdminUpdatePromoCode(ctx context.Context, req oapi.AdminUpdatePromoCodeRequestObject) (oapi.AdminUpdatePromoCodeResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, err := h.admin.UpdatePromoCode(ctx, req.ID, promoInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdatePromoCode200JSONResponse(adminPromoDTO(p)), nil
}

// AdminDeletePromoCode removes a promo code.
func (h *Handlers) AdminDeletePromoCode(ctx context.Context, req oapi.AdminDeletePromoCodeRequestObject) (oapi.AdminDeletePromoCodeResponseObject, error) {
	if err := h.admin.DeletePromoCode(ctx, req.ID); err != nil {
		return nil, err
	}
	return oapi.AdminDeletePromoCode204Response{}, nil
}

// ---------- Settings ----------

// AdminGetSettings returns store settings.
func (h *Handlers) AdminGetSettings(ctx context.Context, _ oapi.AdminGetSettingsRequestObject) (oapi.AdminGetSettingsResponseObject, error) {
	s, err := h.admin.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return oapi.AdminGetSettings200JSONResponse(adminSettingsDTO(s)), nil
}

// AdminUpdateSettings replaces store settings.
func (h *Handlers) AdminUpdateSettings(ctx context.Context, req oapi.AdminUpdateSettingsRequestObject) (oapi.AdminUpdateSettingsResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	s, err := h.admin.UpdateSettings(ctx, settingsInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.AdminUpdateSettings200JSONResponse(adminSettingsDTO(s)), nil
}

// ---------- Orders and dashboard ----------

// AdminListOrders lists orders.
func (h *Handlers) AdminListOrders(ctx context.Context, req oapi.AdminListOrdersRequestObject) (oapi.AdminListOrdersResponseObject, error) {
	p := req.Params
	f := service.OrderFilter{Query: deref(p.Q)}
	if p.Status != nil {
		s := db.OrderStatus(*p.Status)
		f.Status = &s
	}
	if p.From != nil {
		t := p.From.Time
		f.From = &t
	}
	if p.To != nil {
		t := p.To.Time
		f.To = &t
	}
	if p.Page != nil {
		f.Page = *p.Page
	}
	if p.PerPage != nil {
		f.PerPage = *p.PerPage
	}
	l, err := h.admin.ListOrders(ctx, f)
	if err != nil {
		return nil, err
	}
	return oapi.AdminListOrders200JSONResponse(orderListDTO(l)), nil
}

// AdminGetOrder returns order details.
func (h *Handlers) AdminGetOrder(ctx context.Context, req oapi.AdminGetOrderRequestObject) (oapi.AdminGetOrderResponseObject, error) {
	o, err := h.admin.GetOrder(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return oapi.AdminGetOrder200JSONResponse(h.adminOrderDTO(o)), nil
}

// AdminSetOrderStatus moves an order to another status.
func (h *Handlers) AdminSetOrderStatus(ctx context.Context, req oapi.AdminSetOrderStatusRequestObject) (oapi.AdminSetOrderStatusResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	actor := currentAdmin(ctx).ID
	o, err := h.admin.SetOrderStatus(ctx, &actor, req.ID, db.OrderStatus(req.Body.Status))
	if err != nil {
		return nil, err
	}
	return oapi.AdminSetOrderStatus200JSONResponse(h.adminOrderDTO(o)), nil
}

// AdminSetOrderPayment marks an order paid or unpaid.
func (h *Handlers) AdminSetOrderPayment(ctx context.Context, req oapi.AdminSetOrderPaymentRequestObject) (oapi.AdminSetOrderPaymentResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	o, err := h.admin.SetOrderPayment(ctx, req.ID, db.PaymentStatus(req.Body.PaymentStatus))
	if err != nil {
		return nil, err
	}
	return oapi.AdminSetOrderPayment200JSONResponse(h.adminOrderDTO(o)), nil
}

// AdminSetOrderComment sets the internal comment.
func (h *Handlers) AdminSetOrderComment(ctx context.Context, req oapi.AdminSetOrderCommentRequestObject) (oapi.AdminSetOrderCommentResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	o, err := h.admin.SetOrderComment(ctx, req.ID, req.Body.InternalComment)
	if err != nil {
		return nil, err
	}
	return oapi.AdminSetOrderComment200JSONResponse(h.adminOrderDTO(o)), nil
}

// AdminGetDashboard returns sales metrics.
func (h *Handlers) AdminGetDashboard(ctx context.Context, _ oapi.AdminGetDashboardRequestObject) (oapi.AdminGetDashboardResponseObject, error) {
	d, err := h.admin.Dashboard(ctx)
	if err != nil {
		return nil, err
	}
	return oapi.AdminGetDashboard200JSONResponse(dashboardDTO(d)), nil
}
