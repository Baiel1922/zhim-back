package http

import (
	"encoding/json"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// The admin panel is Russian-only, so admin payloads carry both languages as
// stored and use Russian wherever a single text is shown (order items, zones).

func adminDTO(a db.Admin) oapi.Admin {
	return oapi.Admin{ID: a.ID, TelegramID: a.TelegramID, Name: a.Name, Role: oapi.AdminRole(a.Role), IsActive: a.IsActive}
}

func adminCategoryDTO(c db.Category, productCount int32) oapi.AdminCategory {
	return oapi.AdminCategory{ID: c.ID, Slug: c.Slug, NameRu: c.NameRu, NameKy: c.NameKy, Sort: c.Sort, ProductCount: productCount}
}

func adminColorDTO(c db.Color) oapi.AdminColor {
	return oapi.AdminColor{ID: c.ID, NameRu: c.NameRu, NameKy: c.NameKy, Hex: c.Hex}
}

func sizeChartDataDTO(d domain.SizeChartData) oapi.SizeChartData {
	out := oapi.SizeChartData{
		Unit:    d.Unit,
		Columns: make([]oapi.SizeChartColumnInput, 0, len(d.Columns)),
		Rows:    make([]oapi.SizeChartRow, 0, len(d.Rows)),
		Fit:     make([]oapi.SizeFitRule, 0, len(d.Fit)),
	}
	for _, c := range d.Columns {
		out.Columns = append(out.Columns, oapi.SizeChartColumnInput{Key: c.Key, NameRu: c.NameRu, NameKy: c.NameKy})
	}
	for _, r := range d.Rows {
		values := r.Values
		if values == nil {
			values = map[string]string{}
		}
		out.Rows = append(out.Rows, oapi.SizeChartRow{Size: r.Size, Values: values})
	}
	for _, f := range d.Fit {
		out.Fit = append(out.Fit, oapi.SizeFitRule{Size: f.Size, HeightMin: f.HeightMin, HeightMax: f.HeightMax, WeightMin: f.WeightMin, WeightMax: f.WeightMax})
	}
	return out
}

func sizeChartDataInput(d oapi.SizeChartData) domain.SizeChartData {
	out := domain.SizeChartData{Unit: d.Unit}
	for _, c := range d.Columns {
		out.Columns = append(out.Columns, domain.SizeChartColumn{Key: c.Key, NameRu: c.NameRu, NameKy: c.NameKy})
	}
	for _, r := range d.Rows {
		out.Rows = append(out.Rows, domain.SizeChartRow{Size: r.Size, Values: r.Values})
	}
	for _, f := range d.Fit {
		out.Fit = append(out.Fit, domain.SizeFitRule{Size: f.Size, HeightMin: f.HeightMin, HeightMax: f.HeightMax, WeightMin: f.WeightMin, WeightMax: f.WeightMax})
	}
	return out
}

func adminSizeChartDTO(c db.SizeChart) (oapi.AdminSizeChart, error) {
	var data domain.SizeChartData
	if err := json.Unmarshal(c.Data, &data); err != nil {
		return oapi.AdminSizeChart{}, err
	}
	return oapi.AdminSizeChart{ID: c.ID, Name: c.Name, Data: sizeChartDataDTO(data)}, nil
}

func adminVariantDTO(v db.ProductVariant) oapi.AdminVariant {
	return oapi.AdminVariant{ID: v.ID, ColorID: v.ColorID, Size: v.Size, Sku: v.Sku, Stock: v.Stock, IsActive: v.IsActive}
}

func (h *Handlers) adminImageDTO(img db.ProductImage) oapi.AdminImage {
	src := func(p imgproxy.Preset) oapi.ImageSource {
		s := h.images.Source(img.S3Key, p)
		return oapi.ImageSource{Src: s.Src, Srcset: s.Srcset}
	}
	return oapi.AdminImage{
		ID: img.ID, ColorID: img.ColorID, Sort: img.Sort, AltRu: img.AltRu, AltKy: img.AltKy,
		Thumb: src(imgproxy.Thumb), Card: src(imgproxy.Card),
	}
}

func (h *Handlers) adminProductDTO(p *service.AdminProduct) oapi.AdminProduct {
	pr := p.Product
	out := oapi.AdminProduct{
		ID: pr.ID, Slug: pr.Slug, CategoryID: pr.CategoryID,
		TitleRu: pr.TitleRu, TitleKy: pr.TitleKy,
		DescriptionRu: pr.DescriptionRu, DescriptionKy: pr.DescriptionKy,
		CompositionRu: pr.CompositionRu, CompositionKy: pr.CompositionKy,
		CareRu: pr.CareRu, CareKy: pr.CareKy,
		Price: pr.Price, OldPrice: pr.OldPrice, IsActive: pr.IsActive, IsNew: pr.IsNew, Sort: pr.Sort,
		SizeChartID: pr.SizeChartID, CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt,
		Variants: make([]oapi.AdminVariant, 0, len(p.Variants)),
		Images:   make([]oapi.AdminImage, 0, len(p.Images)),
	}
	for _, v := range p.Variants {
		out.Variants = append(out.Variants, adminVariantDTO(v))
	}
	for _, img := range p.Images {
		out.Images = append(out.Images, h.adminImageDTO(img))
	}
	return out
}

func productInput(b oapi.AdminProductInput) service.ProductInput {
	return service.ProductInput{
		Slug: b.Slug, CategoryID: b.CategoryID, TitleRu: b.TitleRu, TitleKy: b.TitleKy,
		DescriptionRu: b.DescriptionRu, DescriptionKy: b.DescriptionKy,
		CompositionRu: b.CompositionRu, CompositionKy: b.CompositionKy,
		CareRu: b.CareRu, CareKy: b.CareKy, Price: b.Price, OldPrice: b.OldPrice,
		IsActive: b.IsActive, IsNew: b.IsNew, Sort: b.Sort, SizeChartID: b.SizeChartID,
	}
}

func adminZoneDTO(z db.DeliveryZone) oapi.AdminDeliveryZone {
	return oapi.AdminDeliveryZone{ID: z.ID, NameRu: z.NameRu, NameKy: z.NameKy, Price: z.Price, FreeFrom: z.FreeFrom, IsActive: z.IsActive, Sort: z.Sort}
}

func adminPromoDTO(p db.PromoCode) oapi.AdminPromoCode {
	return oapi.AdminPromoCode{
		ID: p.ID, Code: p.Code, Type: oapi.PromoType(p.Type), Value: p.Value, MinTotal: p.MinTotal,
		MaxUses: p.MaxUses, UsedCount: p.UsedCount, ValidFrom: p.ValidFrom, ValidTo: p.ValidTo,
		IsActive: p.IsActive, CreatedAt: p.CreatedAt,
	}
}

func promoInput(b oapi.AdminPromoCodeInput) service.PromoCodeInput {
	return service.PromoCodeInput{
		Code: b.Code, Type: db.PromoType(b.Type), Value: b.Value, MinTotal: b.MinTotal,
		MaxUses: b.MaxUses, ValidFrom: b.ValidFrom, ValidTo: b.ValidTo, IsActive: b.IsActive,
	}
}

func adminSettingsDTO(s *service.StoreSettings) oapi.AdminSettings {
	out := oapi.AdminSettings{
		Contacts: oapi.Contacts{Phones: s.Contacts.Phones, Whatsapp: s.Contacts.WhatsApp, Telegram: s.Contacts.Telegram, Instagram: s.Contacts.Instagram},
		Pickup:   oapi.AdminPickup{AddressRu: s.Pickup.AddressRu, AddressKy: s.Pickup.AddressKy, HoursRu: s.Pickup.HoursRu, HoursKy: s.Pickup.HoursKy},
		Hero:     oapi.AdminHero{TitleRu: s.Hero.TitleRu, TitleKy: s.Hero.TitleKy, SubtitleRu: s.Hero.SubtitleRu, SubtitleKy: s.Hero.SubtitleKy},
	}
	if out.Contacts.Phones == nil {
		out.Contacts.Phones = []string{}
	}
	if s.Pickup.MapURL != "" {
		out.Pickup.MapURL = &s.Pickup.MapURL
	}
	return out
}

func settingsInput(b oapi.AdminSettings) service.StoreSettings {
	return service.StoreSettings{
		Contacts: domain.Contacts{Phones: b.Contacts.Phones, WhatsApp: b.Contacts.Whatsapp, Telegram: b.Contacts.Telegram, Instagram: b.Contacts.Instagram},
		Pickup:   domain.Pickup{AddressRu: b.Pickup.AddressRu, AddressKy: b.Pickup.AddressKy, HoursRu: b.Pickup.HoursRu, HoursKy: b.Pickup.HoursKy, MapURL: deref(b.Pickup.MapURL)},
		Hero:     domain.Hero{TitleRu: b.Hero.TitleRu, TitleKy: b.Hero.TitleKy, SubtitleRu: b.Hero.SubtitleRu, SubtitleKy: b.Hero.SubtitleKy},
	}
}

func (h *Handlers) adminOrderDTO(o *service.AdminOrder) oapi.AdminOrder {
	public := presenter{lang: oapi.LangRu, images: h.images}.order(o.OrderDetails)
	ord := o.Order
	out := oapi.AdminOrder{
		ID:              ord.ID,
		Number:          ord.Number,
		Status:          public.Status,
		PaymentStatus:   public.PaymentStatus,
		PaymentMethod:   public.PaymentMethod,
		DeliveryMethod:  public.DeliveryMethod,
		DeliveryZone:    public.DeliveryZone,
		Address:         ord.Address,
		CustomerName:    ord.CustomerName,
		Phone:           ord.Phone,
		Comment:         ord.Comment,
		InternalComment: ord.InternalComment,
		Lang:            oapi.Lang(ord.Lang),
		PromoCode:       public.PromoCode,
		Items:           public.Items,
		Subtotal:        ord.Subtotal,
		Discount:        ord.Discount,
		DeliveryPrice:   ord.DeliveryPrice,
		Total:           ord.Total,
		Attribution: oapi.Attribution{
			UtmSource: ord.UtmSource, UtmMedium: ord.UtmMedium, UtmCampaign: ord.UtmCampaign, UtmContent: ord.UtmContent,
			Fbp: ord.Fbp, Fbc: ord.Fbc, Ttclid: ord.Ttclid,
		},
		History:         make([]oapi.OrderHistoryEntry, 0, len(o.History)),
		AllowedStatuses: make([]oapi.OrderStatus, 0, len(o.Allowed)),
		CreatedAt:       ord.CreatedAt,
		UpdatedAt:       ord.UpdatedAt,
	}
	for _, e := range o.History {
		entry := oapi.OrderHistoryEntry{ToStatus: oapi.OrderStatus(e.ToStatus), AdminName: e.AdminName, CreatedAt: e.CreatedAt}
		if e.FromStatus != nil {
			from := oapi.OrderStatus(*e.FromStatus)
			entry.FromStatus = &from
		}
		out.History = append(out.History, entry)
	}
	for _, s := range o.Allowed {
		out.AllowedStatuses = append(out.AllowedStatuses, oapi.OrderStatus(s))
	}
	return out
}

func orderListDTO(l *service.OrderList) oapi.AdminOrderList {
	out := oapi.AdminOrderList{
		Items:   make([]oapi.AdminOrderSummary, 0, len(l.Orders)),
		Total:   l.Total,
		Page:    l.Page,
		PerPage: l.PerPage,
		StatusCounts: oapi.OrderStatusCounts{
			New:            l.StatusCounts[db.OrderStatusNew],
			Confirmed:      l.StatusCounts[db.OrderStatusConfirmed],
			Assembling:     l.StatusCounts[db.OrderStatusAssembling],
			Shipped:        l.StatusCounts[db.OrderStatusShipped],
			ReadyForPickup: l.StatusCounts[db.OrderStatusReadyForPickup],
			Completed:      l.StatusCounts[db.OrderStatusCompleted],
			Cancelled:      l.StatusCounts[db.OrderStatusCancelled],
		},
	}
	for _, row := range l.Orders {
		o := row.Order
		out.Items = append(out.Items, oapi.AdminOrderSummary{
			ID: o.ID, Number: o.Number, Status: oapi.OrderStatus(o.Status), PaymentStatus: oapi.PaymentStatus(o.PaymentStatus),
			PaymentMethod: oapi.PaymentMethod(o.PaymentMethod), DeliveryMethod: oapi.DeliveryMethod(o.DeliveryMethod),
			CustomerName: o.CustomerName, Phone: o.Phone, Total: o.Total, ItemsCount: row.ItemsCount, CreatedAt: o.CreatedAt,
		})
	}
	return out
}

func dashboardDTO(d *service.Dashboard) oapi.Dashboard {
	stats := func(s service.PeriodStats) oapi.PeriodStats {
		return oapi.PeriodStats{Orders: s.Orders, Revenue: s.Revenue}
	}
	out := oapi.Dashboard{
		Today: stats(d.Today), Last7Days: stats(d.Last7Days), Last30Days: stats(d.Last30Days),
		TopProducts: make([]oapi.TopProduct, 0, len(d.TopProducts)),
		LowStock:    make([]oapi.LowStockVariant, 0, len(d.LowStock)),
	}
	for _, p := range d.TopProducts {
		out.TopProducts = append(out.TopProducts, oapi.TopProduct{ProductID: p.ProductID, Title: p.Title, Qty: p.Qty, Revenue: p.Revenue})
	}
	for _, v := range d.LowStock {
		out.LowStock = append(out.LowStock, oapi.LowStockVariant{
			VariantID: v.ID, ProductID: v.ProductID, Title: v.TitleRu, ColorName: v.ColorNameRu, Size: v.Size, Sku: v.Sku, Stock: v.Stock,
		})
	}
	return out
}
