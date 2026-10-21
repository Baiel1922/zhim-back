// Package seed loads development test data. It is idempotent: rows are upserted
// by natural keys (slug, sku, name, telegram_id), so re-running resets them to
// the seed values without duplicating anything.
package seed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
)

// Options configures the seed.
type Options struct {
	// OwnerTelegramID is the Telegram user id of the first owner admin; 0 skips it.
	OwnerTelegramID int64
	OwnerName       string
}

// Result summarizes what was written.
type Result struct {
	Categories    int
	Colors        int
	Products      int
	Variants      int
	DeliveryZones int
	Settings      int
	OwnerSeeded   bool
}

// Run writes all seed data in a single transaction.
func Run(ctx context.Context, store *repo.Store, opts Options) (Result, error) {
	var res Result
	err := store.InTx(ctx, func(q *db.Queries) error {
		var err error
		res, err = run(ctx, q, opts)
		return err
	})
	return res, err
}

func run(ctx context.Context, q *db.Queries, opts Options) (Result, error) {
	var res Result

	categoryIDs := make(map[string]int64, len(categories))
	for i, c := range categories {
		row, err := q.UpsertCategory(ctx, db.UpsertCategoryParams{
			Slug: c.slug, NameRu: c.nameRu, NameKy: c.nameKy, Sort: int32(i * 10),
		})
		if err != nil {
			return res, fmt.Errorf("category %s: %w", c.slug, err)
		}
		categoryIDs[c.slug] = row.ID
		res.Categories++
	}

	colorIDs := make(map[string]int64, len(colors))
	for _, c := range colors {
		row, err := q.UpsertColor(ctx, db.UpsertColorParams{NameRu: c.nameRu, NameKy: c.nameKy, Hex: c.hex})
		if err != nil {
			return res, fmt.Errorf("color %s: %w", c.code, err)
		}
		colorIDs[c.code] = row.ID
		res.Colors++
	}

	chartJSON, err := json.Marshal(sizeChart)
	if err != nil {
		return res, fmt.Errorf("marshal size chart: %w", err)
	}
	chart, err := q.UpsertSizeChart(ctx, db.UpsertSizeChartParams{Name: sizeChartName, Data: chartJSON})
	if err != nil {
		return res, fmt.Errorf("size chart: %w", err)
	}

	for i, p := range products {
		row, err := q.UpsertProduct(ctx, db.UpsertProductParams{
			Slug:          p.slug,
			CategoryID:    categoryIDs[p.category],
			TitleRu:       p.titleRu,
			TitleKy:       p.titleKy,
			DescriptionRu: p.descriptionRu,
			DescriptionKy: p.descriptionKy,
			CompositionRu: compositionRu,
			CompositionKy: compositionKy,
			CareRu:        careRu,
			CareKy:        careKy,
			Price:         p.price,
			OldPrice:      p.oldPrice,
			IsActive:      true,
			IsNew:         p.isNew,
			Sort:          int32(i * 10),
			SizeChartID:   &chart.ID,
		})
		if err != nil {
			return res, fmt.Errorf("product %s: %w", p.slug, err)
		}
		res.Products++

		for _, colorCode := range p.colors {
			for si, size := range sizes {
				stock := baseStock[si]
				if v, ok := p.stockOverrides[colorCode+"-"+size]; ok {
					stock = v
				}
				if _, err := q.UpsertVariant(ctx, db.UpsertVariantParams{
					ProductID: row.ID,
					ColorID:   colorIDs[colorCode],
					Size:      size,
					Sku:       fmt.Sprintf("ZH-%s-%s-%s", p.code, colorCode, size),
					Stock:     stock,
					IsActive:  true,
				}); err != nil {
					return res, fmt.Errorf("variant %s %s %s: %w", p.slug, colorCode, size, err)
				}
				res.Variants++
			}
		}
	}

	for i, z := range deliveryZones {
		freeFrom := z.freeFrom
		if _, err := q.UpsertDeliveryZone(ctx, db.UpsertDeliveryZoneParams{
			NameRu: z.nameRu, NameKy: z.nameKy, Price: z.price, FreeFrom: &freeFrom, IsActive: true, Sort: int32(i * 10),
		}); err != nil {
			return res, fmt.Errorf("delivery zone %s: %w", z.nameRu, err)
		}
		res.DeliveryZones++
	}

	settings := map[string]any{
		domain.SettingContacts: contacts,
		domain.SettingPickup:   pickup,
		domain.SettingHero:     hero,
	}
	for key, value := range settings {
		raw, err := json.Marshal(value)
		if err != nil {
			return res, fmt.Errorf("marshal setting %s: %w", key, err)
		}
		if _, err := q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: raw}); err != nil {
			return res, fmt.Errorf("setting %s: %w", key, err)
		}
		res.Settings++
	}

	if opts.OwnerTelegramID != 0 {
		name := opts.OwnerName
		if name == "" {
			name = "Owner"
		}
		if _, err := q.UpsertAdmin(ctx, db.UpsertAdminParams{
			TelegramID: opts.OwnerTelegramID, Name: name, Role: db.AdminRoleOwner, IsActive: true,
		}); err != nil {
			return res, fmt.Errorf("owner admin: %w", err)
		}
		res.OwnerSeeded = true
	}

	return res, nil
}
