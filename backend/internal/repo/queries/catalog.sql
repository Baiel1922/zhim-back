-- name: ListCategories :many
SELECT * FROM categories
ORDER BY sort, id;

-- name: GetCategoryBySlug :one
SELECT * FROM categories
WHERE slug = $1;

-- name: UpsertCategory :one
INSERT INTO categories (slug, name_ru, name_ky, sort)
VALUES ($1, $2, $3, $4)
ON CONFLICT (slug) DO UPDATE
SET name_ru = EXCLUDED.name_ru,
    name_ky = EXCLUDED.name_ky,
    sort    = EXCLUDED.sort
RETURNING *;

-- name: GetSizeChart :one
SELECT * FROM size_charts
WHERE id = $1;

-- name: UpsertSizeChart :one
INSERT INTO size_charts (name, data)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE
SET data = EXCLUDED.data
RETURNING *;

-- name: ListColors :many
SELECT * FROM colors
ORDER BY id;

-- name: UpsertColor :one
INSERT INTO colors (name_ru, name_ky, hex)
VALUES ($1, $2, $3)
ON CONFLICT (name_ru) DO UPDATE
SET name_ky = EXCLUDED.name_ky,
    hex     = EXCLUDED.hex
RETURNING *;
