-- Message attachments: a message may carry an optional file (image or document)
-- stored in object storage. NULL columns mean a plain text message.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS attachment_url  TEXT,
    ADD COLUMN IF NOT EXISTS attachment_type TEXT,
    ADD COLUMN IF NOT EXISTS attachment_name TEXT,
    ADD COLUMN IF NOT EXISTS attachment_size BIGINT;

-- Once an attachment can stand on its own, the body is no longer mandatory; a
-- message is valid as long as it has text or an attachment.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'messages_text_or_attachment'
    ) THEN
        ALTER TABLE messages
            ADD CONSTRAINT messages_text_or_attachment
            CHECK (length(coalesce(body, '')) > 0 OR attachment_url IS NOT NULL);
    END IF;
END $$;
