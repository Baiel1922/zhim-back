package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// ProductSort is the catalog ordering.
type ProductSort string

// Catalog orderings.
const (
	SortFeatured  ProductSort = "featured"
	SortNew       ProductSort = "new"
	SortPriceAsc  ProductSort = "price_asc"
	SortPriceDesc ProductSort = "price_desc"
)

// Catalog serves storefront reads.
type Catalog struct {
	store *repo.Store
}

// NewCatalog creates a Catalog.
func NewCatalog(store *repo.Store) *Catalog {
	return &Catalog{store: store}
}

// Categories returns all categories in display order.
func (c *Catalog) Categories(ctx context.Context) ([]db.Category, error) {
	return c.store.ListCategories(ctx)
}

// ProductList is the catalog grid plus the colors its rows reference.
type ProductList struct {
	Products []db.ListActiveProductsRow
	Colors   map[int64]db.Color
}

// Products lists active products, optionally within a category ("" = all).
func (c *Catalog) Products(ctx context.Context, categorySlug string, sort ProductSort) (*ProductList, error) {
	params := db.ListActiveProductsParams{}
	if categorySlug != "" {
		if _, err := c.store.GetCategoryBySlug(ctx, categorySlug); err != nil {
			if repo.IsNotFound(err) {
				return nil, newError(CodeCategoryNotFound, "category %q not found", categorySlug)
			}
			return nil, fmt.Errorf("get category: %w", err)
		}
		params.CategorySlug = &categorySlug
	}
	switch sort {
	case SortNew, SortPriceAsc, SortPriceDesc:
		params.OrderBy = string(sort)
	case SortFeatured, "":
		// Manual order (products.sort).
	default:
		return nil, &Error{Code: CodeValidation, Message: "unknown sort", Fields: []FieldError{{"sort", FieldInvalid}}}
	}

	rows, err := c.store.ListActiveProducts(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	colors, err := c.store.ListColors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list colors: %w", err)
	}
	byID := make(map[int64]db.Color, len(colors))
	for _, col := range colors {
		byID[col.ID] = col
	}
	return &ProductList{Products: rows, Colors: byID}, nil
}

// ProductDetails is everything the product page needs.
type ProductDetails struct {
	Product  db.Product
	Category db.Category
	// Active variants only, in size order.
	Variants  []db.ListVariantsByProductRow
	Images    []db.ProductImage
	SizeChart *domain.SizeChartData
}

// Product returns an active product by slug.
func (c *Catalog) Product(ctx context.Context, slug string) (*ProductDetails, error) {
	row, err := c.store.GetActiveProductBySlug(ctx, slug)
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, newError(CodeProductNotFound, "product %q not found", slug)
		}
		return nil, fmt.Errorf("get product: %w", err)
	}
	p := row.Product
	details := &ProductDetails{
		Product: p,
		Category: db.Category{
			ID: p.CategoryID, Slug: row.CategorySlug, NameRu: row.CategoryNameRu, NameKy: row.CategoryNameKy,
		},
	}

	variants, err := c.store.ListVariantsByProduct(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}
	for _, v := range variants {
		if v.ProductVariant.IsActive {
			details.Variants = append(details.Variants, v)
		}
	}

	if details.Images, err = c.store.ListProductImages(ctx, p.ID); err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	if p.SizeChartID != nil {
		chart, err := c.store.GetSizeChart(ctx, *p.SizeChartID)
		if err != nil {
			return nil, fmt.Errorf("get size chart: %w", err)
		}
		var data domain.SizeChartData
		if err := json.Unmarshal(chart.Data, &data); err != nil {
			return nil, fmt.Errorf("decode size chart %d: %w", chart.ID, err)
		}
		details.SizeChart = &data
	}
	return details, nil
}

// Settings are the public store settings.
type Settings struct {
	Contacts      domain.Contacts
	Pickup        domain.Pickup
	Hero          domain.Hero
	DeliveryZones []db.DeliveryZone
}

// PublicSettings returns contacts, pickup, hero texts and active delivery zones.
func (c *Catalog) PublicSettings(ctx context.Context) (*Settings, error) {
	rows, err := c.store.ListSettings(ctx, []string{domain.SettingContacts, domain.SettingPickup, domain.SettingHero})
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	s := &Settings{}
	targets := map[string]any{
		domain.SettingContacts: &s.Contacts,
		domain.SettingPickup:   &s.Pickup,
		domain.SettingHero:     &s.Hero,
	}
	for _, row := range rows {
		if err := json.Unmarshal(row.Value, targets[row.Key]); err != nil {
			return nil, fmt.Errorf("decode setting %s: %w", row.Key, err)
		}
	}
	if s.Contacts.Phones == nil {
		s.Contacts.Phones = []string{}
	}
	if s.DeliveryZones, err = c.store.ListActiveDeliveryZones(ctx); err != nil {
		return nil, fmt.Errorf("list delivery zones: %w", err)
	}
	return s, nil
}

func readPickup(ctx context.Context, q *db.Queries) (domain.Pickup, error) {
	var p domain.Pickup
	row, err := q.GetSetting(ctx, domain.SettingPickup)
	if err != nil {
		if repo.IsNotFound(err) {
			return p, nil
		}
		return p, fmt.Errorf("get pickup setting: %w", err)
	}
	if err := json.Unmarshal(row.Value, &p); err != nil {
		return p, fmt.Errorf("decode pickup setting: %w", err)
	}
	return p, nil
}
