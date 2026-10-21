package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/revalidate"
)

// MaxImageBytes caps uploaded photos.
const MaxImageBytes = 15 << 20

const maxAltLen = 200

// imageExtensions are the accepted photo types (sniffed from content, not the file name).
var imageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// ImageUpload is an uploaded photo with its metadata.
type ImageUpload struct {
	Data         []byte
	ColorID      *int64
	AltRu, AltKy string
}

// UploadImage stores the original in S3 and appends it to the product's photos.
func (a *Admin) UploadImage(ctx context.Context, productID int64, in ImageUpload) (db.ProductImage, error) {
	switch {
	case len(in.Data) == 0:
		return db.ProductImage{}, &Error{Code: CodeValidation, Message: "file is required", Fields: []FieldError{{"file", FieldRequired}}}
	case len(in.Data) > MaxImageBytes:
		return db.ProductImage{}, newError(CodeFileTooLarge, "photo exceeds %d MB", MaxImageBytes>>20)
	}
	contentType := http.DetectContentType(in.Data)
	ext, ok := imageExtensions[contentType]
	if !ok {
		return db.ProductImage{}, newError(CodeUnsupportedMediaType, "%s is not a JPEG, PNG or WebP image", contentType)
	}
	var f fields
	altRu := f.optionalText("alt_ru", in.AltRu, maxAltLen)
	altKy := f.optionalText("alt_ky", in.AltKy, maxAltLen)
	if err := f.err(); err != nil {
		return db.ProductImage{}, err
	}

	p, err := a.store.GetProductByID(ctx, productID)
	if err != nil {
		return db.ProductImage{}, lookupErr(err, "product")
	}
	key := fmt.Sprintf("products/%d/%s%s", productID, randomHex(16), ext)
	if err := a.objects.Put(ctx, key, contentType, in.Data); err != nil {
		return db.ProductImage{}, err
	}
	sort, err := a.store.NextProductImageSort(ctx, productID)
	if err != nil {
		a.deleteObject(ctx, key)
		return db.ProductImage{}, fmt.Errorf("next image sort: %w", err)
	}
	img, err := a.store.CreateProductImage(ctx, db.CreateProductImageParams{
		ProductID: productID, ColorID: in.ColorID, S3Key: key, Sort: sort, AltRu: altRu, AltKy: altKy,
	})
	if err != nil {
		a.deleteObject(ctx, key)
		if repo.IsForeignKeyViolation(err, "product_images_color_id_fkey") {
			return db.ProductImage{}, invalidField("color_id")
		}
		return db.ProductImage{}, fmt.Errorf("create image: %w", err)
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(p.Slug))
	return img, nil
}

// ReorderImages sets photo order; ids must list every photo of the product once.
func (a *Admin) ReorderImages(ctx context.Context, productID int64, ids []int64) (*AdminProduct, error) {
	p, err := a.GetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	current := map[int64]bool{}
	for _, img := range p.Images {
		current[img.ID] = true
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !current[id] || seen[id] {
			return nil, invalidField("image_ids")
		}
		seen[id] = true
	}
	if len(seen) != len(current) {
		return nil, invalidField("image_ids")
	}

	err = a.store.InTx(ctx, func(q *db.Queries) error {
		for i, id := range ids {
			if _, err := q.SetProductImageSort(ctx, db.SetProductImageSortParams{ID: id, ProductID: productID, Sort: int32(i)}); err != nil {
				return fmt.Errorf("set image sort: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.notify.Notify(revalidate.TagProducts, revalidate.ProductTag(p.Product.Slug))
	return a.GetProduct(ctx, productID)
}

// UpdateImage binds a photo to a color (nil = every color) and sets alt texts.
func (a *Admin) UpdateImage(ctx context.Context, id int64, colorID *int64, altRu, altKy string) (db.ProductImage, error) {
	var f fields
	altRu = f.optionalText("alt_ru", altRu, maxAltLen)
	altKy = f.optionalText("alt_ky", altKy, maxAltLen)
	if err := f.err(); err != nil {
		return db.ProductImage{}, err
	}
	img, err := a.store.UpdateProductImage(ctx, db.UpdateProductImageParams{ID: id, ColorID: colorID, AltRu: altRu, AltKy: altKy})
	switch {
	case repo.IsForeignKeyViolation(err, "product_images_color_id_fkey"):
		return db.ProductImage{}, invalidField("color_id")
	case err != nil:
		return db.ProductImage{}, lookupErr(err, "image")
	}
	a.notifyProduct(ctx, img.ProductID)
	return img, nil
}

// DeleteImage removes a photo and its S3 object.
func (a *Admin) DeleteImage(ctx context.Context, id int64) error {
	img, err := a.store.GetProductImage(ctx, id)
	if err != nil {
		return lookupErr(err, "image")
	}
	n, err := a.store.DeleteProductImage(ctx, id)
	if err := deleted(n, err, "image"); err != nil {
		return err
	}
	a.deleteObject(ctx, img.S3Key)
	a.notifyProduct(ctx, img.ProductID)
	return nil
}

// deleteObject removes an S3 object; failures only leave an orphan, so they are logged.
func (a *Admin) deleteObject(ctx context.Context, key string) {
	if err := a.objects.Delete(ctx, key); err != nil {
		a.log.WarnContext(ctx, "delete photo from storage", slog.String("key", key), slog.Any("error", err))
	}
}

func (a *Admin) notifyProduct(ctx context.Context, productID int64) {
	tags := []string{revalidate.TagProducts}
	if p, err := a.store.GetProductByID(ctx, productID); err == nil {
		tags = append(tags, revalidate.ProductTag(p.Slug))
	}
	a.notify.Notify(tags...)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}
