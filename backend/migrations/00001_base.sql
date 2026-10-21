-- Extensions, shared helpers and enum types.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

-- Keeps updated_at current on every UPDATE.
-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Display order of clothing sizes; unknown sizes go last.
-- +goose StatementBegin
CREATE FUNCTION size_rank(size text) RETURNS integer
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE upper(size)
        WHEN 'XXS' THEN 10
        WHEN 'XS' THEN 20
        WHEN 'S' THEN 30
        WHEN 'M' THEN 40
        WHEN 'L' THEN 50
        WHEN 'XL' THEN 60
        WHEN 'XXL' THEN 70
        WHEN '2XL' THEN 70
        WHEN 'XXXL' THEN 80
        WHEN '3XL' THEN 80
        ELSE 1000
    END
$$;
-- +goose StatementEnd

CREATE TYPE app_lang AS ENUM ('ru', 'ky');
CREATE TYPE admin_role AS ENUM ('owner', 'manager');
CREATE TYPE promo_type AS ENUM ('percent', 'fixed');
CREATE TYPE order_status AS ENUM (
    'new', 'confirmed', 'assembling', 'shipped', 'ready_for_pickup', 'completed', 'cancelled'
);
CREATE TYPE payment_status AS ENUM ('unpaid', 'paid');
CREATE TYPE payment_method AS ENUM ('cash', 'transfer');
CREATE TYPE delivery_method AS ENUM ('courier', 'pickup');

-- +goose Down
DROP TYPE delivery_method;
DROP TYPE payment_method;
DROP TYPE payment_status;
DROP TYPE order_status;
DROP TYPE promo_type;
DROP TYPE admin_role;
DROP TYPE app_lang;
DROP FUNCTION size_rank(text);
DROP FUNCTION set_updated_at();
DROP EXTENSION IF EXISTS citext;
