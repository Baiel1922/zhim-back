-- Delivery zones, promo codes, key-value settings and admins.

-- +goose Up
CREATE TABLE delivery_zones (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name_ru    text        NOT NULL UNIQUE CHECK (btrim(name_ru) <> ''),
    name_ky    text        NOT NULL CHECK (btrim(name_ky) <> ''),
    price      integer     NOT NULL CHECK (price >= 0),
    -- Free delivery when the order subtotal (after discount) reaches this amount.
    free_from  integer     CHECK (free_from > 0),
    is_active  boolean     NOT NULL DEFAULT true,
    sort       integer     NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER delivery_zones_set_updated_at BEFORE UPDATE ON delivery_zones
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE promo_codes (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code       citext      NOT NULL UNIQUE CHECK (code ~ '^[A-Za-z0-9_-]{3,32}$'),
    type       promo_type  NOT NULL,
    -- Percent (1–100) or a fixed amount in KGS, depending on type.
    value      integer     NOT NULL CHECK (value > 0),
    min_total  integer     NOT NULL DEFAULT 0 CHECK (min_total >= 0),
    max_uses   integer     CHECK (max_uses > 0),
    used_count integer     NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    valid_from timestamptz,
    valid_to   timestamptz,
    is_active  boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT promo_codes_percent_range_chk CHECK (type <> 'percent' OR value <= 100),
    CONSTRAINT promo_codes_validity_chk CHECK (valid_to > valid_from),
    CONSTRAINT promo_codes_max_uses_chk CHECK (used_count <= max_uses)
);
CREATE TRIGGER promo_codes_set_updated_at BEFORE UPDATE ON promo_codes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Keys in use: contacts, pickup, hero. Value shapes are validated in the service layer.
CREATE TABLE settings (
    key        text        PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]*$'),
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER settings_set_updated_at BEFORE UPDATE ON settings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE admins (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    telegram_id bigint      NOT NULL UNIQUE CHECK (telegram_id > 0),
    name        text        NOT NULL CHECK (btrim(name) <> ''),
    role        admin_role  NOT NULL DEFAULT 'manager',
    is_active   boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER admins_set_updated_at BEFORE UPDATE ON admins
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE admins;
DROP TABLE settings;
DROP TABLE promo_codes;
DROP TABLE delivery_zones;
