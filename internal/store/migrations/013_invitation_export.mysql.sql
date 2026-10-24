ALTER TABLE invitation_codes
    ADD COLUMN encrypted_code TEXT,
    ADD COLUMN source_id BIGINT,
    ADD COLUMN generated_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN used_at BIGINT,
    ADD COLUMN source_user_id BIGINT;
CREATE UNIQUE INDEX invitation_codes_source_id ON invitation_codes(source_id);
