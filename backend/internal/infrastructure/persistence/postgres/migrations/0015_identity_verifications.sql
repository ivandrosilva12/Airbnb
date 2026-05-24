-- 0015_identity_verifications.sql — KYC identity verification.
--
-- A user submits a government identity document (document_ref points at the
-- uploaded scan held by the KYC provider, never raw PII); a platform
-- administrator approves or rejects it. The user's verified state is the status
-- of their most recent row.

CREATE TABLE IF NOT EXISTS identity_verifications (
    id               UUID PRIMARY KEY,
    user_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status           TEXT        NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    document_type    TEXT        NOT NULL CHECK (document_type IN ('passport', 'id_card', 'driver_license')),
    document_ref     TEXT        NOT NULL,
    legal_name       TEXT        NOT NULL,
    rejection_reason TEXT        NOT NULL DEFAULT '',
    reviewer_id      UUID        REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_identity_user ON identity_verifications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_identity_status ON identity_verifications (status, created_at DESC);
