CREATE TABLE IF NOT EXISTS texture_likes (
    user_id BIGINT NOT NULL,
    record_id CHAR(32) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, record_id),
    INDEX texture_likes_record_created_idx (record_id, created_at),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (record_id) REFERENCES texture_records(record_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
