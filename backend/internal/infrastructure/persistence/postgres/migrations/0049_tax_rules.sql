-- Tax rules per jurisdiction (S48). One row per (rule_id); the rule
-- itself carries the country/city scope and the calculation knobs.
--
-- The schema is intentionally flat (no separate jurisdiction table)
-- because the rule set is tiny (dozens for the foreseeable future)
-- and a 1:N "jurisdiction has many rules" model would just add joins
-- without buying anything until we get to thousands of rules.

CREATE TABLE IF NOT EXISTS tax_rules (
    id                  UUID        PRIMARY KEY,
    name                TEXT        NOT NULL,
    kind                TEXT        NOT NULL CHECK (kind IN ('percent','per_night_per_guest','per_stay')),
    country             TEXT        NOT NULL DEFAULT '',
    city                TEXT        NOT NULL DEFAULT '',
    currency            CHAR(3)     NOT NULL,
    rate_pct_bips       INTEGER     NOT NULL DEFAULT 0,
    flat_amount_cents   BIGINT      NOT NULL DEFAULT 0,
    max_nights          INTEGER     NOT NULL DEFAULT 0,
    effective_from      TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01 00:00:00+00',
    effective_until     TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01 00:00:00+00'
);

-- Hot-path lookup for the calculator: "give me every rule for
-- (country, city) right now". Lower-casing in SQL would simplify the
-- predicate; we leave the matcher to handle case-insensitive
-- comparison in Go so the storage layer stays case-preserving (the
-- admin UI shows the same string the host typed).
CREATE INDEX IF NOT EXISTS idx_tax_rules_jurisdiction
    ON tax_rules (country, city);
