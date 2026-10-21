-- name: SaveOrderNotification :exec
INSERT INTO order_notifications (order_id, chat_id, message_id)
VALUES ($1, $2, $3)
ON CONFLICT (order_id, chat_id) DO UPDATE
SET message_id = EXCLUDED.message_id;

-- name: ListOrderNotifications :many
SELECT * FROM order_notifications
WHERE order_id = $1
ORDER BY chat_id;

-- name: ListActiveOwners :many
SELECT * FROM admins
WHERE role = 'owner' AND is_active
ORDER BY id;
