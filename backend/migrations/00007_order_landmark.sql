-- Optional landmark for the courier ("next to the pharmacy"), courier orders only.

-- +goose Up
ALTER TABLE orders
    ADD COLUMN landmark text,
    ADD CONSTRAINT orders_landmark_courier_only CHECK (delivery_method = 'courier' OR landmark IS NULL);

-- +goose Down
ALTER TABLE orders
    DROP CONSTRAINT orders_landmark_courier_only,
    DROP COLUMN landmark;
