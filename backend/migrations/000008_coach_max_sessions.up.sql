ALTER TABLE coaches
    ADD COLUMN max_sessions_per_day SMALLINT NOT NULL DEFAULT 4
        CHECK (max_sessions_per_day BETWEEN 2 AND 8);
