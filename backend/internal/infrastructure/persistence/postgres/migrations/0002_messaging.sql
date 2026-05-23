-- 0002_messaging.sql — host↔guest messaging.

CREATE TABLE IF NOT EXISTS conversations (
    id              UUID PRIMARY KEY,
    property_id     UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    host_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    guest_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL,
    last_message_at TIMESTAMPTZ NOT NULL,
    UNIQUE (property_id, guest_id),
    CHECK (host_id <> guest_id)
);

CREATE INDEX IF NOT EXISTS idx_conversations_host  ON conversations (host_id);
CREATE INDEX IF NOT EXISTS idx_conversations_guest ON conversations (guest_id);

CREATE TABLE IF NOT EXISTS messages (
    id              UUID PRIMARY KEY,
    conversation_id UUID        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body            TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages (conversation_id, created_at);
