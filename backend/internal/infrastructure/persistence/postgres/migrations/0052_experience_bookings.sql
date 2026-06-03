-- Experience bookings (S80 in memory → promoted to postgres in S81).
--
-- A guest's reservation of a host-led session — modelled as a separate
-- aggregate from booking (the property reservation) because the
-- invariants are different: per-session, per-guest pricing, no overnight
-- range, no cleaning fee, hard cap from the parent experience's
-- MaxGuests. The domain aggregate lives in
-- internal/domain/experiencebooking; this table is the storage edge for
-- experiencebooking.Repository.
--
-- Schema-level CHECKs mirror the same invariants the domain
-- constructor enforces — defence in depth so a buggy adapter (or a
-- manual INSERT during a hotfix) cannot land a row the application
-- would later refuse to load:
--   status IN the closed reservation lifecycle;
--   session_duration_minutes > 0 (a zero-length session is nonsense
--     and the EndAt() accessor would equal StartAt);
--   guests > 0 (the per-guest pricing math would otherwise floor to 0).
--
-- Pricing is stored as JSONB so the breakdown (per-guest price, subtotal,
-- service fee, total, currency) round-trips as one column rather than 4
-- pairs of (cents, currency) ints. The shape is owned by
-- experiencebooking.Pricing — the adapter marshals/unmarshals via
-- encoding/json. This also keeps the door open for future fields (tax
-- lines, payout split) without a column migration each time.
--
-- FK to experiences.id is ON DELETE CASCADE so that deleting an
-- experience (which the domain currently forbids once any booking
-- exists, but a future hard-delete admin path may permit) cleans up its
-- booking history rather than leaving orphans behind.
CREATE TABLE IF NOT EXISTS experience_bookings (
    id                       UUID        PRIMARY KEY,
    experience_id            UUID        NOT NULL REFERENCES experiences(id) ON DELETE CASCADE,
    host_id                  UUID        NOT NULL,
    guest_id                 UUID        NOT NULL,
    session_start_at         TIMESTAMPTZ NOT NULL,
    session_duration_minutes INTEGER     NOT NULL CHECK (session_duration_minutes > 0),
    guests                   INTEGER     NOT NULL CHECK (guests > 0),
    pricing_jsonb            JSONB       NOT NULL,
    status                   TEXT        NOT NULL CHECK (status IN ('pending','confirmed','cancelled','completed')),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Read patterns:
--   guest's "My experiences" list, newest first  → idx_xb_guest
--   host dashboard "bookings against my listings" → idx_xb_host
--   overlap check on Create / Modify              → idx_xb_overlap
--
-- The (created_at DESC, id) tiebreaker matches the deterministic
-- pagination contract from S101 / P1 — so identical CreatedAt rows
-- still page deterministically across requests.
CREATE INDEX IF NOT EXISTS idx_xb_guest
    ON experience_bookings (guest_id, created_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_xb_host
    ON experience_bookings (host_id, created_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_xb_overlap
    ON experience_bookings (experience_id, session_start_at);
