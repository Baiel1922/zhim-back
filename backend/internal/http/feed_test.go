package http

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

func TestRenderMetaFeed(t *testing.T) {
	signer, err := imgproxy.NewSigner("https://img.zhim.kg", "zhim", "0011", "2233")
	if err != nil {
		t.Fatal(err)
	}
	old := int32(2490)
	keys := make([]string, 15)
	for i := range keys {
		keys[i] = "p/" + string(rune('a'+i)) + ".jpg"
	}
	items := []service.FeedItem{
		{SKU: "ZH-A-S", GroupID: "shorts", Slug: "shorts", Title: "Шорты", Description: "D", Category: "Шорты",
			Color: "Чёрный", Size: "S", Price: 1990, OldPrice: &old, Stock: 3, ImageKeys: keys},
		{SKU: "ZH-A-M", GroupID: "shorts", Slug: "shorts", Title: "Шорты", Description: "D", Category: "Шорты",
			Color: "Чёрный", Size: "M", Price: 1990, Stock: 0, ImageKeys: keys[:1]},
		// No photos: Meta would reject it.
		{SKU: "ZH-B-S", GroupID: "tee", Slug: "tee", Title: "Футболка", Price: 990, Stock: 5},
	}

	body, err := renderMetaFeed(items, signer, "https://zhim.kg/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `xmlns:g="http://base.google.com/ns/1.0"`) {
		t.Fatalf("namespace missing:\n%s", body)
	}

	var doc struct {
		Items []struct {
			ID           string   `xml:"id"`
			Group        string   `xml:"item_group_id"`
			Link         string   `xml:"link"`
			Image        string   `xml:"image_link"`
			Extra        []string `xml:"additional_image_link"`
			Availability string   `xml:"availability"`
			Price        string   `xml:"price"`
			SalePrice    string   `xml:"sale_price"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("items = %d, want 2 (the one without photos is skipped)", len(doc.Items))
	}
	sale, out := doc.Items[0], doc.Items[1]
	if sale.ID != "ZH-A-S" || sale.Group != "shorts" || sale.Link != "https://zhim.kg/ru/p/shorts" {
		t.Errorf("ids/link = %+v", sale)
	}
	if sale.Price != "2490.00 KGS" || sale.SalePrice != "1990.00 KGS" {
		t.Errorf("sale prices = %q / %q", sale.Price, sale.SalePrice)
	}
	if sale.Availability != "in stock" || out.Availability != "out of stock" {
		t.Errorf("availability = %q / %q", sale.Availability, out.Availability)
	}
	if !strings.HasPrefix(sale.Image, "https://img.zhim.kg/") || !strings.Contains(sale.Image, "rs:fill:1080:1350") {
		t.Errorf("image = %q", sale.Image)
	}
	if len(sale.Extra) != feedMaxExtraImages || len(out.Extra) != 0 {
		t.Errorf("extra images = %d / %d", len(sale.Extra), len(out.Extra))
	}
	if out.Price != "1990.00 KGS" || out.SalePrice != "" {
		t.Errorf("regular price = %q / %q", out.Price, out.SalePrice)
	}
}
