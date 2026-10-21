-- Catalog: categories, size charts, colors, products, variants, images.
-- Money is stored as integer KGS (som).

-- +goose Up
CREATE TABLE categories (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug       text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    name_ru    text        NOT NULL CHECK (btrim(name_ru) <> ''),
    name_ky    text        NOT NULL CHECK (btrim(name_ky) <> ''),
    sort       integer     NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER categories_set_updated_at BEFORE UPDATE ON categories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- data shape (validated in the service layer):
-- {
--   "unit": "cm",
--   "columns": [{"key": "chest", "name_ru": "...", "name_ky": "..."}],
--   "rows":    [{"size": "M", "values": {"chest": "96–102"}}],
--   "fit":     [{"size": "M", "height_min": 172, "height_max": 180, "weight_min": 68, "weight_max": 78}]
-- }
CREATE TABLE size_charts (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL UNIQUE CHECK (btrim(name) <> ''),
    data       jsonb       NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER size_charts_set_updated_at BEFORE UPDATE ON size_charts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE colors (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name_ru text NOT NULL UNIQUE CHECK (btrim(name_ru) <> ''),
    name_ky text NOT NULL CHECK (btrim(name_ky) <> ''),
    hex     text NOT NULL CHECK (hex ~ '^#[0-9A-F]{6}$')
);

CREATE TABLE products (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug           text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    category_id    bigint      NOT NULL REFERENCES categories (id) ON DELETE RESTRICT,
    title_ru       text        NOT NULL CHECK (btrim(title_ru) <> ''),
    title_ky       text        NOT NULL CHECK (btrim(title_ky) <> ''),
    description_ru text        NOT NULL DEFAULT '',
    description_ky text        NOT NULL DEFAULT '',
    composition_ru text        NOT NULL DEFAULT '',
    composition_ky text        NOT NULL DEFAULT '',
    care_ru        text        NOT NULL DEFAULT '',
    care_ky        text        NOT NULL DEFAULT '',
    price          integer     NOT NULL CHECK (price > 0),
    old_price      integer,
    is_active      boolean     NOT NULL DEFAULT false,
    is_new         boolean     NOT NULL DEFAULT false,
    sort           integer     NOT NULL DEFAULT 0,
    size_chart_id  bigint      REFERENCES size_charts (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT products_old_price_chk CHECK (old_price > price)
);
CREATE TRIGGER products_set_updated_at BEFORE UPDATE ON products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- Storefront listing: active products, optionally by category, in manual sort order.
CREATE INDEX products_active_sort_idx ON products (sort, id) WHERE is_active;
CREATE INDEX products_category_active_sort_idx ON products (category_id, sort, id) WHERE is_active;
-- FK lookups (admin lists by category, ON DELETE SET NULL on size_charts).
CREATE INDEX products_category_id_idx ON products (category_id);
CREATE INDEX products_size_chart_id_idx ON products (size_chart_id);

CREATE TABLE product_variants (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id bigint      NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    color_id   bigint      NOT NULL REFERENCES colors (id) ON DELETE RESTRICT,
    size       text        NOT NULL CHECK (btrim(size) <> '' AND length(size) <= 16),
    sku        text        NOT NULL UNIQUE CHECK (sku ~ '^[A-Z0-9][A-Z0-9-]{1,63}$'),
    stock      integer     NOT NULL DEFAULT 0 CHECK (stock >= 0),
    is_active  boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Also serves lookups by product_id.
    CONSTRAINT product_variants_product_color_size_key UNIQUE (product_id, color_id, size)
);
CREATE TRIGGER product_variants_set_updated_at BEFORE UPDATE ON product_variants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX product_variants_color_id_idx ON product_variants (color_id);

CREATE TABLE product_images (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id bigint      NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    -- NULL = shown for every color.
    color_id   bigint      REFERENCES colors (id) ON DELETE SET NULL,
    s3_key     text        NOT NULL UNIQUE CHECK (btrim(s3_key) <> ''),
    sort       integer     NOT NULL DEFAULT 0,
    alt_ru     text        NOT NULL DEFAULT '',
    alt_ky     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX product_images_product_sort_idx ON product_images (product_id, sort, id);
CREATE INDEX product_images_color_id_idx ON product_images (color_id);

-- +goose Down
DROP TABLE product_images;
DROP TABLE product_variants;
DROP TABLE products;
DROP TABLE colors;
DROP TABLE size_charts;
DROP TABLE categories;
