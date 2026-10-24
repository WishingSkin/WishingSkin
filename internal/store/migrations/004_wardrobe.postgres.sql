CREATE TABLE IF NOT EXISTS wardrobe (
    user_id BIGINT NOT NULL,
    record_id CHAR(32) COLLATE "C" NOT NULL,
    display_name VARCHAR(255) COLLATE "C" NOT NULL,
    collected_at BIGINT NOT NULL,
    PRIMARY KEY (user_id,record_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (record_id) REFERENCES texture_records(record_id)
);
CREATE INDEX IF NOT EXISTS wardrobe_record_id_idx ON wardrobe(record_id);
