CREATE TABLE IF NOT EXISTS wardrobe (
    user_id BIGINT NOT NULL,
    record_id CHAR(32) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    collected_at BIGINT NOT NULL,
    PRIMARY KEY (user_id,record_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (record_id) REFERENCES texture_records(record_id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
