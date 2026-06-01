-- 0040_disputes.sql
--
-- S13 — Resolution Center. A Dispute attaches to a Booking and represents a
-- post-stay claim raised by either the guest or the host (refund, damage,
-- other). The dispute moves through open → under_review → resolved|rejected;
-- an admin records the final resolution text. Evidence (notes + optional
-- URLs) is appended over time and stored in a child table.
--
-- A booking can have at most one *active* dispute at a time (open or
-- under_review) so the moderation queue does not split a single incident.

CREATE TABLE disputes (
    id                     uuid PRIMARY KEY,
    booking_id             uuid NOT NULL REFERENCES bookings(id),
    opener_id              uuid NOT NULL REFERENCES users(id),
    kind                   text NOT NULL CHECK (kind IN ('refund', 'damage', 'other')),
    reason                 text NOT NULL,
    requested_amount_cents bigint NOT NULL DEFAULT 0,
    currency               text NOT NULL,
    status                 text NOT NULL CHECK (status IN ('open', 'under_review', 'resolved', 'rejected')),
    host_response          text NOT NULL DEFAULT '',
    resolution             text NOT NULL DEFAULT '',
    admin_id               uuid REFERENCES users(id),
    opened_at              timestamptz NOT NULL DEFAULT now(),
    decided_at             timestamptz,
    updated_at             timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX disputes_booking_idx ON disputes (booking_id);
CREATE INDEX disputes_opener_idx ON disputes (opener_id);
-- A partial unique index enforces "one active dispute per booking" without
-- blocking new disputes after the previous one is resolved or rejected.
CREATE UNIQUE INDEX disputes_one_active_per_booking_idx
    ON disputes (booking_id)
    WHERE status IN ('open', 'under_review');

CREATE TABLE dispute_evidence (
    id          uuid PRIMARY KEY,
    dispute_id  uuid NOT NULL REFERENCES disputes(id) ON DELETE CASCADE,
    url         text NOT NULL DEFAULT '',
    note        text NOT NULL DEFAULT '',
    added_by    uuid NOT NULL REFERENCES users(id),
    added_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dispute_evidence_dispute_idx ON dispute_evidence (dispute_id, added_at);
