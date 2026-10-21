-- name: AdminListCategories :many
SELECT
    c.*,
    (SELECT count(*) FROM products p WHERE p.category_id = c.id)::integer AS product_count
FROM categories c
ORDER BY c.sort, c.id;

-- name: GetCategory :one
SELECT * FROM categories
WHERE id = $1;

-- name: CreateCategory :one
INSERT INTO categories (slug, name_ru, name_ky, sort)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories
SET slug = $2, name_ru = $3, name_ky = $4, sort = $5
WHERE id = $1
RETURNING *;

-- name: DeleteCategory :execrows
DELETE FROM categories
WHERE id = $1;

-- name: GetColor :one
SELECT * FROM colors
WHERE id = $1;

-- name: CreateColor :one
INSERT INTO colors (name_ru, name_ky, hex)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateColor :one
UPDATE colors
SET name_ru = $2, name_ky = $3, hex = $4
WHERE id = $1
RETURNING *;

-- name: DeleteColor :execrows
DELETE FROM colors
WHERE id = $1;

-- name: ListSizeCharts :many
SELECT * FROM size_charts
ORDER BY name;

-- name: CreateSizeChart :one
INSERT INTO size_charts (name, data)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateSizeChart :one
UPDATE size_charts
SET name = $2, data = $3
WHERE id = $1
RETURNING *;

-- name: DeleteSizeChart :execrows
DELETE FROM size_charts
WHERE id = $1;

-- name: AdminListProducts :many
-- All products (active or not) with stock totals and the first photo.
SELECT
    p.*,
    coalesce((SELECT sum(v.stock) FROM product_variants v WHERE v.product_id = p.id AND v.is_active), 0)::integer AS total_stock,
    (SELECT count(*) FROM product_variants v WHERE v.product_id = p.id)::integer AS variant_count,
    coalesce((
        SELECT pi.s3_key FROM product_images pi
        WHERE pi.product_id = p.id
        ORDER BY pi.sort, pi.id
        LIMIT 1
    ), '')::text AS preview_s3_key
FROM products p
WHERE (sqlc.narg('category_id')::bigint IS NULL OR p.category_id = sqlc.narg('category_id')::bigint)
  AND (sqlc.narg('search')::text IS NULL
       OR p.title_ru ILIKE '%' || sqlc.narg('search')::text || '%'
       OR p.slug ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY p.sort, p.id;

-- name: CreateProduct :one
INSERT INTO products (
    slug, category_id, title_ru, title_ky, description_ru, description_ky,
    composition_ru, composition_ky, care_ru, care_ky, price, old_price,
    is_active, is_new, sort, size_chart_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16
)
RETURNING *;

-- name: UpdateProduct :one
UPDATE products
SET slug = $2, category_id = $3, title_ru = $4, title_ky = $5,
    description_ru = $6, description_ky = $7, composition_ru = $8, composition_ky = $9,
    care_ru = $10, care_ky = $11, price = $12, old_price = $13,
    is_active = $14, is_new = $15, sort = $16, size_chart_id = $17
WHERE id = $1
RETURNING *;

-- name: DeleteProduct :execrows
DELETE FROM products
WHERE id = $1;

-- name: GetVariant :one
SELECT
    sqlc.embed(v),
    p.slug AS product_slug
FROM product_variants v
JOIN products p ON p.id = v.product_id
WHERE v.id = $1;

-- name: CreateVariantIfMissing :execrows
-- Matrix creation: an existing product+color+size combination is skipped.
INSERT INTO product_variants (product_id, color_id, size, sku, stock, is_active)
VALUES ($1, $2, $3, $4, $5, true)
ON CONFLICT (product_id, color_id, size) DO NOTHING;

-- name: UpdateVariant :one
UPDATE product_variants
SET sku = $2, is_active = $3
WHERE id = $1
RETURNING *;

-- name: SetVariantStock :one
UPDATE product_variants
SET stock = $2
WHERE id = $1
RETURNING *;

-- name: DeleteVariant :execrows
DELETE FROM product_variants
WHERE id = $1;

-- name: GetProductImage :one
SELECT * FROM product_images
WHERE id = $1;

-- name: NextProductImageSort :one
SELECT (coalesce(max(sort), -1) + 1)::integer AS next_sort
FROM product_images
WHERE product_id = $1;

-- name: UpdateProductImage :one
UPDATE product_images
SET color_id = $2, alt_ru = $3, alt_ky = $4
WHERE id = $1
RETURNING *;

-- name: SetProductImageSort :execrows
UPDATE product_images
SET sort = $3
WHERE id = $1 AND product_id = $2;

-- name: DeleteProductImage :execrows
DELETE FROM product_images
WHERE id = $1;

-- name: AdminListDeliveryZones :many
SELECT * FROM delivery_zones
ORDER BY sort, id;

-- name: CreateDeliveryZone :one
INSERT INTO delivery_zones (name_ru, name_ky, price, free_from, is_active, sort)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateDeliveryZone :one
UPDATE delivery_zones
SET name_ru = $2, name_ky = $3, price = $4, free_from = $5, is_active = $6, sort = $7
WHERE id = $1
RETURNING *;

-- name: DeleteDeliveryZone :execrows
DELETE FROM delivery_zones
WHERE id = $1;

-- name: AdminListPromoCodes :many
SELECT * FROM promo_codes
ORDER BY created_at DESC, id DESC;

-- name: UpdatePromoCode :one
UPDATE promo_codes
SET code = $2, type = $3, value = $4, min_total = $5, max_uses = $6,
    valid_from = $7, valid_to = $8, is_active = $9
WHERE id = $1
RETURNING *;

-- name: DeletePromoCode :execrows
DELETE FROM promo_codes
WHERE id = $1;

-- name: ListAdmins :many
SELECT * FROM admins
ORDER BY id;

-- name: GetAdmin :one
SELECT * FROM admins
WHERE id = $1;

-- name: CreateAdmin :one
INSERT INTO admins (telegram_id, name, role, is_active)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateAdmin :one
UPDATE admins
SET name = $2, role = $3, is_active = $4
WHERE id = $1
RETURNING *;

-- name: DeleteAdmin :execrows
DELETE FROM admins
WHERE id = $1;

-- name: LockActiveOwners :many
-- Serializes changes that could leave the store without an active owner.
SELECT id FROM admins
WHERE role = 'owner' AND is_active
ORDER BY id
FOR UPDATE;
