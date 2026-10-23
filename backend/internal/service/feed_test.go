package service

import (
	"slices"
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

func TestFeedItems(t *testing.T) {
	black, mauve := int64(1), int64(4)
	d := &ProductDetails{
		Product:  db.Product{Slug: "core", TitleRu: "Термофутболка Core", Price: 1990},
		Category: db.Category{NameRu: "Термофутболки"},
		Variants: []db.ListVariantsByProductRow{
			{ProductVariant: db.ProductVariant{Sku: "C-BLK-S", ColorID: black, Size: "S", Stock: 2}, ColorNameRu: "Чёрный"},
			{ProductVariant: db.ProductVariant{Sku: "C-MAU-S", ColorID: mauve, Size: "S"}, ColorNameRu: "Лиловый"},
		},
		Images: []db.ProductImage{
			{S3Key: "shared.jpg"},
			{S3Key: "mauve.jpg", ColorID: &mauve},
			{S3Key: "black.jpg", ColorID: &black},
		},
	}

	items := feedItems(d)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	b, m := items[0], items[1]
	if b.SKU != "C-BLK-S" || b.GroupID != "core" || b.Color != "Чёрный" || b.Category != "Термофутболки" || b.Stock != 2 {
		t.Errorf("black = %+v", b)
	}
	// Photos of the variant's color come first, then the shared ones.
	if !slices.Equal(b.ImageKeys, []string{"black.jpg", "shared.jpg"}) {
		t.Errorf("black images = %v", b.ImageKeys)
	}
	if !slices.Equal(m.ImageKeys, []string{"mauve.jpg", "shared.jpg"}) {
		t.Errorf("mauve images = %v", m.ImageKeys)
	}
	// Meta requires a description: an empty one falls back to the title.
	if b.Description != "Термофутболка Core" {
		t.Errorf("description = %q", b.Description)
	}
}
