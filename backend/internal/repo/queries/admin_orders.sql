-- Filters shared by AdminListOrders and AdminCountOrdersByStatus:
-- search matches the number; search_digits (3+ digits of the query) matches the phone.

-- name: AdminListOrders :many
SELECT
    sqlc.embed(o),
    coalesce((SELECT sum(oi.qty) FROM order_items oi WHERE oi.order_id = o.id), 0)::integer AS items_count
FROM orders o
WHERE (sqlc.narg('status')::order_status IS NULL OR o.status = sqlc.narg('status')::order_status)
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR o.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR o.created_at < sqlc.narg('created_to')::timestamptz)
  AND (sqlc.narg('search')::text IS NULL
       OR o.number ILIKE '%' || sqlc.narg('search')::text || '%'
       OR (sqlc.narg('search_digits')::text IS NOT NULL AND o.phone LIKE '%' || sqlc.narg('search_digits')::text || '%'))
ORDER BY o.created_at DESC, o.id DESC
LIMIT sqlc.arg('limit_rows')::integer OFFSET sqlc.arg('offset_rows')::integer;

-- name: AdminCountOrdersByStatus :many
SELECT o.status, count(*)::integer AS count
FROM orders o
WHERE (sqlc.narg('created_from')::timestamptz IS NULL OR o.created_at >= sqlc.narg('created_from')::timestamptz)
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR o.created_at < sqlc.narg('created_to')::timestamptz)
  AND (sqlc.narg('search')::text IS NULL
       OR o.number ILIKE '%' || sqlc.narg('search')::text || '%'
       OR (sqlc.narg('search_digits')::text IS NOT NULL AND o.phone LIKE '%' || sqlc.narg('search_digits')::text || '%'))
GROUP BY o.status;

-- name: GetOrder :one
SELECT * FROM orders
WHERE id = $1;

-- name: LockOrder :one
SELECT * FROM orders
WHERE id = $1
FOR UPDATE;

-- name: UpdateOrderPaymentStatus :one
UPDATE orders
SET payment_status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateOrderInternalComment :one
UPDATE orders
SET internal_comment = $2
WHERE id = $1
RETURNING *;

-- name: ListOrderHistoryWithAdmins :many
SELECT h.*, a.name AS admin_name
FROM order_status_history h
LEFT JOIN admins a ON a.id = h.admin_id
WHERE h.order_id = $1
ORDER BY h.created_at, h.id;

-- name: DecrementPromoCodeUsage :exec
UPDATE promo_codes
SET used_count = used_count - 1
WHERE id = $1 AND used_count > 0;

-- name: OrderStatsSince :one
-- Cancelled orders do not count.
SELECT count(*)::integer AS orders, coalesce(sum(total), 0)::bigint AS revenue
FROM orders
WHERE created_at >= $1 AND status <> 'cancelled';

-- name: TopProductsSince :many
SELECT
    oi.product_id,
    max(oi.title_ru)::text          AS title,
    sum(oi.qty)::integer            AS qty,
    sum(oi.price * oi.qty)::bigint  AS revenue
FROM order_items oi
JOIN orders o ON o.id = oi.order_id
WHERE o.created_at >= sqlc.arg('since') AND o.status <> 'cancelled'
GROUP BY oi.product_id
ORDER BY qty DESC, revenue DESC
LIMIT sqlc.arg('limit_rows')::integer;

-- name: ListLowStockVariants :many
SELECT
    v.id,
    v.product_id,
    v.size,
    v.sku,
    v.stock,
    p.title_ru,
    c.name_ru AS color_name_ru
FROM product_variants v
JOIN products p ON p.id = v.product_id
JOIN colors c ON c.id = v.color_id
WHERE v.is_active AND p.is_active AND v.stock <= sqlc.arg('threshold')::integer
ORDER BY v.stock, p.title_ru, size_rank(v.size);
