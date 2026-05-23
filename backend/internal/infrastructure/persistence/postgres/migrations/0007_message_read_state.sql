-- 0007_message_read_state.sql — per-participant read markers for conversations.

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS host_last_read_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS guest_last_read_at TIMESTAMPTZ;
