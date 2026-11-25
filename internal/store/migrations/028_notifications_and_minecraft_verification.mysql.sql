CREATE TABLE IF NOT EXISTS user_notifications (
    notification_id CHAR(32) NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind VARCHAR(32) NOT NULL,
    title VARCHAR(160) NOT NULL,
    body VARCHAR(600) NOT NULL,
    link VARCHAR(255) NOT NULL DEFAULT '',
    read_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX user_notifications_user_created_idx(user_id, created_at, notification_id),
    INDEX user_notifications_user_unread_idx(user_id, read_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE TABLE IF NOT EXISTS minecraft_verifications (
    user_id BIGINT NOT NULL PRIMARY KEY,
    mc_uuid CHAR(32) NOT NULL UNIQUE,
    mc_name VARCHAR(64) NOT NULL,
    ms_subject VARCHAR(128) NOT NULL,
    verified_at BIGINT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE TABLE IF NOT EXISTS minecraft_flows (
    flow_hash CHAR(64) NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    INDEX minecraft_flow_expiry(expires_at)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
