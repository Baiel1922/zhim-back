-- name: ListActiveDeliveryZones :many
SELECT * FROM delivery_zones
WHERE is_active
ORDER BY sort, id;

-- name: GetDeliveryZone :one
SELECT * FROM delivery_zones
WHERE id = $1;

-- name: UpsertDeliveryZone :one
INSERT INTO delivery_zones (name_ru, name_ky, price, free_from, is_active, sort)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (name_ru) DO UPDATE
SET name_ky   = EXCLUDED.name_ky,
    price     = EXCLUDED.price,
    free_from = EXCLUDED.free_from,
    is_active = EXCLUDED.is_active,
    sort      = EXCLUDED.sort
RETURNING *;

-- name: GetPromoCodeByCode :one
-- Case-insensitive (citext).
SELECT * FROM promo_codes
WHERE code = sqlc.arg('code')::citext;

-- name: GetPromoCodeByID :one
SELECT * FROM promo_codes
WHERE id = $1;

-- name: CreatePromoCode :one
INSERT INTO promo_codes (code, type, value, min_total, max_uses, valid_from, valid_to, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: IncrementPromoCodeUsage :execrows
-- Affects 0 rows when the code has reached max_uses.
UPDATE promo_codes
SET used_count = used_count + 1
WHERE id = $1 AND (max_uses IS NULL OR used_count < max_uses);

-- name: GetSetting :one
SELECT * FROM settings
WHERE key = $1;

-- name: ListSettings :many
SELECT * FROM settings
WHERE key = ANY (sqlc.arg('keys')::text[])
ORDER BY key;

-- name: UpsertSetting :one
INSERT INTO settings (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value
RETURNING *;

-- name: GetActiveAdminByTelegramID :one
SELECT * FROM admins
WHERE telegram_id = $1 AND is_active;

-- name: ListActiveAdmins :many
SELECT * FROM admins
WHERE is_active
ORDER BY id;

-- name: UpsertAdmin :one
INSERT INTO admins (telegram_id, name, role, is_active)
VALUES ($1, $2, $3, $4)
ON CONFLICT (telegram_id) DO UPDATE
SET name      = EXCLUDED.name,
    role      = EXCLUDED.role,
    is_active = EXCLUDED.is_active
RETURNING *;
