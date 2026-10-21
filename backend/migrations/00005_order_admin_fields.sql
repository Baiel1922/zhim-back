-- Admin-only order notes.

-- +goose Up
ALTER TABLE orders
    ADD COLUMN internal_comment text NOT NULL DEFAULT ''
        CONSTRAINT orders_internal_comment_check CHECK (length(internal_comment) <= 2000);

-- +goose Down
ALTER TABLE orders DROP COLUMN internal_comment;
