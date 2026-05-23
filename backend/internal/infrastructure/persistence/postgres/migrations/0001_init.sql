-- 0001_init.sql — initial schema for the AirHost platform.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users -----------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id           UUID PRIMARY KEY,
    keycloak_sub TEXT        NOT NULL UNIQUE,
    email        TEXT        NOT NULL UNIQUE,
    full_name    TEXT        NOT NULL,
    role         TEXT        NOT NULL DEFAULT 'guest'
                 CHECK (role IN ('guest', 'host', 'admin')),
    avatar_url   TEXT        NOT NULL DEFAULT '',
    is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

-- Properties ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS properties (
    id            UUID PRIMARY KEY,
    host_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title         TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    type          TEXT        NOT NULL
                  CHECK (type IN ('apartment', 'house', 'room', 'villa', 'cabin')),
    status        TEXT        NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'published', 'suspended')),
    address_line1 TEXT        NOT NULL DEFAULT '',
    city          TEXT        NOT NULL,
    country       TEXT        NOT NULL,
    postal_code   TEXT        NOT NULL DEFAULT '',
    latitude      DOUBLE PRECISION NOT NULL DEFAULT 0,
    longitude     DOUBLE PRECISION NOT NULL DEFAULT 0,
    price_cents   BIGINT      NOT NULL CHECK (price_cents >= 0),
    currency      CHAR(3)     NOT NULL,
    max_guests    INTEGER     NOT NULL CHECK (max_guests >= 1),
    bedrooms      INTEGER     NOT NULL DEFAULT 0,
    beds          INTEGER     NOT NULL DEFAULT 0,
    bathrooms     INTEGER     NOT NULL DEFAULT 0,
    amenities     TEXT[]      NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_properties_host    ON properties (host_id);
CREATE INDEX IF NOT EXISTS idx_properties_city    ON properties (city);
CREATE INDEX IF NOT EXISTS idx_properties_status  ON properties (status);
CREATE INDEX IF NOT EXISTS idx_properties_price   ON properties (price_cents);
CREATE INDEX IF NOT EXISTS idx_properties_amenity ON properties USING GIN (amenities);

-- Property photos -------------------------------------------------------------
CREATE TABLE IF NOT EXISTS property_photos (
    id          UUID PRIMARY KEY,
    property_id UUID    NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    object_key  TEXT    NOT NULL,
    url         TEXT    NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_property_photos_property ON property_photos (property_id);

-- Bookings --------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS bookings (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    guest_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    check_in    TIMESTAMPTZ NOT NULL,
    check_out   TIMESTAMPTZ NOT NULL,
    guests      INTEGER     NOT NULL CHECK (guests >= 1),
    total_cents BIGINT      NOT NULL CHECK (total_cents >= 0),
    currency    CHAR(3)     NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'confirmed', 'cancelled', 'completed')),
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    CHECK (check_out > check_in)
);

CREATE INDEX IF NOT EXISTS idx_bookings_property ON bookings (property_id);
CREATE INDEX IF NOT EXISTS idx_bookings_guest    ON bookings (guest_id);
CREATE INDEX IF NOT EXISTS idx_bookings_dates    ON bookings (property_id, check_in, check_out);

-- Reviews ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS reviews (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    booking_id  UUID        NOT NULL UNIQUE REFERENCES bookings (id) ON DELETE CASCADE,
    guest_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    rating      INTEGER     NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment     TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reviews_property ON reviews (property_id);
