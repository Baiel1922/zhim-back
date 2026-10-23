package http

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/http/oapi"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

const (
	feedTTL = 5 * time.Minute
	// Meta needs at least 500×500; 1080 gives 1080×1350.
	feedImageWidth = 1080
	// Meta accepts up to 20 additional images.
	feedMaxExtraImages = 10
	feedBrand          = "ZHIM"
	// Google product taxonomy, which Meta also uses.
	feedCategory = "Apparel & Accessories > Clothing > Activewear"
)

// feedCache keeps the rendered feed for feedTTL: Meta fetches it on a
// schedule, and building it reads every product.
type feedCache struct {
	mu      sync.Mutex
	body    []byte
	expires time.Time
}

// GetMetaFeed serves the product catalog feed for Meta Commerce Manager.
func (h *Handlers) GetMetaFeed(ctx context.Context, _ oapi.GetMetaFeedRequestObject) (oapi.GetMetaFeedResponseObject, error) {
	h.feed.mu.Lock()
	defer h.feed.mu.Unlock()
	if h.feed.body == nil || time.Now().After(h.feed.expires) {
		items, err := h.catalog.FeedItems(ctx)
		if err != nil {
			return nil, err
		}
		body, err := renderMetaFeed(items, h.images, h.webURL)
		if err != nil {
			return nil, err
		}
		h.feed.body, h.feed.expires = body, time.Now().Add(feedTTL)
	}
	return oapi.GetMetaFeed200ApplicationXMLResponse{
		Body:          bytes.NewReader(h.feed.body),
		ContentLength: int64(len(h.feed.body)),
	}, nil
}

type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	NSG     string     `xml:"xmlns:g,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link"`
	Description string     `xml:"description"`
	Items       []feedItem `xml:"item"`
}

type feedItem struct {
	ID               string   `xml:"g:id"`
	ItemGroupID      string   `xml:"g:item_group_id"`
	Title            string   `xml:"g:title"`
	Description      string   `xml:"g:description"`
	Link             string   `xml:"g:link"`
	ImageLink        string   `xml:"g:image_link"`
	AdditionalImages []string `xml:"g:additional_image_link"`
	Availability     string   `xml:"g:availability"`
	Condition        string   `xml:"g:condition"`
	Price            string   `xml:"g:price"`
	SalePrice        string   `xml:"g:sale_price,omitempty"`
	Brand            string   `xml:"g:brand"`
	GoogleCategory   string   `xml:"g:google_product_category"`
	ProductType      string   `xml:"g:product_type"`
	Color            string   `xml:"g:color"`
	Size             string   `xml:"g:size"`
	Gender           string   `xml:"g:gender"`
	AgeGroup         string   `xml:"g:age_group"`
}

// renderMetaFeed builds the RSS document. Variants without photos are left
// out: Meta rejects items without image_link.
func renderMetaFeed(items []service.FeedItem, images *imgproxy.Signer, webURL string) ([]byte, error) {
	webURL = strings.TrimRight(webURL, "/")
	doc := rss{
		Version: "2.0",
		NSG:     "http://base.google.com/ns/1.0",
		Channel: rssChannel{Title: feedBrand, Link: webURL, Description: "ZHIM — мужская одежда для фитнеса"},
	}
	for _, it := range items {
		if len(it.ImageKeys) == 0 {
			continue
		}
		item := feedItem{
			ID:             it.SKU,
			ItemGroupID:    it.GroupID,
			Title:          it.Title,
			Description:    it.Description,
			Link:           webURL + "/ru/p/" + it.Slug,
			ImageLink:      images.URL(it.ImageKeys[0], feedImageWidth),
			Availability:   "out of stock",
			Condition:      "new",
			Price:          feedPrice(it.Price),
			Brand:          feedBrand,
			GoogleCategory: feedCategory,
			ProductType:    it.Category,
			Color:          it.Color,
			Size:           it.Size,
			Gender:         "male",
			AgeGroup:       "adult",
		}
		if it.Stock > 0 {
			item.Availability = "in stock"
		}
		// A sale shows the old price struck through: price is the regular one.
		if it.OldPrice != nil && *it.OldPrice > it.Price {
			item.Price, item.SalePrice = feedPrice(*it.OldPrice), feedPrice(it.Price)
		}
		for _, key := range it.ImageKeys[1:min(len(it.ImageKeys), feedMaxExtraImages+1)] {
			item.AdditionalImages = append(item.AdditionalImages, images.URL(key, feedImageWidth))
		}
		doc.Channel.Items = append(doc.Channel.Items, item)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode feed: %w", err)
	}
	return buf.Bytes(), nil
}

func feedPrice(som int32) string {
	return fmt.Sprintf("%d.00 KGS", som)
}
