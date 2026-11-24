ALTER TABLE users ADD COLUMN character_limit BIGINT NULL;
ALTER TABLE texture_records ADD COLUMN created_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX texture_records_created_idx ON texture_records(created_at, record_id);
