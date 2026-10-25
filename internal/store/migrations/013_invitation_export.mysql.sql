ALTER TABLE invitation_codes ADD COLUMN encrypted_code TEXT;
ALTER TABLE invitation_codes ADD COLUMN source_id BIGINT;
ALTER TABLE invitation_codes ADD COLUMN generated_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE invitation_codes ADD COLUMN used_at BIGINT;
ALTER TABLE invitation_codes ADD COLUMN source_user_id BIGINT;
CREATE UNIQUE INDEX invitation_codes_source_id ON invitation_codes(source_id);
