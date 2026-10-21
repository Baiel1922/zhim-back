package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/revalidate"
)

var (
	slugRe      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	hexRe       = regexp.MustCompile(`^#[0-9A-F]{6}$`)
	skuRe       = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,63}$`)
	chartKeyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	nonSKUChars = regexp.MustCompile(`[^A-Z0-9]+`)
)

const (
	maxTitleLen       = 200
	maxLongTextLen    = 5000
	maxShortNameLen   = 100
	maxColorNameLen   = 50
	maxSizeLen        = 16
	maxMatrixColors   = 20
	maxMatrixSizes    = 20
	maxSizeChartLines = 50
)

func (f *fields) slug(field, v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		f.add(field, FieldRequired)
	case len(v) > 100 || !slugRe.MatchString(v):
		f.add(field, FieldInvalid)
	}
	return v
}

// ---------- Categories ----------

// CategoryInput is a category form.
type CategoryInput struct {
	Slug, NameRu, NameKy string
	Sort                 int32
}

func (in CategoryInput) normalize() (CategoryInput, error) {
	var f fields
	in.Slug = f.slug("slug", in.Slug)
	in.NameRu = f.text("name_ru", in.NameRu, maxShortNameLen)
	in.NameKy = f.text("name_ky", in.NameKy, maxShortNameLen)
	return in, f.err()
}

// ListCategories returns categories with product counts.
func (a *Admin) ListCategories(ctx context.Context) ([]db.AdminListCategoriesRow, error) {
	return a.store.AdminListCategories(ctx)
}

// CreateCategory adds a category.
func (a *Admin) CreateCategory(ctx context.Context, in CategoryInput) (db.Category, error) {
	in, err := in.normalize()
	if err != nil {
		return db.Category{}, err
	}
	c, err := a.store.CreateCategory(ctx, db.CreateCategoryParams{Slug: in.Slug, NameRu: in.NameRu, NameKy: in.NameKy, Sort: in.Sort})
	if repo.IsUniqueViolation(err, "categories_slug_key") {
		return db.Category{}, taken("slug")
	}
	if err != nil {
		return db.Category{}, fmt.Errorf("create category: %w", err)
	}
	a.notify.Notify(revalidate.TagCategories)
	return c, nil
}

// UpdateCategory replaces a category.
func (a *Admin) UpdateCategory(ctx context.Context, id int64, in CategoryInput) (db.Category, error) {
	in, err := in.normalize()
	if err != nil {
		return db.Category{}, err
	}
	c, err := a.store.UpdateCategory(ctx, db.UpdateCategoryParams{ID: id, Slug: in.Slug, NameRu: in.NameRu, NameKy: in.NameKy, Sort: in.Sort})
	switch {
	case repo.IsUniqueViolation(err, "categories_slug_key"):
		return db.Category{}, taken("slug")
	case err != nil:
		return db.Category{}, lookupErr(err, "category")
	}
	a.notify.Notify(revalidate.TagCategories, revalidate.TagProducts)
	return c, nil
}

// DeleteCategory removes an empty category.
func (a *Admin) DeleteCategory(ctx context.Context, id int64) error {
	n, err := a.store.DeleteCategory(ctx, id)
	if repo.IsForeignKeyViolation(err, "products_category_id_fkey") {
		return newError(CodeInUse, "category has products")
	}
	if err := deleted(n, err, "category"); err != nil {
		return err
	}
	a.notify.Notify(revalidate.TagCategories)
	return nil
}

// ---------- Colors ----------

// ColorInput is a color form.
type ColorInput struct {
	NameRu, NameKy, Hex string
}

func (in ColorInput) normalize() (ColorInput, error) {
	var f fields
	in.NameRu = f.text("name_ru", in.NameRu, maxColorNameLen)
	in.NameKy = f.text("name_ky", in.NameKy, maxColorNameLen)
	in.Hex = strings.ToUpper(strings.TrimSpace(in.Hex))
	if !hexRe.MatchString(in.Hex) {
		f.add("hex", FieldInvalid)
	}
	return in, f.err()
}

// ListColors returns all colors.
func (a *Admin) ListColors(ctx context.Context) ([]db.Color, error) {
	return a.store.ListColors(ctx)
}

// CreateColor adds a color.
func (a *Admin) CreateColor(ctx context.Context, in ColorInput) (db.Color, error) {
	in, err := in.normalize()
	if err != nil {
		return db.Color{}, err
	}
	c, err := a.store.CreateColor(ctx, db.CreateColorParams{NameRu: in.NameRu, NameKy: in.NameKy, Hex: in.Hex})
	if repo.IsUniqueViolation(err, "colors_name_ru_key") {
		return db.Color{}, taken("name_ru")
	}
	if err != nil {
		return db.Color{}, fmt.Errorf("create color: %w", err)
	}
	return c, nil
}

// UpdateColor replaces a color.
func (a *Admin) UpdateColor(ctx context.Context, id int64, in ColorInput) (db.Color, error) {
	in, err := in.normalize()
	if err != nil {
		return db.Color{}, err
	}
	c, err := a.store.UpdateColor(ctx, db.UpdateColorParams{ID: id, NameRu: in.NameRu, NameKy: in.NameKy, Hex: in.Hex})
	switch {
	case repo.IsUniqueViolation(err, "colors_name_ru_key"):
		return db.Color{}, taken("name_ru")
	case err != nil:
		return db.Color{}, lookupErr(err, "color")
	}
	a.notify.Notify(revalidate.TagProducts)
	return c, nil
}

// DeleteColor removes a color no variant uses.
func (a *Admin) DeleteColor(ctx context.Context, id int64) error {
	n, err := a.store.DeleteColor(ctx, id)
	if repo.IsForeignKeyViolation(err, "product_variants_color_id_fkey") {
		return newError(CodeInUse, "color is used by variants")
	}
	if err := deleted(n, err, "color"); err != nil {
		return err
	}
	a.notify.Notify(revalidate.TagProducts)
	return nil
}

// ---------- Size charts ----------

// SizeChartInput is a size chart form.
type SizeChartInput struct {
	Name string
	Data domain.SizeChartData
}

func (in SizeChartInput) normalize() (SizeChartInput, error) {
	var f fields
	in.Name = f.text("name", in.Name, maxShortNameLen)
	d := &in.Data
	d.Unit = f.text("data.unit", d.Unit, 10)
	if len(d.Columns) > maxSizeChartLines || len(d.Rows) > maxSizeChartLines || len(d.Fit) > maxSizeChartLines {
		f.add("data", FieldTooMany)
	}
	keys := map[string]bool{}
	for i := range d.Columns {
		c := &d.Columns[i]
		c.Key = strings.TrimSpace(c.Key)
		if !chartKeyRe.MatchString(c.Key) || keys[c.Key] {
			f.add(fmt.Sprintf("data.columns[%d].key", i), FieldInvalid)
		}
		keys[c.Key] = true
		c.NameRu = f.text(fmt.Sprintf("data.columns[%d].name_ru", i), c.NameRu, maxShortNameLen)
		c.NameKy = f.text(fmt.Sprintf("data.columns[%d].name_ky", i), c.NameKy, maxShortNameLen)
	}
	for i := range d.Rows {
		r := &d.Rows[i]
		r.Size = f.text(fmt.Sprintf("data.rows[%d].size", i), r.Size, maxSizeLen)
		for k := range r.Values {
			if !keys[k] {
				f.add(fmt.Sprintf("data.rows[%d].values.%s", i, k), FieldInvalid)
			}
		}
	}
	for i := range d.Fit {
		r := &d.Fit[i]
		r.Size = f.text(fmt.Sprintf("data.fit[%d].size", i), r.Size, maxSizeLen)
		if r.HeightMin <= 0 || r.HeightMax < r.HeightMin || r.WeightMin <= 0 || r.WeightMax < r.WeightMin {
			f.add(fmt.Sprintf("data.fit[%d]", i), FieldInvalid)
		}
	}
	return in, f.err()
}

// ListSizeCharts returns all size charts.
func (a *Admin) ListSizeCharts(ctx context.Context) ([]db.SizeChart, error) {
	return a.store.ListSizeCharts(ctx)
}

// CreateSizeChart adds a size chart.
func (a *Admin) CreateSizeChart(ctx context.Context, in SizeChartInput) (db.SizeChart, error) {
	in, err := in.normalize()
	if err != nil {
		return db.SizeChart{}, err
	}
	data, err := json.Marshal(in.Data)
	if err != nil {
		return db.SizeChart{}, fmt.Errorf("encode size chart: %w", err)
	}
	c, err := a.store.CreateSizeChart(ctx, db.CreateSizeChartParams{Name: in.Name, Data: data})
	if repo.IsUniqueViolation(err, "size_charts_name_key") {
		return db.SizeChart{}, taken("name")
	}
	if err != nil {
		return db.SizeChart{}, fmt.Errorf("create size chart: %w", err)
	}
	return c, nil
}

// UpdateSizeChart replaces a size chart.
func (a *Admin) UpdateSizeChart(ctx context.Context, id int64, in SizeChartInput) (db.SizeChart, error) {
	in, err := in.normalize()
	if err != nil {
		return db.SizeChart{}, err
	}
	data, err := json.Marshal(in.Data)
	if err != nil {
		return db.SizeChart{}, fmt.Errorf("encode size chart: %w", err)
	}
	c, err := a.store.UpdateSizeChart(ctx, db.UpdateSizeChartParams{ID: id, Name: in.Name, Data: data})
	switch {
	case repo.IsUniqueViolation(err, "size_charts_name_key"):
		return db.SizeChart{}, taken("name")
	case err != nil:
		return db.SizeChart{}, lookupErr(err, "size chart")
	}
	a.notify.Notify(revalidate.TagProducts)
	return c, nil
}

// DeleteSizeChart removes a size chart; products keep no chart.
func (a *Admin) DeleteSizeChart(ctx context.Context, id int64) error {
	n, err := a.store.DeleteSizeChart(ctx, id)
	if err := deleted(n, err, "size chart"); err != nil {
		return err
	}
	a.notify.Notify(revalidate.TagProducts)
	return nil
}

// ---------- Products ----------

// ProductInput is a product form (both languages).
type ProductInput struct {
	Slug                         string
	CategoryID                   int64
	TitleRu, TitleKy             string
	DescriptionRu, DescriptionKy string
	CompositionRu, CompositionKy string
	CareRu, CareKy               string
	Price                        int32
	OldPrice                     *int32
	IsActive, IsNew              bool
	Sort                         int32
	SizeChartID                  *int64
}

func (in ProductInput) normalize() (ProductInput, error) {
	var f fields
	in.Slug = f.slug("slug", in.Slug)
	in.TitleRu = f.text("title_ru", in.TitleRu, maxTitleLen)
	in.TitleKy = f.text("title_ky", in.TitleKy, maxTitleLen)
	in.DescriptionRu = f.optionalText("description_ru", in.DescriptionRu, maxLongTextLen)
	in.DescriptionKy = f.optionalText("description_ky", in.DescriptionKy, maxLongTextLen)
	in.CompositionRu = f.optionalText("composition_ru", in.CompositionRu, maxLongTextLen)
	in.CompositionKy = f.optionalText("composition_ky", in.CompositionKy, maxLongTextLen)
	in.CareRu = f.optionalText("care_ru", in.CareRu, maxLongTextLen)
	in.CareKy = f.optionalText("care_ky", in.CareKy, maxLongTextLen)
	if in.CategoryID <= 0 {
		f.add("category_id", FieldRequired)
	}
	if in.Price <= 0 {
		f.add("price", FieldInvalid)
	}
	if in.OldPrice != nil && *in.OldPrice <= in.Price {
		f.add("old_price", FieldInvalid)
	}
	return in, f.err()
}

// productWriteErr maps constraint errors of product inserts/updates.
func productWriteErr(err error) error {
	switch {
	case repo.IsUniqueViolation(err, "products_slug_key"):
		return taken("slug")
	case repo.IsForeignKeyViolation(err, "products_category_id_fkey"):
		return invalidField("category_id")
	case repo.IsForeignKeyViolation(err, "products_size_chart_id_fkey"):
		return invalidField("size_chart_id")
	}
	return err
}

// ProductFilter narrows the admin product list.
type ProductFilter struct {
	CategoryID *int64
	Query      string
}

// ListProducts returns all products, hidden ones included.
func (a *Admin) ListProducts(ctx context.Context, f ProductFilter) ([]db.AdminListProductsRow, error) {
	params := db.AdminListProductsParams{CategoryID: f.CategoryID}
	if q := strings.TrimSpace(f.Query); q != "" {
		escaped := escapeLike(q)
		params.Search = &escaped
	}
	return a.store.AdminListProducts(ctx, params)
}

// AdminProduct is a product with all of its variants and images.
type AdminProduct struct {
	Product  db.Product
	Variants []db.ProductVariant
	Images   []db.ProductImage
}

// GetProduct returns a product for editing.
func (a *Admin) GetProduct(ctx context.Context, id int64) (*AdminProduct, error) {
	p, err := a.store.GetProductByID(ctx, id)
	if err != nil {
		return nil, lookupErr(err, "product")
	}
	out := &AdminProduct{Product: p}
	rows, err := a.store.ListVariantsByProduct(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}
	for _, r := range rows {
		out.Variants = append(out.Variants, r.ProductVariant)
	}
	if out.Images, err = a.store.ListProductImages(ctx, id); err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	return out, nil
}

// CreateProduct adds a product (no variants yet).
func (a *Admin) CreateProduct(ctx context.Context, in ProductInput) (*AdminProduct, error) {
	in, err := in.normalize()
	if err != nil {
		return nil, err
	}
	p, err := a.store.CreateProduct(ctx, db.CreateProductParams{
		Slug: in.Slug, CategoryID: in.CategoryID, TitleRu: in.TitleRu, TitleKy: in.TitleKy,
		DescriptionRu: in.DescriptionRu, DescriptionKy: in.DescriptionKy,
		CompositionRu: in.CompositionRu, CompositionKy: in.CompositionKy,
		CareRu: in.CareRu, CareKy: in.CareKy, Price: in.Price, OldPrice: in.OldPrice,
		IsActive: in.IsActive, IsNew: in.IsNew, Sort: in.Sort, SizeChartID: in.SizeChartID,
	})
	if err != nil {
		return nil, fmt.Errorf("create product: %w", productWriteErr(err))
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(p.Slug))
	return &AdminProduct{Product: p, Variants: []db.ProductVariant{}, Images: []db.ProductImage{}}, nil
}

// UpdateProduct replaces product fields.
func (a *Admin) UpdateProduct(ctx context.Context, id int64, in ProductInput) (*AdminProduct, error) {
	in, err := in.normalize()
	if err != nil {
		return nil, err
	}
	old, err := a.store.GetProductByID(ctx, id)
	if err != nil {
		return nil, lookupErr(err, "product")
	}
	p, err := a.store.UpdateProduct(ctx, db.UpdateProductParams{
		ID: id, Slug: in.Slug, CategoryID: in.CategoryID, TitleRu: in.TitleRu, TitleKy: in.TitleKy,
		DescriptionRu: in.DescriptionRu, DescriptionKy: in.DescriptionKy,
		CompositionRu: in.CompositionRu, CompositionKy: in.CompositionKy,
		CareRu: in.CareRu, CareKy: in.CareKy, Price: in.Price, OldPrice: in.OldPrice,
		IsActive: in.IsActive, IsNew: in.IsNew, Sort: in.Sort, SizeChartID: in.SizeChartID,
	})
	if err != nil {
		if repo.IsNotFound(err) {
			return nil, notFound("product")
		}
		return nil, fmt.Errorf("update product: %w", productWriteErr(err))
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(old.Slug), revalidate.ProductTag(p.Slug))
	return a.GetProduct(ctx, id)
}

// DeleteProduct removes a product with its variants and photos.
func (a *Admin) DeleteProduct(ctx context.Context, id int64) error {
	p, err := a.store.GetProductByID(ctx, id)
	if err != nil {
		return lookupErr(err, "product")
	}
	images, err := a.store.ListProductImages(ctx, id)
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}
	n, err := a.store.DeleteProduct(ctx, id)
	if err := deleted(n, err, "product"); err != nil {
		return err
	}
	for _, img := range images {
		a.deleteObject(ctx, img.S3Key)
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(p.Slug))
	return nil
}

// ---------- Variants ----------

// VariantMatrixInput creates every size × color combination.
type VariantMatrixInput struct {
	ColorIDs []int64
	Sizes    []string
	Stock    int32
}

// CreateVariants adds missing size × color variants with generated SKUs.
func (a *Admin) CreateVariants(ctx context.Context, productID int64, in VariantMatrixInput) (*AdminProduct, error) {
	var f fields
	switch {
	case len(in.ColorIDs) == 0:
		f.add("color_ids", FieldRequired)
	case len(in.ColorIDs) > maxMatrixColors:
		f.add("color_ids", FieldTooMany)
	}
	var sizes []string
	seen := map[string]bool{}
	for i, s := range in.Sizes {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || utf8.RuneCountInString(s) > maxSizeLen || nonSKUChars.ReplaceAllString(s, "") == "" {
			f.add(fmt.Sprintf("sizes[%d]", i), FieldInvalid)
			continue
		}
		if !seen[s] {
			seen[s] = true
			sizes = append(sizes, s)
		}
	}
	switch {
	case len(in.Sizes) == 0:
		f.add("sizes", FieldRequired)
	case len(sizes) > maxMatrixSizes:
		f.add("sizes", FieldTooMany)
	}
	if in.Stock < 0 {
		f.add("stock", FieldInvalid)
	}
	if err := f.err(); err != nil {
		return nil, err
	}

	p, err := a.store.GetProductByID(ctx, productID)
	if err != nil {
		return nil, lookupErr(err, "product")
	}
	colors, err := a.store.ListColors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list colors: %w", err)
	}
	known := map[int64]bool{}
	for _, c := range colors {
		known[c.ID] = true
	}
	for i, id := range in.ColorIDs {
		if !known[id] {
			f.add(fmt.Sprintf("color_ids[%d]", i), FieldInvalid)
		}
	}
	if err := f.err(); err != nil {
		return nil, err
	}

	err = a.store.InTx(ctx, func(q *db.Queries) error {
		for _, colorID := range in.ColorIDs {
			for _, size := range sizes {
				_, err := q.CreateVariantIfMissing(ctx, db.CreateVariantIfMissingParams{
					ProductID: productID, ColorID: colorID, Size: size,
					Sku:   fmt.Sprintf("ZH-%d-%d-%s", productID, colorID, strings.Trim(nonSKUChars.ReplaceAllString(size, "-"), "-")),
					Stock: in.Stock,
				})
				if repo.IsUniqueViolation(err, "product_variants_sku_key") {
					return taken("sku")
				}
				if err != nil {
					return fmt.Errorf("create variant: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(p.Slug))
	return a.GetProduct(ctx, productID)
}

// UpdateVariant changes a variant's SKU and visibility.
func (a *Admin) UpdateVariant(ctx context.Context, id int64, sku string, isActive bool) (db.ProductVariant, error) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	if !skuRe.MatchString(sku) {
		return db.ProductVariant{}, invalidField("sku")
	}
	v, err := a.store.GetVariant(ctx, id)
	if err != nil {
		return db.ProductVariant{}, lookupErr(err, "variant")
	}
	updated, err := a.store.UpdateVariant(ctx, db.UpdateVariantParams{ID: id, Sku: sku, IsActive: isActive})
	switch {
	case repo.IsUniqueViolation(err, "product_variants_sku_key"):
		return db.ProductVariant{}, taken("sku")
	case err != nil:
		return db.ProductVariant{}, lookupErr(err, "variant")
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(v.ProductSlug))
	return updated, nil
}

// SetVariantStock sets the stock of a variant (managers may do this).
func (a *Admin) SetVariantStock(ctx context.Context, id int64, stock int32) (db.ProductVariant, error) {
	if stock < 0 {
		return db.ProductVariant{}, invalidField("stock")
	}
	v, err := a.store.GetVariant(ctx, id)
	if err != nil {
		return db.ProductVariant{}, lookupErr(err, "variant")
	}
	updated, err := a.store.SetVariantStock(ctx, db.SetVariantStockParams{ID: id, Stock: stock})
	if err != nil {
		return db.ProductVariant{}, lookupErr(err, "variant")
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(v.ProductSlug))
	return updated, nil
}

// DeleteVariant removes a variant; past order items keep their snapshots.
func (a *Admin) DeleteVariant(ctx context.Context, id int64) error {
	v, err := a.store.GetVariant(ctx, id)
	if err != nil {
		return lookupErr(err, "variant")
	}
	n, err := a.store.DeleteVariant(ctx, id)
	if err := deleted(n, err, "variant"); err != nil {
		return err
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(v.ProductSlug))
	return nil
}

// escapeLike escapes LIKE wildcards so user input matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
