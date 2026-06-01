-- 0044_cohosts.sql — per-listing co-host grants.
--
-- A co-host is a user other than the primary host who has been granted one or
-- more granular permissions on a single listing (managing the calendar,
-- managing per-date pricing, replying to messages). The primary host invites,
-- updates and revokes co-hosts; co-hosts themselves cannot manage other
-- co-hosts, delete the listing, or move money. The HTTP layer enforces the
-- "only the primary host can mutate the cohost set" rule; this table only
-- enforces uniqueness and referential integrity.

CREATE TABLE IF NOT EXISTS cohosts (
    id            UUID PRIMARY KEY,
    property_id   UUID NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Permissions is the set of granted CohostPermission codes. We store it
    -- as a TEXT[] (Postgres array) so a single row carries the whole grant;
    -- the application normalises and validates each entry before persistence.
    permissions   TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A user has at most one grant per listing — repeated invites mutate the
-- existing row (no duplicate grants to confuse the permission check).
CREATE UNIQUE INDEX IF NOT EXISTS cohosts_property_user_uniq
    ON cohosts (property_id, user_id);

-- Inverse lookup used by "listings I help manage".
CREATE INDEX IF NOT EXISTS cohosts_user_id_idx ON cohosts (user_id);
