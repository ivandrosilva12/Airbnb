-- Denormalised Superhost flag on each listing, fanned out from the owning
-- host's aggregate guest rating (see property.QualifiesAsSuperhost). Lets search
-- and listing reads surface the badge without a per-host lookup.
ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS host_is_superhost BOOLEAN NOT NULL DEFAULT FALSE;

-- Speeds up the host-scoped aggregate and the fan-out UPDATE that recomputes the
-- flag whenever a guest review is published.
CREATE INDEX IF NOT EXISTS idx_properties_host_id ON properties (host_id);
