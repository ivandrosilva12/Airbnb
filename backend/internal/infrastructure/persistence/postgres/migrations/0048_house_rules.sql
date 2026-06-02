-- House rules acceptance flow (S47). Two tables: house_rules holds
-- every version a host has ever authored (append-only — old rows are
-- never updated or deleted), house_rule_acceptances holds one row per
-- booking that acknowledged a specific version.
--
-- Why version history is kept: a guest's acceptance must always
-- resolve back to the exact text they agreed to. A naïve "current
-- row gets updated" schema would let a host change the rules after
-- the booking and rewrite the dispute history.

CREATE TABLE IF NOT EXISTS house_rules (
    property_id  UUID        NOT NULL,
    version      INTEGER     NOT NULL CHECK (version > 0),
    items        TEXT[]      NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (property_id, version)
);

-- The current-version lookup ("show me the latest for property X")
-- is served by the PK with an ORDER BY version DESC LIMIT 1; the
-- composite PK already covers it efficiently. No extra index needed.

CREATE TABLE IF NOT EXISTS house_rule_acceptances (
    booking_id        UUID        PRIMARY KEY,
    guest_id          UUID        NOT NULL,
    property_id       UUID        NOT NULL,
    accepted_version  INTEGER     NOT NULL CHECK (accepted_version > 0),
    accepted_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Show me every acceptance for a guest" — supports a future
-- "your house-rules history" privacy export without scanning the
-- whole table.
CREATE INDEX IF NOT EXISTS idx_house_rule_acceptances_guest
    ON house_rule_acceptances (guest_id, accepted_at DESC);

-- "Show me every acceptance for a property" — supports host
-- dashboards and admin spot-checks.
CREATE INDEX IF NOT EXISTS idx_house_rule_acceptances_property
    ON house_rule_acceptances (property_id, accepted_at DESC);
