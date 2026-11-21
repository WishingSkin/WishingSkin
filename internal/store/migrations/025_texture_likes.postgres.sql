CREATE TABLE IF NOT EXISTS texture_likes (
    user_id BIGINT NOT NULL,
    record_id CHAR(32) COLLATE "C" NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, record_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (record_id) REFERENCES texture_records(record_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS texture_likes_record_created_idx ON texture_likes(record_id, created_at);
