-- Telegram messages sent about an order, so every admin's copy can be updated.

-- +goose Up
CREATE TABLE order_notifications (
    order_id   bigint      NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    chat_id    bigint      NOT NULL,
    message_id bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, chat_id)
);

-- +goose Down
DROP TABLE order_notifications;
