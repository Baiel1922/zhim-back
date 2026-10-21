-- name: ListActiveProducts :many
-- Storefront catalog. Sizes and colors include only active variants in stock;
-- preview_s3_keys holds the first two images (main + hover).
SELECT
    p.id,
    p.slug,
    p.category_id,
    c.slug AS category_slug,
    p.title_ru,
    p.title_ky,
    p.price,
    p.old_price,
    p.is_new,
    p.sort,
    p.created_at,
    (
        SELECT coalesce(array_agg(s.size ORDER BY size_rank(s.size), s.size), '{}')
        FROM (
            SELECT DISTINCT v.size
            FROM product_variants v
            WHERE v.product_id = p.id AND v.is_active AND v.stock > 0
        ) s
    )::text[] AS available_sizes,
    (
        SELECT coalesce(array_agg(DISTINCT v.color_id ORDER BY v.color_id), '{}')
        FROM product_variants v
        WHERE v.product_id = p.id AND v.is_active AND v.stock > 0
    )::bigint[] AS available_color_ids,
    (
        SELECT coalesce(array_agg(i.s3_key ORDER BY i.sort, i.id), '{}')
        FROM (
            SELECT pi.s3_key, pi.sort, pi.id
            FROM product_images pi
            WHERE pi.product_id = p.id
            ORDER BY pi.sort, pi.id
            LIMIT 2
        ) i
    )::text[] AS preview_s3_keys
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.is_active
  AND (sqlc.narg('category_slug')::text IS NULL OR c.slug = sqlc.narg('category_slug')::text)
ORDER BY
    CASE WHEN sqlc.arg('order_by')::text = 'price_asc' THEN p.price END,
    CASE WHEN sqlc.arg('order_by')::text = 'price_desc' THEN p.price END DESC,
    CASE WHEN sqlc.arg('order_by')::text = 'new' THEN p.created_at END DESC,
    p.sort,
    p.id;

-- name: GetProductBySlug :one
SELECT * FROM products
WHERE slug = $1;

-- name: GetActiveProductBySlug :one
SELECT
    sqlc.embed(p),
    c.slug    AS category_slug,
    c.name_ru AS category_name_ru,
    c.name_ky AS category_name_ky
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.slug = $1 AND p.is_active;

-- name: GetProductByID :one
SELECT * FROM products
WHERE id = $1;

-- name: UpsertProduct :one
INSERT INTO products (
    slug, category_id, title_ru, title_ky, description_ru, description_ky,
    composition_ru, composition_ky, care_ru, care_ky, price, old_price,
    is_active, is_new, sort, size_chart_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16
)
ON CONFLICT (slug) DO UPDATE
SET category_id    = EXCLUDED.category_id,
    title_ru       = EXCLUDED.title_ru,
    title_ky       = EXCLUDED.title_ky,
    description_ru = EXCLUDED.description_ru,
    description_ky = EXCLUDED.description_ky,
    composition_ru = EXCLUDED.composition_ru,
    composition_ky = EXCLUDED.composition_ky,
    care_ru        = EXCLUDED.care_ru,
    care_ky        = EXCLUDED.care_ky,
    price          = EXCLUDED.price,
    old_price      = EXCLUDED.old_price,
    is_active      = EXCLUDED.is_active,
    is_new         = EXCLUDED.is_new,
    sort           = EXCLUDED.sort,
    size_chart_id  = EXCLUDED.size_chart_id
RETURNING *;

-- name: ListVariantsByProduct :many
SELECT
    sqlc.embed(v),
    c.name_ru AS color_name_ru,
    c.name_ky AS color_name_ky,
    c.hex     AS color_hex
FROM product_variants v
JOIN colors c ON c.id = v.color_id
WHERE v.product_id = $1
-- Size order first, so distinct sizes come out ranked when scanning rows.
ORDER BY size_rank(v.size), v.size, v.color_id;

-- name: UpsertVariant :one
INSERT INTO product_variants (product_id, color_id, size, sku, stock, is_active)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (sku) DO UPDATE
SET product_id = EXCLUDED.product_id,
    color_id   = EXCLUDED.color_id,
    size       = EXCLUDED.size,
    stock      = EXCLUDED.stock,
    is_active  = EXCLUDED.is_active
RETURNING *;

-- name: LockVariantsForOrder :many
-- Row-locks the variants of an order being created. Locks are taken in id order
-- so concurrent checkouts with overlapping items cannot deadlock.
SELECT
    v.id,
    v.product_id,
    v.size,
    v.sku,
    v.stock,
    v.is_active,
    p.title_ru,
    p.title_ky,
    p.price,
    p.is_active AS product_is_active,
    c.name_ru   AS color_name_ru,
    c.name_ky   AS color_name_ky
FROM product_variants v
JOIN products p ON p.id = v.product_id
JOIN colors c ON c.id = v.color_id
WHERE v.id = ANY (sqlc.arg('ids')::bigint[])
ORDER BY v.id
FOR UPDATE OF v;

-- name: DecrementVariantStock :execrows
-- Affects 0 rows when there is not enough stock.
UPDATE product_variants
SET stock = stock - sqlc.arg('qty')::integer
WHERE id = sqlc.arg('id') AND stock >= sqlc.arg('qty')::integer;

-- name: IncrementVariantStock :exec
UPDATE product_variants
SET stock = stock + sqlc.arg('qty')::integer
WHERE id = sqlc.arg('id');

-- name: ListProductImages :many
SELECT * FROM product_images
WHERE product_id = $1
ORDER BY sort, id;

-- name: CreateProductImage :one
INSERT INTO product_images (product_id, color_id, s3_key, sort, alt_ru, alt_ky)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListProductSlugs :many
SELECT id, slug FROM products
WHERE id = ANY (sqlc.arg('ids')::bigint[]);
