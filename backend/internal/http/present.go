package http

import (
	"sort"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// presenter turns service results into localized API payloads with signed image URLs.
type presenter struct {
	lang   oapi.Lang
	images *imgproxy.Signer
}

func (p presenter) pick(ru, ky string) string {
	if p.lang == oapi.LangKy && ky != "" {
		return ky
	}
	return ru
}

func (p presenter) image(s3Key string, colorID *int64, alt string) oapi.Image {
	src := func(preset imgproxy.Preset) oapi.ImageSource {
		s := p.images.Source(s3Key, preset)
		return oapi.ImageSource{Src: s.Src, Srcset: s.Srcset}
	}
	return oapi.Image{
		ColorID: colorID,
		Alt:     alt,
		Thumb:   src(imgproxy.Thumb),
		Card:    src(imgproxy.Card),
		Full:    src(imgproxy.Full),
	}
}

func (p presenter) category(c db.Category) oapi.Category {
	return oapi.Category{ID: c.ID, Slug: c.Slug, Name: p.pick(c.NameRu, c.NameKy)}
}

func (p presenter) color(c db.Color) oapi.Color {
	return oapi.Color{ID: c.ID, Name: p.pick(c.NameRu, c.NameKy), Hex: c.Hex}
}

func (p presenter) productCards(list *service.ProductList) oapi.ProductList {
	items := make([]oapi.ProductCard, 0, len(list.Products))
	for _, row := range list.Products {
		title := p.pick(row.TitleRu, row.TitleKy)
		card := oapi.ProductCard{
			ID:           row.ID,
			Slug:         row.Slug,
			Title:        title,
			CategorySlug: row.CategorySlug,
			Price:        row.Price,
			OldPrice:     row.OldPrice,
			IsNew:        row.IsNew,
			InStock:      len(row.AvailableSizes) > 0,
			Sizes:        row.AvailableSizes,
			Colors:       make([]oapi.Color, 0, len(row.AvailableColorIDs)),
			Images:       make([]oapi.Image, 0, len(row.PreviewS3Keys)),
		}
		for _, id := range row.AvailableColorIDs {
			if c, ok := list.Colors[id]; ok {
				card.Colors = append(card.Colors, p.color(c))
			}
		}
		for _, key := range row.PreviewS3Keys {
			card.Images = append(card.Images, p.image(key, nil, title))
		}
		items = append(items, card)
	}
	return oapi.ProductList{Items: items}
}

func (p presenter) product(d *service.ProductDetails) oapi.Product {
	pr := d.Product
	title := p.pick(pr.TitleRu, pr.TitleKy)
	out := oapi.Product{
		ID:          pr.ID,
		Slug:        pr.Slug,
		Title:       title,
		Description: p.pick(pr.DescriptionRu, pr.DescriptionKy),
		Composition: p.pick(pr.CompositionRu, pr.CompositionKy),
		Care:        p.pick(pr.CareRu, pr.CareKy),
		Price:       pr.Price,
		OldPrice:    pr.OldPrice,
		IsNew:       pr.IsNew,
		Category:    p.category(d.Category),
		Colors:      []oapi.Color{},
		Sizes:       []string{},
		Variants:    make([]oapi.Variant, 0, len(d.Variants)),
		Images:      make([]oapi.Image, 0, len(d.Images)),
	}

	// Variants arrive in size order, so first-seen order of sizes is the display order.
	seenSize := map[string]bool{}
	seenColor := map[int64]bool{}
	for _, row := range d.Variants {
		v := row.ProductVariant
		out.Variants = append(out.Variants, oapi.Variant{ID: v.ID, ColorID: v.ColorID, Size: v.Size, Sku: v.Sku, Stock: v.Stock})
		if !seenSize[v.Size] {
			seenSize[v.Size] = true
			out.Sizes = append(out.Sizes, v.Size)
		}
		if !seenColor[v.ColorID] {
			seenColor[v.ColorID] = true
			out.Colors = append(out.Colors, oapi.Color{ID: v.ColorID, Name: p.pick(row.ColorNameRu, row.ColorNameKy), Hex: row.ColorHex})
		}
	}
	sort.Slice(out.Colors, func(i, j int) bool { return out.Colors[i].ID < out.Colors[j].ID })

	for _, img := range d.Images {
		alt := p.pick(img.AltRu, img.AltKy)
		if alt == "" {
			alt = title
		}
		out.Images = append(out.Images, p.image(img.S3Key, img.ColorID, alt))
	}

	if c := d.SizeChart; c != nil {
		chart := oapi.SizeChart{
			Unit:    c.Unit,
			Columns: make([]oapi.SizeChartColumn, 0, len(c.Columns)),
			Rows:    make([]oapi.SizeChartRow, 0, len(c.Rows)),
			Fit:     make([]oapi.SizeFitRule, 0, len(c.Fit)),
		}
		for _, col := range c.Columns {
			chart.Columns = append(chart.Columns, oapi.SizeChartColumn{Key: col.Key, Name: p.pick(col.NameRu, col.NameKy)})
		}
		for _, row := range c.Rows {
			chart.Rows = append(chart.Rows, oapi.SizeChartRow{Size: row.Size, Values: row.Values})
		}
		for _, f := range c.Fit {
			chart.Fit = append(chart.Fit, oapi.SizeFitRule{
				Size: f.Size, HeightMin: f.HeightMin, HeightMax: f.HeightMax, WeightMin: f.WeightMin, WeightMax: f.WeightMax,
			})
		}
		out.SizeChart = &chart
	}
	return out
}

func (p presenter) pickup(v domain.Pickup) oapi.Pickup {
	out := oapi.Pickup{Address: p.pick(v.AddressRu, v.AddressKy), Hours: p.pick(v.HoursRu, v.HoursKy)}
	if v.MapURL != "" {
		out.MapURL = &v.MapURL
	}
	return out
}

func (p presenter) deliveryZone(z db.DeliveryZone) oapi.DeliveryZone {
	return oapi.DeliveryZone{ID: z.ID, Name: p.pick(z.NameRu, z.NameKy), Price: z.Price, FreeFrom: z.FreeFrom}
}

func (p presenter) settings(s *service.Settings) oapi.PublicSettings {
	out := oapi.PublicSettings{
		Contacts: oapi.Contacts{
			Phones:    s.Contacts.Phones,
			Whatsapp:  s.Contacts.WhatsApp,
			Telegram:  s.Contacts.Telegram,
			Instagram: s.Contacts.Instagram,
		},
		Pickup:        p.pickup(s.Pickup),
		Hero:          oapi.Hero{Title: p.pick(s.Hero.TitleRu, s.Hero.TitleKy), Subtitle: p.pick(s.Hero.SubtitleRu, s.Hero.SubtitleKy)},
		DeliveryZones: make([]oapi.DeliveryZone, 0, len(s.DeliveryZones)),
	}
	for _, z := range s.DeliveryZones {
		out.DeliveryZones = append(out.DeliveryZones, p.deliveryZone(z))
	}
	return out
}

func (p presenter) order(d *service.OrderDetails) oapi.Order {
	o := d.Order
	out := oapi.Order{
		Number:         o.Number,
		Status:         oapi.OrderStatus(o.Status),
		PaymentStatus:  oapi.PaymentStatus(o.PaymentStatus),
		PaymentMethod:  oapi.PaymentMethod(o.PaymentMethod),
		DeliveryMethod: oapi.DeliveryMethod(o.DeliveryMethod),
		Address:        o.Address,
		CustomerName:   o.CustomerName,
		Phone:          o.Phone,
		Comment:        o.Comment,
		Items:          make([]oapi.OrderItem, 0, len(d.Items)),
		Subtotal:       o.Subtotal,
		Discount:       o.Discount,
		DeliveryPrice:  o.DeliveryPrice,
		Total:          o.Total,
		CreatedAt:      o.CreatedAt,
	}
	if d.DeliveryZone != nil {
		z := p.deliveryZone(*d.DeliveryZone)
		out.DeliveryZone = &z
	}
	if d.Pickup != nil {
		pk := p.pickup(*d.Pickup)
		out.Pickup = &pk
	}
	if d.PromoCode != "" {
		out.PromoCode = &d.PromoCode
	}
	for _, it := range d.Items {
		item := oapi.OrderItem{
			VariantID: it.VariantID,
			Title:     p.pick(it.TitleRu, it.TitleKy),
			ColorName: p.pick(it.ColorNameRu, it.ColorNameKy),
			Size:      it.Size,
			Sku:       it.Sku,
			Price:     it.Price,
			Qty:       it.Qty,
			LineTotal: it.Price * it.Qty,
		}
		if it.ProductID != nil {
			if slug, ok := d.ProductSlugs[*it.ProductID]; ok {
				item.ProductSlug = &slug
			}
		}
		out.Items = append(out.Items, item)
	}
	return out
}
