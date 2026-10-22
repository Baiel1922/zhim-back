-- name: CreateOrder :one
INSERT INTO orders (
    payment_method, customer_name, phone, comment,
    delivery_method, delivery_zone_id, address, landmark,
    subtotal, delivery_price, discount, total, promo_code_id, lang,
    utm_source, utm_medium, utm_campaign, utm_content, fbp, fbc, ttclid,
    idempotency_key
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20, $21,
    $22
)
RETURNING *;

-- name: CreateOrderItem :one
INSERT INTO order_items (
    order_id, variant_id, product_id, title_ru, title_ky,
    color_name_ru, color_name_ky, size, sku, price, qty
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetOrderByIdempotencyKey :one
SELECT * FROM orders
WHERE idempotency_key = $1;

-- name: GetOrderByNumber :one
SELECT * FROM orders
WHERE number = $1;

-- name: GetOrderByNumberAndPhone :one
-- Public order status page: the phone acts as a second factor for the guessable number.
SELECT * FROM orders
WHERE number = $1 AND phone = $2;

-- name: ListOrderItems :many
SELECT * FROM order_items
WHERE order_id = $1
ORDER BY id;

-- name: UpdateOrderStatus :one
-- Compare-and-set: returns no rows if the status changed concurrently.
UPDATE orders
SET status = sqlc.arg('to_status')
WHERE id = sqlc.arg('id') AND status = sqlc.arg('from_status')
RETURNING *;

-- name: CreateOrderStatusHistory :one
INSERT INTO order_status_history (order_id, from_status, to_status, admin_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListOrderStatusHistory :many
SELECT * FROM order_status_history
WHERE order_id = $1
ORDER BY created_at, id;
