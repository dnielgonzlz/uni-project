ALTER TABLE availability_campaign_recipients
    ADD COLUMN IF NOT EXISTS parse_status TEXT DEFAULT 'pending'
        CHECK (parse_status IN ('pending', 'parsed', 'ambiguous', 'failed'));
