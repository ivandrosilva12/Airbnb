-- 0046_message_templates.sql — host-authored saved reply templates.
--
-- The messaging composer reads /me/message-templates to surface chips next to
-- the built-in canned replies. Templates are author-scoped: only the owning
-- host reads/edits/deletes them.

CREATE TABLE IF NOT EXISTS message_templates (
    id          UUID PRIMARY KEY,
    host_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label       TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    body        TEXT NOT NULL CHECK (char_length(body)  BETWEEN 1 AND 4000),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- "List my templates" is the only read path, so an index on host_id keeps it
-- fast as the host's playbook grows.
CREATE INDEX IF NOT EXISTS message_templates_host_id_idx
    ON message_templates (host_id);
