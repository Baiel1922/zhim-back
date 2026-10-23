package service

import (
	"context"
	"fmt"
)

// FeedItem is one variant in the ad catalog feed (Russian texts).
type FeedItem struct {
	// SKU is the item ID; the pixels send the same value as content_ids.
	SKU string
	// GroupID ties the variants of one product together (the product slug).
	GroupID     string
	Slug        string
	Title       string
	Description string
	Category    string
	Color       string
	Size        string
	Price       int32
	OldPrice    *int32
	Stock       int32
	// ImageKeys are S3 keys: photos of the variant's color first, then photos
	// shared by all colors.
	ImageKeys []string
}

// FeedItems lists every active variant of every active product.
func (c *Catalog) FeedItems(ctx context.Context) ([]FeedItem, error) {
	list, err := c.Products(ctx, "", SortFeatured)
	if err != nil {
		return nil, err
	}
	var items []FeedItem
	for _, row := range list.Products {
		d, err := c.Product(ctx, row.Slug)
		if err != nil {
			return nil, fmt.Errorf("feed product %q: %w", row.Slug, err)
		}
		items = append(items, feedItems(d)...)
	}
	return items, nil
}

func feedItems(d *ProductDetails) []FeedItem {
	p := d.Product
	description := p.DescriptionRu
	if description == "" {
		description = p.TitleRu
	}
	items := make([]FeedItem, 0, len(d.Variants))
	for _, row := range d.Variants {
		v := row.ProductVariant
		items = append(items, FeedItem{
			SKU:         v.Sku,
			GroupID:     p.Slug,
			Slug:        p.Slug,
			Title:       p.TitleRu,
			Description: description,
			Category:    d.Category.NameRu,
			Color:       row.ColorNameRu,
			Size:        v.Size,
			Price:       p.Price,
			OldPrice:    p.OldPrice,
			Stock:       v.Stock,
			ImageKeys:   imagesForColor(d, v.ColorID),
		})
	}
	return items
}

func imagesForColor(d *ProductDetails, colorID int64) []string {
	var own, shared []string
	for _, img := range d.Images {
		switch {
		case img.ColorID == nil:
			shared = append(shared, img.S3Key)
		case *img.ColorID == colorID:
			own = append(own, img.S3Key)
		}
	}
	return append(own, shared...)
}
