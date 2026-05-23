-- 0011_property_blocks.sql — host-managed calendar blocks.

CREATE TABLE IF NOT EXISTS property_blocks (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    check_in    TIMESTAMPTZ NOT NULL,
    check_out   TIMESTAMPTZ NOT NULL,
    reason      TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    CHECK (check_out > check_in)
);

CREATE INDEX IF NOT EXISTS idx_property_blocks_property ON property_blocks (property_id, check_in, check_out);
