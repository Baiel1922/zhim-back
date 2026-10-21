-- Orders, order items (price/title snapshots) and status history.

-- +goose Up
CREATE SEQUENCE order_number_seq;

-- Human-readable order number: ZH-000123 (grows past 6 digits instead of truncating).
-- +goose StatementBegin
CREATE FUNCTION next_order_number() RETURNS text
LANGUAGE sql VOLATILE AS $$
    SELECT 'ZH-' || lpad(n::text, greatest(6, length(n::text)), '0')
    FROM nextval('order_number_seq') AS n
$$;
-- +goose StatementEnd

CREATE TABLE orders (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    number           text            NOT NULL UNIQUE DEFAULT next_order_number(),
    status           order_status    NOT NULL DEFAULT 'new',
    payment_status   payment_status  NOT NULL DEFAULT 'unpaid',
    payment_method   payment_method  NOT NULL,
    customer_name    text            NOT NULL CHECK (length(btrim(customer_name)) BETWEEN 1 AND 100),
    phone            text            NOT NULL CHECK (phone ~ '^\+996[0-9]{9}$'),
    comment          text            NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    delivery_method  delivery_method NOT NULL,
    delivery_zone_id bigint          REFERENCES delivery_zones (id) ON DELETE RESTRICT,
    address          text,
    subtotal         integer         NOT NULL CHECK (subtotal >= 0),
    delivery_price   integer         NOT NULL DEFAULT 0 CHECK (delivery_price >= 0),
    discount         integer         NOT NULL DEFAULT 0 CHECK (discount >= 0),
    total            integer         NOT NULL,
    promo_code_id    bigint          REFERENCES promo_codes (id) ON DELETE SET NULL,
    lang             app_lang        NOT NULL DEFAULT 'ru',
    utm_source       text,
    utm_medium       text,
    utm_campaign     text,
    utm_content      text,
    fbp              text,
    fbc              text,
    ttclid           text,
    idempotency_key  text            NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    created_at       timestamptz     NOT NULL DEFAULT now(),
    updated_at       timestamptz     NOT NULL DEFAULT now(),
    CONSTRAINT orders_discount_chk CHECK (discount <= subtotal),
    CONSTRAINT orders_total_chk CHECK (total = subtotal - discount + delivery_price),
    CONSTRAINT orders_delivery_chk CHECK (
        CASE delivery_method
            WHEN 'courier' THEN delivery_zone_id IS NOT NULL AND btrim(coalesce(address, '')) <> ''
            WHEN 'pickup' THEN delivery_zone_id IS NULL AND address IS NULL AND delivery_price = 0
        END
    )
);
CREATE TRIGGER orders_set_updated_at BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- Admin list (newest first, optionally by status) and dashboard date ranges.
CREATE INDEX orders_created_at_idx ON orders (created_at DESC);
CREATE INDEX orders_status_created_at_idx ON orders (status, created_at DESC);
-- Admin search by phone; storefront lookup uses the unique number index.
CREATE INDEX orders_phone_idx ON orders (phone);
CREATE INDEX orders_delivery_zone_id_idx ON orders (delivery_zone_id) WHERE delivery_zone_id IS NOT NULL;
CREATE INDEX orders_promo_code_id_idx ON orders (promo_code_id) WHERE promo_code_id IS NOT NULL;

CREATE TABLE order_items (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id      bigint  NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    -- Live links survive only while the catalog rows exist; snapshots below are the record.
    variant_id    bigint  REFERENCES product_variants (id) ON DELETE SET NULL,
    product_id    bigint  REFERENCES products (id) ON DELETE SET NULL,
    title_ru      text    NOT NULL,
    title_ky      text    NOT NULL,
    color_name_ru text    NOT NULL,
    color_name_ky text    NOT NULL,
    size          text    NOT NULL,
    sku           text    NOT NULL,
    price         integer NOT NULL CHECK (price >= 0),
    qty           integer NOT NULL CHECK (qty BETWEEN 1 AND 99)
);
CREATE INDEX order_items_order_id_idx ON order_items (order_id);
CREATE INDEX order_items_variant_id_idx ON order_items (variant_id);
CREATE INDEX order_items_product_id_idx ON order_items (product_id);

CREATE TABLE order_status_history (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id    bigint       NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    -- NULL on the initial "order created" entry.
    from_status order_status,
    to_status   order_status NOT NULL,
    -- NULL when the change was made by the customer or the system.
    admin_id    bigint       REFERENCES admins (id) ON DELETE SET NULL,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT order_status_history_change_chk CHECK (from_status IS DISTINCT FROM to_status)
);
CREATE INDEX order_status_history_order_id_idx ON order_status_history (order_id, created_at);
CREATE INDEX order_status_history_admin_id_idx ON order_status_history (admin_id) WHERE admin_id IS NOT NULL;

-- +goose Down
DROP TABLE order_status_history;
DROP TABLE order_items;
DROP TABLE orders;
DROP FUNCTION next_order_number();
DROP SEQUENCE order_number_seq;
